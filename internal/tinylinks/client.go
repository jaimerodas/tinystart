package tinylinks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MaxResults is how many federated links the command bar will show, and so
// both what is asked for and what is kept.
const MaxResults = 10

// Link is one result, trimmed to what the command bar renders. The other app
// sends a description, tags, and a visit count with each one. This package
// does not show them, and dropping them here keeps /search.json the shape
// the browser already expects.
//
// The tags are the JSON the browser receives, not the JSON the other app
// sends, because this struct is handed straight to the search endpoint's
// encoder. ID is a number for the same reason: the other app numbers its
// links, and the command bar puts this value back into /visits?link_id=.
type Link struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Client talks to one connected app on behalf of one user's connection.
//
// One user's, always: a token grants access to exactly one account over there,
// so a Client built from one person's connection must never serve another's
// command bar.
type Client struct {
	base  *url.URL
	token string
	host  string
	http  *http.Client
}

// NewClient builds a client for a connection's base URL and token. Pass a
// *http.Client to control the timeouts, or nil for DefaultHTTPClient.
func NewClient(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = DefaultHTTPClient()
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		// A base URL that does not parse cannot be reached either. The
		// request built from the empty one below fails with a message that
		// names "the connected app". Rails said the same thing when
		// Connection#hostname came back nil.
		base = &url.URL{}
	}

	return &Client{base: base, token: token, host: hostLabel(base), http: httpClient}
}

// Search asks the other app for links that match query, capped at MaxResults
// and in the order it returned them.
//
// The error is the whole outcome. It is a *StatusError when the app answered
// and said no. It is a wrapped transport error when it did not answer. It is
// ErrEmptyQuery when there was nothing to ask. A nil error means the
// credential works, which is what clears a recorded failure. Any error at all
// means show no federated results — never a broken command bar.
func (c *Client) Search(ctx context.Context, query string) ([]Link, error) {
	if strings.TrimSpace(query) == "" {
		return nil, ErrEmptyQuery
	}

	body, err := c.do(ctx, http.MethodGet, "/api/v1/search", url.Values{
		"q":        {query},
		"per_page": {strconv.Itoa(MaxResults)},
	})
	if err != nil {
		return nil, err
	}

	// A body with no "links" key is a success with nothing in it, the way
	// Rails' fetch("links", []) was: an app that has an empty archive and one
	// that phrases its emptiness oddly both mean the same to the command bar.
	var envelope struct {
		Links []Link `json:"links"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("%s returned something that isn't JSON: %w", c.host, err)
	}

	// per_page above already asks for ten. This is the other app that keeps
	// its side of that bargain, checked rather than trusted.
	return envelope.Links[:min(len(envelope.Links), MaxResults)], nil
}

// RecordVisit forwards a click on a federated result to the app the link
// belongs to. Fire and forget: the browser already navigated away, and
// nothing on either side waits for this.
//
// linkID is a string because it arrives as one, from the query string the
// browser sent.
func (c *Client) RecordVisit(ctx context.Context, linkID string) error {
	if strings.TrimSpace(linkID) == "" {
		return ErrEmptyLinkID
	}

	// Escaped, so that an id from the query string cannot walk out of
	// /api/v1/links and post to some other endpoint. Rails interpolated it raw.
	_, err := c.do(ctx, http.MethodPost, "/api/v1/links/"+url.PathEscape(linkID)+"/visit", nil)
	return err
}

// do makes the request and returns the body of a successful reply. Every
// failure it can see becomes an error. The callers turn those into empty
// results.
func (c *Client) do(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	// ResolveReference against an absolute path is Ruby's URI.join: a base URL
	// that carries a path of its own has it replaced, not appended to.
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.host, err)
	}
	target := c.base.ResolveReference(ref)
	target.RawQuery = params.Encode()

	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.host, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, c.unreachable(err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, c.unreachable(err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, &StatusError{Status: response.StatusCode, Host: c.host}
	}
	return body, nil
}

// unreachable wraps a transport failure in the sentence Rails logged for it.
// The distinction between the two is worth keeping: a timeout means the other
// app is slow. Anything else means the app is not there at all.
func (c *Client) unreachable(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s timed out: %w", c.host, err)
	}
	return fmt.Errorf("could not reach %s: %w", c.host, err)
}

// hostLabel is Connection#hostname: what the messages call the other end. A
// person reads them — on the start page for a rejected token, in the log
// otherwise — so they name the host rather than a product.
func hostLabel(base *url.URL) string {
	if host := base.Hostname(); host != "" {
		return host
	}
	return "the connected app"
}
