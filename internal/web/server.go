// Package web is the HTTP half of TinyStart: routing, sessions, templates and
// the static assets. It knows nothing about SQL — everything it needs from the
// database it asks internal/store for — and it reaches the outside world only
// through Mailer, which is the one interface declared here.
//
// The shape is the one net/http suggests and no more: NewServer takes the
// dependencies and returns an http.Handler, addRoutes lists every URL in one
// place, handlers are methods that return an http.Handler, and middleware is
// func(http.Handler) http.Handler. There is no framework and no container to
// look inside.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
)

// healthCheckPath is what kamal-proxy polls, and the one path the request log
// stays quiet about. It is the path the Rails image answered, so the deploy
// configuration does not change.
const healthCheckPath = "/up"

// Config is everything about the deployment that the handlers need to know.
// Anything that is a dependency rather than a setting — the database, the
// logger, the mailer — is a parameter of NewServer instead.
type Config struct {
	// SecretKey signs the cookies and the password reset tokens. Rails used
	// secret_key_base for the same two jobs; this is a different key, so
	// everyone signs in once after the cutover.
	//
	// It must be at least 32 bytes, which is the output size of the hash
	// underneath: a shorter key is a shorter signature however long the MAC
	// looks. Generate one with `openssl rand -hex 32`.
	SecretKey []byte

	// SecureCookies marks every cookie Secure, and turns the HSTS header on.
	// It is true in production, where kamal-proxy terminates TLS. It is false
	// in development and in tests: a Secure cookie never gets stored without
	// HTTPS, and the pages appear to lose the session.
	SecureCookies bool

	// Host is the canonical host, used to build the absolute URLs that go in
	// mail. Empty means "take it from the request", which is right in
	// development and merely adequate in production. A request with a
	// forged Host header puts a forged link in a password reset mail, and
	// setting this closes that off.
	Host string

	// MailFrom is the envelope sender for everything the app sends. It is
	// ApplicationMailer's `default from:`.
	MailFrom string

	// HTTPClient is the client the tinylinks calls will use, once there are
	// any: it is here so that the tests in the phases that add federated
	// search can point them at an httptest server. Nil means the default.
	HTTPClient *http.Client
}

// minSecretKeyLength is the hash's output size. See Config.SecretKey.
const minSecretKeyLength = 32

// Mailer is the one thing the app needs from Postmark: send this message.
//
// It is an interface, and *postmark.Client satisfies it, so the handler tests
// hand it a recorder, which captures the message instead of sending it.
// Declaring it here rather than in the postmark package is
// the Go convention and the useful one: the consumer says what it needs, and
// the producer does not have to guess.
type Mailer interface {
	Send(ctx context.Context, message postmark.Message) error
}

// Server holds what the handlers share. It is not exported: NewServer returns
// an http.Handler, which is all a caller can do anything with, and every
// method here hangs off a value only this package can hold.
type Server struct {
	cfg       Config
	db        *store.DB
	log       *slog.Logger
	mailer    Mailer
	now       func() time.Time
	templates *renderer
	assets    *assetSet

	// The two rate limiters, one per policy. Both are per-IP and in memory.
	// See ratelimit.go for why that is enough.
	signIn *limiter
	signUp *limiter
}

// The rate limits, from config/routes.rb's two rate_limit declarations. Both
// answer the same way when they trip — back to the sign-in page with "Try
// again later." — which is what SessionsController and UsersController did.
const (
	signInLimit  = 10
	signInWindow = 3 * time.Minute
	signUpLimit  = 2
	signUpWindow = 5 * time.Minute
)

// NewServer builds the application.
//
// now is the clock, taken as a parameter so that the tests can move time
// forward without sleeping — the rate limiter's windows and the session
// refresh both depend on it. Pass nil for time.Now.
func NewServer(cfg Config, db *store.DB, logger *slog.Logger, mailer Mailer, now func() time.Time) (http.Handler, error) {
	s, err := newServer(cfg, db, logger, mailer, now)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	addRoutes(mux, s)
	return s.wrap(mux), nil
}

// newServer builds the Server without routing it.
//
// The split exists for the tests. They mount one extra route of their own —
// a page behind requireAuthentication, which no real page is yet. Then they
// wrap the result the same way NewServer does. Everything else about the
// server they get is the real thing.
func newServer(cfg Config, db *store.DB, logger *slog.Logger, mailer Mailer, now func() time.Time) (*Server, error) {
	if len(cfg.SecretKey) < minSecretKeyLength {
		return nil, errors.New("web: secret key must be at least 32 bytes")
	}
	if cfg.MailFrom == "" {
		cfg.MailFrom = postmark.DefaultFrom
	}
	if now == nil {
		now = time.Now
	}

	assets, err := newAssets()
	if err != nil {
		return nil, err
	}
	templates, err := newRenderer(assets)
	if err != nil {
		return nil, err
	}

	s := &Server{
		cfg:       cfg,
		db:        db,
		log:       logger,
		mailer:    mailer,
		now:       now,
		templates: templates,
		assets:    assets,
		signIn:    newLimiter(signInLimit, signInWindow, now),
		signUp:    newLimiter(signUpLimit, signUpWindow, now),
	}

	return s, nil
}

// wrap puts the middleware around the routes.
//
// The list below is the order a request meets them, outermost first, and each
// one is where it is because the one inside it can assume what it did:
//
//   - recoverPanics, so that a panic anywhere below is a 500 and not a dead
//     process. Outermost, because it has to catch the others too.
//   - requestID, so that everything logged for one request shares an
//     identifier, including the panic.
//   - logRequests, so that the line it writes reports the final status, after
//     every wrapper below has had its say.
//   - strictTransportSecurity, so the header is on every response, error
//     pages included.
//   - methodOverride, before routing, because it changes which route matches.
//   - crossOriginProtection, before any handler that writes anything. It looks
//     at whether the browser calls the request same-origin, not at the
//     method, so it does not mind a rewritten one.
//   - resumeSession, last, so that every handler — and the not-found page —
//     knows who is asking.
func (s *Server) wrap(routes http.Handler) http.Handler {
	stack := []middleware{
		s.recoverPanics,
		requestID,
		logRequests(s.log),
		strictTransportSecurity(s.cfg.SecureCookies),
		methodOverride,
		crossOriginProtection(),
		s.resumeSession,
	}

	// Wrapping happens inside out, so the list is walked backwards and the
	// first entry ends up outermost — which is the order it reads in.
	handler := routes
	for _, wrap := range slices.Backward(stack) {
		handler = wrap(handler)
	}
	return handler
}
