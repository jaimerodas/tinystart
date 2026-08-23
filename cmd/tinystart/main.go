// Command tinystart serves the TinyStart start page.
//
// Everything the program does lives in run, and main does nothing but call it
// and turn an error into an exit code. That split is the whole point: run
// takes its context, its arguments, its environment and its output as
// parameters. As a result, a test can hand it a cancellable context, a fake
// environment and a buffer. Then it can exercise the real startup path rather
// than a rehearsal of it.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
	"github.com/jaimerodas/tinystart/internal/web"
)

func main() {
	if err := run(context.Background(), os.Args, os.Getenv, os.Stdin, os.Stdout); err != nil {
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
	// what makes running the binary from a checkout safe. Rails in development
	// used letter_opener and mailed nobody. Without it, a password reset on a
	// laptop goes to a real inbox. Empty means the real API, which is what
	// production leaves it as.
	envPostmarkURL = "POSTMARK_API_URL"
)

// configFromEnv reads the environment through the getenv it is given rather
// than os.Getenv. That is what lets the tests configure a run without
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

// run is the program. With no arguments it serves the start page and blocks
// until the context is canceled or the server fails, then shuts down and
// returns. With a subcommand — there is one, set-password — it does that
// instead and returns when it is done. Only a subcommand reads stdin.
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	// SIGTERM is how Kamal stops a container, and SIGINT is Ctrl-C in
	// development. Both cancel ctx, and the shutdown below is the one path
	// that runs on every deploy.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(stdout, nil))
	cfg := configFromEnv(getenv)

	if len(args) > 1 {
		switch args[1] {
		case "set-password":
			return setPassword(ctx, cfg, args[2:], stdin, stdout)
		default:
			return fmt.Errorf("unknown command %q (the only one is set-password)", args[1])
		}
	}

	// The secret key signs every cookie and every password reset link. There
	// is no sensible default — a hardcoded one means anyone can forge a
	// session. So a missing key stops the process here rather than producing
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
	// an empty file becomes the full schema, and a database that Rails
	// already writes to is left exactly as it is.
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrating %s: %w", cfg.dbPath, err)
	}

	// No token, no Postmark: mail goes to the log. On a laptop that is the
	// point. On the server it means the secret went missing, and the
	// warning is how that gets noticed before someone waits for a reset link.
	var mailer web.Mailer = &postmark.Client{Token: cfg.postmarkToken, BaseURL: cfg.postmarkURL}
	if cfg.postmarkToken == "" {
		logger.Warn(envPostmarkToken + " is not set; mail is written to the log instead of sent")
		mailer = web.LogMailer{Logger: logger}
	}

	handler, err := web.NewServer(web.Config{
		SecretKey:     cfg.secretKey,
		SecureCookies: cfg.production,
		Host:          cfg.host,
	}, db, logger, mailer, time.Now)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler: handler,
		// A server on the public internet needs every one of these. Without
		// them, a client that opens a connection and then says nothing holds a
		// goroutine and a file descriptor forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		// net/http writes its own occasional errors to a log.Logger. Route
		// them through slog so there is one log format, not two.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	// Listening here instead of letting srv.ListenAndServe do it means a port
	// that is already taken is an error run can return. It also means the
	// code can log the address actually bound. That matters when the
	// requested port was 0 and the kernel chose the real one. The tests rely
	// on that log line.
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.addr, err)
	}

	logger.Info("listening", "addr", listener.Addr().String(), "database", cfg.dbPath)

	serveErr := make(chan error, 1)
	go func() {
		// Shutdown makes Serve return ErrServerClosed. That is the ordinary
		// ending, not a failure, so this code normalizes it away here and
		// reports anything else.
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

	// ctx is already canceled by the time we get here, so we derive the
	// shutdown deadline from a context that is not. Otherwise it expires
	// immediately and in-flight requests get cut off.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return <-serveErr
}

// setPassword is `tinystart set-password <email>`: the way back into an
// account when the reset mail cannot reach anyone. That covers a laptop with
// no Postmark token, or an admin locked out in production, where it runs
// through `kamal app exec`. It is what `bin/rails console` was for.
//
// setPassword reads the password from stdin, one line, so it never appears
// in a shell history or a process list:
//
//	tinystart set-password jaime@example.com   (then type it and press Enter)
//	tinystart set-password jaime@example.com < password.txt
//
// It goes through the store's ResetPassword, so the same rules apply as to a
// reset from the page, and it leaves existing sessions alone. The person at
// the keyboard is the account's owner, and signing them out everywhere is
// not what they asked for.
func setPassword(ctx context.Context, cfg config, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: tinystart set-password <email>  (the password is read from stdin)")
	}
	email := args[0]

	password, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading the password: %w", err)
	}
	password = strings.TrimRight(password, "\r\n")
	if password == "" {
		return errors.New("the password is empty; it is read as one line from stdin")
	}

	db, err := store.Open(ctx, cfg.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	user, err := db.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no account has the email %s", email)
	}
	if err != nil {
		return err
	}
	if err := db.ResetPassword(ctx, user.ID, password); err != nil {
		return fmt.Errorf("setting the password: %w", err)
	}

	fmt.Fprintf(stdout, "password set for %s\n", user.Email)
	return nil
}
