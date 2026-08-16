// Command tinystart serves the TinyStart start page.
//
// Everything the program does lives in run, and main does nothing but call it
// and turn an error into an exit code. That split is the whole point: run
// takes its context, its arguments, its environment and its output as
// parameters, so a test can hand it a cancellable context, a fake environment
// and a buffer, and exercise the real startup path rather than a rehearsal of
// it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
	"github.com/jaimerodas/tinystart/internal/web"
)

func main() {
	if err := run(context.Background(), os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "tinystart: %v\n", err)
		os.Exit(1)
	}
}

// config is everything the process reads from the outside world at startup.
// Keeping it a struct filled by one function means there is exactly one place
// to look for "what does this app need to run".
type config struct {
	addr          string
	dbPath        string
	secretKey     []byte
	host          string
	postmarkToken string
	postmarkURL   string
	production    bool
}

// The environment variables, named so that a typo is a compile error rather
// than a setting that silently does nothing.
const (
	envAddr          = "TINYSTART_ADDR"
	envDB            = "TINYSTART_DB"
	envSecretKey     = "TINYSTART_SECRET_KEY"
	envHost          = "TINYSTART_HOST"
	envEnvironment   = "TINYSTART_ENV"
	envPostmarkToken = "POSTMARK_API_TOKEN"

	// envPostmarkURL points the mailer somewhere other than Postmark. It is
	// what makes running the binary from a checkout safe: Rails in development
	// used letter_opener and mailed nobody, and without this a password reset
	// on a laptop would go to a real inbox. Empty means the real API, which is
	// what production leaves it as.
	envPostmarkURL = "POSTMARK_API_URL"
)

// configFromEnv reads the environment through the getenv it is given rather
// than os.Getenv, which is what lets the tests configure a run without
// touching the process's real environment.
func configFromEnv(getenv func(string) string) config {
	cfg := config{
		addr:          getenv(envAddr),
		dbPath:        getenv(envDB),
		secretKey:     []byte(getenv(envSecretKey)),
		host:          getenv(envHost),
		postmarkToken: getenv(envPostmarkToken),
		postmarkURL:   getenv(envPostmarkURL),
		production:    getenv(envEnvironment) == "production",
	}
	if cfg.addr == "" {
		// The Rails app's development port, kept so bin/dev habits carry over.
		// The image overrides it with :80, which is what kamal-proxy expects.
		cfg.addr = ":3000"
	}
	if cfg.dbPath == "" {
		// Where the Rails app keeps its development database, so that running
		// the binary from a checkout opens the data that is already there. The
		// image sets /data/production.sqlite3, on the Kamal volume.
		cfg.dbPath = "storage/development.sqlite3"
	}
	return cfg
}

// run starts the server and blocks until the context is cancelled or the
// server fails, then shuts down and returns. The args parameter is unused for
// now — there are no flags yet — but it stays in the signature because it is
// the natural place for them, and adding it later would change every caller.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	_ = args

	// SIGTERM is how Kamal stops a container, and SIGINT is Ctrl-C in
	// development: both cancel ctx, and the shutdown below is the one path
	// that runs on every deploy.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(stdout, nil))
	cfg := configFromEnv(getenv)

	// The secret key signs every cookie and every password reset link. There
	// is no sensible default — a hardcoded one would mean anyone could forge a
	// session — so a missing key stops the process here rather than producing
	// a server that looks fine and is not. Generate one with
	// `openssl rand -hex 32`.
	if len(cfg.secretKey) == 0 {
		return fmt.Errorf("%s is not set; generate one with `openssl rand -hex 32`", envSecretKey)
	}

	db, err := store.Open(ctx, cfg.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Migrating at boot is what replaces the Rails image's docker-entrypoint:
	// an empty file becomes the full schema, and a database Rails has been
	// writing is left exactly as it is.
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrating %s: %w", cfg.dbPath, err)
	}

	handler, err := web.NewServer(web.Config{
		SecretKey:     cfg.secretKey,
		SecureCookies: cfg.production,
		Host:          cfg.host,
	}, db, logger, &postmark.Client{Token: cfg.postmarkToken, BaseURL: cfg.postmarkURL}, time.Now)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler: handler,
		// A server on the public internet needs every one of these: without
		// them a client that opens a connection and then says nothing holds a
		// goroutine and a file descriptor forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		// net/http writes its own occasional errors to a log.Logger; route
		// them through slog so there is one log format, not two.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// Listening here instead of letting srv.ListenAndServe do it means a port
	// that is already taken is an error run can return, and the address
	// actually bound can be logged — which matters when the requested port was
	// 0 and the kernel chose the real one. The tests rely on that log line.
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.addr, err)
	}

	logger.Info("listening", "addr", listener.Addr().String(), "database", cfg.dbPath)

	serveErr := make(chan error, 1)
	go func() {
		// Shutdown makes Serve return ErrServerClosed; that is the ordinary
		// ending, not a failure, so it is normalised away here and anything
		// else is reported.
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")

	// ctx is already cancelled by the time we get here, so the shutdown
	// deadline has to be derived from a context that isn't — otherwise it
	// expires immediately and in-flight requests get cut off.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return <-serveErr
}
