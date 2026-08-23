// Package postmark sends mail through Postmark's HTTPS API.
//
// It is the whole of what postmark-rails did for this app, which is one
// message: the password reset. The web layer renders the mail itself —
// subject, both bodies, the link with the token in it. This package only
// puts it on the wire.
//
// There is no SMTP, no queue and no retry. A reset that fails to send is a
// reset the person asks for again.
package postmark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is Postmark's API. It is a field on the Client rather
	// than a constant in the code so tests can point at an httptest server.
	DefaultBaseURL = "https://api.postmarkapp.com"

	// DefaultFrom is the sender the app always used — ApplicationMailer's
	// `default from:`. Postmark refuses anything that is not a verified
	// signature on the account. As a result, this is not a free choice.
	DefaultFrom = "noreply@rodas.mx"

	// TestToken is Postmark's own sandbox token: their API accepts a message
	// sent with it, validates it, and delivers nothing. Useful for a smoke
	// test by hand against the real API. The test suite uses a fake server
	// instead, so nothing in it ever leaves the machine.
	TestToken = "POSTMARK_API_TEST"
)

// maxResponseBytes caps how much this package reads back. Postmark answers
// with a small JSON object. Anything larger is a proxy or an error page, and
// this package wants only the status from it.
const maxResponseBytes = 1 << 16

// Client is an account's server token and the way to reach Postmark. The zero
// value plus a token is production configuration: both other fields have
// defaults.
type Client struct {
	// Token is the server token, from the environment. It is a per-server
	// credential, so it belongs in configuration and never in the repository.
	Token string

	// HTTP is the client to send with. Nil means a default one with a
	// timeout, which matters because a request without one waits forever.
	HTTP *http.Client

	// BaseURL overrides Postmark's API, for tests.
	BaseURL string
}

// Message is one mail. Postmark's field names are not Go's — HtmlBody, in
// particular — so every one of them is spelled out in a tag.
//
// The two bodies are optional individually and required together: Postmark
// refuses a message with neither.
type Message struct {
	From     string `json:"From"`
	To       string `json:"To"`
	Subject  string `json:"Subject"`
	TextBody string `json:"TextBody,omitempty"`
	HTMLBody string `json:"HtmlBody,omitempty"`
}

// Error means that Postmark declined to send the message. Status is the HTTP
// status. ErrorCode and Message are Postmark's own, from the JSON body, and
// are what the log needs to say what to fix. Code 300 is a malformed
// address, 406 an inactive recipient, and 401 a token that is not this
// server's.
//
// Both can be empty: a failure from something in front of the API answers
// however it likes, and the status is then all there is.
type Error struct {
	Status    int    `json:"-"`
	ErrorCode int    `json:"ErrorCode"`
	Message   string `json:"Message"`
}

func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("postmark answered %d", e.Status)
	}
	return fmt.Sprintf("postmark answered %d: error %d, %s", e.Status, e.ErrorCode, e.Message)
}

// Send delivers one message. A non-2xx reply comes back as an *Error carrying
// whatever Postmark said about it. Any failure that keeps the request from
// getting an answer at all comes back wrapped.
func (c *Client) Send(ctx context.Context, message Message) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("sending mail: %w", err)
	}

	endpoint := strings.TrimSuffix(c.baseURL(), "/") + "/email"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending mail: %w", err)
	}
	request.Header.Set("X-Postmark-Server-Token", c.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient().Do(request)
	if err != nil {
		return fmt.Errorf("sending mail through %s: %w", endpoint, err)
	}
	defer response.Body.Close()

	reply, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("sending mail through %s: %w", endpoint, err)
	}
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return nil
	}

	// A body that is not Postmark's JSON leaves the two fields zero, which is
	// exactly what Error prints around when there is nothing to add.
	apiError := &Error{Status: response.StatusCode}
	json.Unmarshal(reply, apiError)
	return apiError
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return c.BaseURL
}

// httpClient is the caller's, or one that gives up after thirty seconds —
// the read timeout postmark-rails used. That is long enough that a slow API
// is never the reason a reset mail does not arrive.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}
