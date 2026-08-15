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
)

func main() {
	if err := run(context.Background(), os.Args, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "tinystart: %v\n", err)
		os.Exit(1)
	}
}

// config is everything the process reads from the outside world at startup.
// Later phases add the database path and the secret key here; keeping it a
// struct filled by one function means there is exactly one place to look for
// "what does this app need to run".
type config struct {
	addr string
}

// configFromEnv reads the environment through the getenv it is given rather
// than os.Getenv, which is what lets the tests configure a run without
// touching the process's real environment.
func configFromEnv(getenv func(string) string) config {
	cfg := config{addr: getenv("TINYSTART_ADDR")}
	if cfg.addr == "" {
		// The Rails app's development port, kept so bin/dev habits carry over.
		// The image overrides it with :80, which is what kamal-proxy expects.
		cfg.addr = ":3000"
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

	mux := http.NewServeMux()
	addRoutes(mux)

	srv := &http.Server{
		Handler: mux,
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

	logger.Info("listening", "addr", listener.Addr().String())

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

// addRoutes lists every route the server answers. It stays one function on
// purpose: a single place to read the whole URL surface. It moves to
// internal/web once there is more than a health check to put in it.
func addRoutes(mux *http.ServeMux) {
	// GET /up is kamal-proxy's health check, the same path the Rails image
	// answered, so the deploy configuration does not have to change.
	mux.HandleFunc("GET /up", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
}
