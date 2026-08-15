package tinylinks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The grant this app asks for, and who it says it is.
const (
	// RequestedScopes are the two things this app does with the token:
	// search from the command bar, and record the visits that follow.
	RequestedScopes = "search,visit"

	appName = "tinystart"
)

// Status is where a device authorization got to. The strings are the ones the
// poll endpoint renders into {"status": "…"} and device_flow_controller.js
// switches on, so they are part of the wire format, not just names.
type Status string

const (
	StatusApproved Status = "approved"
	StatusPending  Status = "pending"
	StatusDenied   Status = "denied"
	StatusExpired  Status = "expired"

	// StatusUnreachable is a blip mid-flow, and deliberately not a denial:
	// the page keeps waiting until the grant runs out on its own.
	StatusUnreachable Status = "unreachable"
)

// Grant is an opened device authorization: the code to poll with, the page to
// send the person to, and how long and how often to keep asking.
type Grant struct {
	DeviceCode      string
	VerificationURL string
	ExpiresIn       int
	Interval        int
}

// Token is an approved grant. Its three fields are the three columns
// store.ReplaceConnection wants, in the shapes it wants them.
type Token struct {
	Token     string
	Scopes    ScopeList
	ExpiresAt time.Time
}

// ScopeList is the scopes a token carries. It decodes either shape the other
// app might send — the list it does send, or a bare "search,visit" string —
// because Rails' Array(token["scopes"]) accepted both and a token is too
// expensive to lose over punctuation.
type ScopeList []string

// String is the form the database stores, which is the form the other app
// sends: comma-joined, no spaces.
func (s ScopeList) String() string { return strings.Join(s, ",") }

func (s *ScopeList) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*s = list
		return nil
	}

	var joined string
	if err := json.Unmarshal(data, &joined); err != nil {
		return fmt.Errorf("scopes are neither a list nor a string: %s", data)
	}
	for scope := range strings.SplitSeq(joined, ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			*s = append(*s, scope)
		}
	}
	return nil
}

// DeviceFlow drives a connected app's OAuth 2.0 Device Authorization Grant
// (RFC 8628) so this app can get its own scoped token.
//
// Deliberately non-blocking: Start opens a grant, Check reports where it got
// to. The waiting happens in the browser, not here.
type DeviceFlow struct {
	baseURL    string
	clientHost string
	http       *http.Client
}

// NewDeviceFlow points a flow at the other app.
//
// clientHost is this app's own host, not the other app's — see clientName. It
// is optional, because only Start sends a name and Check does not. Pass a
// *http.Client to control the timeouts, or nil for DefaultHTTPClient.
func NewDeviceFlow(baseURL, clientHost string, httpClient *http.Client) *DeviceFlow {
	if httpClient == nil {
		httpClient = DefaultHTTPClient()
	}
	return &DeviceFlow{baseURL: baseURL, clientHost: clientHost, http: httpClient}
}

// Start opens a grant. The error is for the log and for the "could not reach"
// message on the connections page; the caller needs nothing from it but the
// fact that there is no grant.
func (f *DeviceFlow) Start(ctx context.Context) (*Grant, error) {
	var reply struct {
		DeviceCode      string `json:"device_code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
	}

	err := f.post(ctx, "/api/v1/device_authorizations", url.Values{
		"client_name": {f.clientName()},
		"scopes":      {RequestedScopes},
	}, &reply)
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, fmt.Errorf("%s refused the grant: %s", f.baseURL, reply.Error)
	}

	return &Grant{
		DeviceCode:      reply.DeviceCode,
		VerificationURL: reply.VerificationURL,
		ExpiresIn:       reply.ExpiresIn,
		Interval:        reply.Interval,
	}, nil
}

// Check asks where a grant got to. The token comes back only with
// StatusApproved; the error only with StatusUnreachable, where it says what
// went wrong for the log.
func (f *DeviceFlow) Check(ctx context.Context, deviceCode string) (Status, *Token, error) {
	var reply struct {
		Token     string     `json:"token"`
		Scopes    ScopeList  `json:"scopes"`
		ExpiresAt *time.Time `json:"expires_at"`
		Error     string     `json:"error"`
	}

	err := f.post(ctx, "/api/v1/device_authorizations/token", url.Values{
		"device_code": {deviceCode},
	}, &reply)
	if err != nil {
		return StatusUnreachable, nil, err
	}

	switch reply.Error {
	case "":
		token := &Token{Token: reply.Token, Scopes: reply.Scopes}
		if reply.ExpiresAt != nil {
			token.ExpiresAt = reply.ExpiresAt.UTC()
		}
		return StatusApproved, token, nil
	case "authorization_pending":
		return StatusPending, nil, nil
	case "access_denied":
		return StatusDenied, nil, nil
	default:
		// expired_token, and anything else the other app invents: a status
		// this app cannot act on is the end of the grant, whatever it is
		// called, and the page says so and stops polling.
		return StatusExpired, nil, nil
	}
}

// clientName is what the other app lists this app's token under.
//
// One person can easily have two tinystarts pointed at the same app — a laptop
// and the real thing. Without the host, both tokens read "tinystart" and
// revoking the right one is guesswork. Falls back to the bare name when the
// host isn't known, which is no worse than it used to be.
func (f *DeviceFlow) clientName() string {
	if f.clientHost == "" {
		return appName
	}
	return appName + " (" + f.clientHost + ")"
}

// post sends the form the other app expects and decodes the JSON it answers
// with into reply.
//
// The HTTP status is deliberately not looked at. RFC 8628 has the token
// endpoint answer 400 for every state that is not approval — pending included
// — so the "error" field in the body is the only thing that decides, exactly
// as it was in Ruby, which never read the code either.
func (f *DeviceFlow) post(ctx context.Context, path string, params url.Values, reply any) error {
	base, err := url.Parse(f.baseURL)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", f.baseURL, err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", f.baseURL, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base.ResolveReference(ref).String(), strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", f.baseURL, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := f.http.Do(request)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", f.baseURL, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", f.baseURL, err)
	}
	if err := json.Unmarshal(body, reply); err != nil {
		return fmt.Errorf("%s returned something that isn't JSON: %w", f.baseURL, err)
	}
	return nil
}
