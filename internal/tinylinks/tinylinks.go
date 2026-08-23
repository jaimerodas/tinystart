// Package tinylinks talks to the app on the other side of a connection: the
// federated search behind the command bar, the visits that follow from it, and
// the device flow that got the token in the first place.
//
// Two rules shape everything here.
//
// The first is that federated search is a bonus on top of the local tiles. As
// a result, a slow or absent app must never break the start page or hold up a
// keystroke. Every call ends in either a result or an error — never a panic,
// never a wait longer than the timeouts below. Every caller must carry on
// with an empty list.
//
// The second is that this package does not know the database exists. Rails'
// ConnectionClient wrote to the connection itself. It recorded a rejected
// token so the page can say "reconnect". It cleared that note on the next
// success. Here those are outcomes rather than side effects — a *StatusError
// that NeedsReconnect reports on, and a nil error for the success. The web
// layer does the writing. That keeps the one package that speaks HTTP to the
// outside world testable with nothing but an httptest server. It also keeps
// the decision about what a failure means where the rest of the policy is.
//
// The web layer's half of the bargain, which is what Rails did:
//
//	links, err := client.Search(ctx, query)
//	switch {
//	case err == nil:
//		db.ClearConnectionFailure(ctx, connection.ID)
//	case tinylinks.NeedsReconnect(err):
//		db.RecordConnectionFailure(ctx, connection.ID, err.Error())
//	case !errors.Is(err, tinylinks.ErrEmptyQuery):
//		logger.Warn("search failed", "error", err)
//	}
//
// The messages on those errors are the ones a person reads on the start page,
// so they are Rails' wording word for word.
package tinylinks

import (
	"net"
	"net/http"
	"time"
)

// The timeouts Rails used: Net::HTTP.start(open_timeout: 2, read_timeout: 4).
// They matter because the command bar waits on a search while somebody types.
const (
	openTimeout = 2 * time.Second
	readTimeout = 4 * time.Second
)

// maxResponseBytes caps how much this package reads from the other app. Rails
// read whatever arrived. A cap is cheap insurance against an app that answers
// a search with something enormous. A truncated body degrades to "isn't
// JSON", which is already a path with a test.
const maxResponseBytes = 1 << 20

// DefaultHTTPClient is the client both DeviceFlow and Client use when they are
// given none — tests pass their own, pointed at a fake.
//
// The two Ruby timeouts do not map onto http.Client.Timeout, which is a
// deadline for the whole exchange including reading the body. They map onto
// the transport: open_timeout is the time to get a connection (dial, and the
// TLS handshake that follows it), read_timeout the time to wait for the other
// app to start answering. Nothing here streams, so there is no third case
// where a slow body can run past both timeouts without being cut off.
func DefaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: openTimeout}).DialContext,
			TLSHandshakeTimeout:   openTimeout,
			ResponseHeaderTimeout: readTimeout,
		},
	}
}
