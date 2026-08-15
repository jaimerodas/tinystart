package tinylinks

import (
	"errors"
	"fmt"
	"net/http"
)

// The two "there was nothing to ask about" errors. Neither one is a failure of
// the other app — no request was made — so a caller that clears a recorded
// failure on success has to tell them apart from one. Rails returned an empty
// array and false here, which said nothing about which of the two it was.
var (
	// ErrEmptyQuery comes back from a search for nothing. The command bar
	// fires on every keystroke, backspace included.
	ErrEmptyQuery = errors.New("tinylinks: empty query")

	// ErrEmptyLinkID comes back from a visit with no link to record.
	ErrEmptyLinkID = errors.New("tinylinks: empty link id")
)

// StatusError is a reply that was not a success. Its message is what Rails
// logged, and — for the two that need a reconnect — what it wrote to
// connections.last_error for the start page to show, so the wording is not
// free to drift.
type StatusError struct {
	Status int
	// Host names the other end the way a person would: the bare hostname of
	// the base URL, or "the connected app" when there isn't one.
	Host string
}

func (e *StatusError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("%s rejected the token — reconnect to restore search", e.Host)
	case http.StatusForbidden:
		return fmt.Sprintf("the %s token is missing a scope — reconnect to restore search", e.Host)
	default:
		return fmt.Sprintf("%s answered %d", e.Host, e.Status)
	}
}

// NeedsReconnect is true for the statuses that mean the credential is the
// problem. A bad gateway or a 500 is the other app's problem, not a credential
// problem; asking the user to reconnect would be wrong.
func (e *StatusError) NeedsReconnect() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// NeedsReconnect reports whether err is the kind of failure the user can fix,
// which is the one question the web layer asks of an error from this package.
func NeedsReconnect(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && status.NeedsReconnect()
}
