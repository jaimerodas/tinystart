package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"mime"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// middleware is the one shape every wrapper in this file has. Composing them
// is then just nesting calls, and the order they run in is the order they are
// listed in NewServer.
type middleware func(http.Handler) http.Handler

// requestIDKey holds the identifier the log line and any error report share.
const requestIDKey contextKey = 100

// requestIDFrom returns the current request's identifier, or "" outside a
// request.
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// requestID gives every request an identifier and puts it in the context.
//
// Sixteen hex digits from crypto/rand: enough that two requests in the same
// log file will not collide, short enough to read out loud. It is not
// forwarded from a header — the proxy in front does not set one, and accepting
// a client-supplied id lets anyone poison the logs.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [8]byte
		rand.Read(raw[:]) //nolint:errcheck // crypto/rand.Read never fails; since Go 1.24 it panics rather than returning an error
		ctx := context.WithValue(r.Context(), requestIDKey, hex.EncodeToString(raw[:]))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recorder wraps the ResponseWriter so the log line can report the status.
//
// http.ResponseWriter has no way to ask what was written, and the alternative
// — having every handler report its own status — is a thing to forget. Unwrap
// is what lets http.ResponseController reach the real writer through this one,
// which is how flushing and hijacking keep working.
type recorder struct {
	http.ResponseWriter
	status int
}

func (rec *recorder) WriteHeader(status int) {
	rec.status = status
	rec.ResponseWriter.WriteHeader(status)
}

// Write covers the handlers that never call WriteHeader: writing a body
// implies a 200, and without this the log would say 0.
func (rec *recorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.ResponseWriter.Write(b)
}

func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// logRequests writes one line per request: what was asked for, what came back,
// how long it took, and the identifier that ties it to anything else logged
// while it ran.
//
// /up is skipped. kamal-proxy asks for it every second or two, and a health
// check is the only thing in the log that is never worth reading — the same
// reason production.rb sets silence_healthcheck_path.
func logRequests(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == healthCheckPath {
				next.ServeHTTP(w, r)
				return
			}

			started := time.Now()
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			logger.InfoContext(r.Context(), "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(started),
				"request_id", requestIDFrom(r.Context()))
		})
	}
}

// recoverPanics turns a panic in a handler into a 500 page.
//
// A panic that escapes crashes the whole process, which for a single-binary
// server means every other request in flight dies with it. Catching it here
// costs one deferred function per request and turns "the site is down" into
// "one page is broken, and here is the stack in the log".
//
// The body is public/500.html, the same file Rails served. A panic can come
// from the template layer, and rendering a page to apologize for a rendering
// failure is how one error becomes two.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			problem := recover()
			if problem == nil {
				return
			}
			// ErrAbortHandler is net/http's way for a handler to give up on a
			// connection deliberately. The server already logs it, and it is
			// not a failure of ours.
			if problem == http.ErrAbortHandler {
				panic(problem)
			}
			s.log.ErrorContext(r.Context(), "panic in handler",
				"error", problem,
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", requestIDFrom(r.Context()),
				"stack", stackTrace())
			s.serveErrorPage(w, http.StatusInternalServerError, "/500.html")
		}()
		next.ServeHTTP(w, r)
	})
}

// hstsHeader is what Rails' force_ssl sent, spelled out rather than assembled:
// ActionDispatch::SSL's defaults are two years, subdomains included, no
// preload list. Changing it is changing a promise browsers cache for two
// years, so it is worth having the number in one obvious place.
const hstsHeader = "max-age=63072000; includeSubDomains"

// strictTransportSecurity adds the header in production and nowhere else.
//
// There is no http-to-https redirect to go with it, and there was none in
// Rails either: production.rb sets assume_ssl. By the time a request reaches
// the app, kamal-proxy has already terminated TLS, so a redirect never fires.
// Sending the header in development is worse than useless. A browser told
// that localhost is HTTPS-only stays told, for two years, for every other
// project on the machine.
func strictTransportSecurity(enabled bool) middleware {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", hstsHeader)
			next.ServeHTTP(w, r)
		})
	}
}

// methodOverride lets an HTML form send a DELETE or a PATCH.
//
// Browsers only ever submit GET and POST, so Rails' button_to writes
// <input type="hidden" name="_method" value="delete"> and Rack::MethodOverride
// rewrites the request before routing. The markup is unchanged here — it is
// part of what the parity check compares — so the rewrite has to happen too.
//
// The rules are Rack's, and each of them matters. This overrides only a
// POST, because otherwise someone can turn a GET link into one that deletes
// something. It reads only the form body, not a header or the query string,
// for the same reason. And it only accepts the three methods a form can
// plausibly mean.
func methodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "" {
			if mediaType, _, _ := mime.ParseMediaType(contentType); mediaType != "application/x-www-form-urlencoded" {
				next.ServeHTTP(w, r)
				return
			}
		}
		// ParseForm caches its result on the request, so the handler that runs
		// next reads the same parsed body rather than an empty one: the body
		// itself has already been consumed by the time it gets there.
		if err := r.ParseForm(); err != nil {
			next.ServeHTTP(w, r)
			return
		}
		switch method := r.PostForm.Get("_method"); method {
		case "put", "PUT", "patch", "PATCH", "delete", "DELETE":
			r.Method = strings.ToUpper(method)
		}
		next.ServeHTTP(w, r)
	})
}

// crossOriginProtection is what replaces Rails' authenticity tokens.
//
// A token in every form was the old way to tell "this form was served by us"
// from "this form is on someone else's page". Browsers now say so themselves,
// in Sec-Fetch-Site, and net/http reads it. Safe methods pass — GET must not
// change anything, which is the rule this depends on — and so do same-origin
// requests, which is every fetch the Stimulus controllers make. The forms no
// longer carry a hidden token, and that is the one deliberate difference from
// Rails' markup.
func crossOriginProtection() middleware {
	protection := http.NewCrossOriginProtection()
	return protection.Handler
}

// stackTrace is the goroutine's stack, for the panic log line.
func stackTrace() string {
	buf := make([]byte, 8192)
	return string(buf[:runtime.Stack(buf, false)])
}
