package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
	"github.com/jaimerodas/tinystart/internal/tinylinks"
)

// Settings → Connections: one user's credential for another app, obtained
// through that app's device flow.
//
// There is no admin gate here. Connecting your own account on the other app
// needs no privilege. What keeps one person's archive out of another's
// command bar is that every lookup on this page is by the current user,
// never "the connection that happens to exist".

// defaultBaseURL is the address the form offers when nothing else is known.
const defaultBaseURL = "https://links.pati.to"

// pendingGrant is a device authorization that has been opened and not yet
// approved. Rails parked it in the session. There is no server-side session
// store here, so it goes in a signed cookie instead. That gives it the same
// lifetime with none of the machinery, because it is short-lived, single-use
// and needs no table.
//
// It is signed rather than encrypted: none of it is a secret. The device code
// is useless without the other app agreeing, and the signature is there so
// that nobody can point this app's poller at an address it did not choose.
type pendingGrant struct {
	DeviceCode      string    `json:"device_code"`
	VerificationURL string    `json:"verification_url"`
	BaseURL         string    `json:"base_url"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// connectionsData is the page in each of its three states. At most one of
// Pending and Connected is set; when neither is, the form is shown, with
// LastError above it if the last token was turned down.
type connectionsData struct {
	Nav       []settingsNavItem
	Pending   *pendingGrant
	Connected *connectedView
	LastError string
	BaseURL   string
}

// connectedView is the box that describes a working connection: what it is
// connected to, what the token can do, and how long it has left.
type connectedView struct {
	BaseURL  string
	Hostname string
	Scopes   string
	Expiry   string
}

// handleConnections is GET /settings/connections.
func (s *Server) handleConnections() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		connection, err := s.currentConnection(r)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		data := connectionsData{
			Nav:     settingsNav(user, "Connections"),
			Pending: s.pendingGrant(w, r),
			BaseURL: defaultBaseURL,
		}
		if connection != nil {
			data.BaseURL = connection.BaseURL
			data.LastError = connection.LastError
			if !connection.NeedsReconnect() {
				data.Connected = &connectedView{
					BaseURL:  connection.BaseURL,
					Hostname: connection.Hostname(),
					Scopes:   scopeList(connection.Scopes),
					Expiry:   s.tokenExpiry(connection),
				}
			}
		}

		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsConnections, data)
	})
}

// handleConnectionCreate is POST /settings/connections: it opens a grant on
// the other app and parks it while the browser polls.
func (s *Server) handleConnectionCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseURL, err := s.requestedBaseURL(r)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		// r.Host is host_with_port, so a laptop names its port and the real
		// thing does not: "tinystart (localhost:3000)" against
		// "tinystart (start.pati.to)" in the other app's list of tokens.
		flow := tinylinks.NewDeviceFlow(baseURL, r.Host, s.cfg.HTTPClient)
		grant, err := flow.Start(r.Context())
		if err != nil {
			s.log.WarnContext(r.Context(), "opening a device grant", "error", err, "base_url", baseURL)
			s.redirect(w, r, "/settings/connections", flashAlert,
				"Could not reach "+baseURL+". Check the address and try again.")
			return
		}

		expiresAt := s.now().Add(time.Duration(grant.ExpiresIn) * time.Second)
		if err := s.setPendingGrant(w, pendingGrant{
			DeviceCode:      grant.DeviceCode,
			VerificationURL: grant.VerificationURL,
			BaseURL:         baseURL,
			ExpiresAt:       expiresAt,
		}); err != nil {
			s.serverError(w, r, err)
			return
		}

		s.redirect(w, r, "/settings/connections", "", "")
	})
}

// handleConnectionPoll is GET /settings/connections/poll, which
// device_flow_controller.js calls every few seconds.
//
// It never redirects: the page decides what to do with the answer. The five
// statuses are the ones that controller switches on, so they are a wire format
// rather than a set of names.
func (s *Server) handleConnectionPoll() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grant := s.pendingGrant(w, r)
		if grant == nil {
			s.renderJSON(w, r, map[string]string{"status": "idle"})
			return
		}

		// No client host: only Start sends a name, and Check does not.
		flow := tinylinks.NewDeviceFlow(grant.BaseURL, "", s.cfg.HTTPClient)
		status, token, err := flow.Check(r.Context(), grant.DeviceCode)
		if err != nil {
			s.log.WarnContext(r.Context(), "checking a device grant", "error", err, "base_url", grant.BaseURL)
		}

		switch status {
		case tinylinks.StatusApproved:
			user := userFrom(r.Context())
			_, err := s.db.ReplaceConnection(r.Context(), user.ID,
				grant.BaseURL, token.Token, token.Scopes.String(), token.ExpiresAt)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.clearPendingGrant(w)
			s.renderJSON(w, r, map[string]string{"status": "connected"})

		case tinylinks.StatusPending, tinylinks.StatusUnreachable:
			// An unreachable app mid-flow is usually a blip, not a refusal.
			// Keep waiting until the grant expires on its own.
			s.renderJSON(w, r, map[string]string{"status": "pending"})

		default:
			s.clearPendingGrant(w)
			s.renderJSON(w, r, map[string]string{"status": string(status)})
		}
	})
}

// handleConnectionDestroy is DELETE /settings/connections.
func (s *Server) handleConnectionDestroy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())
		if err := s.db.DeleteConnection(r.Context(), user.ID); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.clearPendingGrant(w)
		s.redirect(w, r, "/settings/connections", flashNotice, "Disconnected.")
	})
}

// currentConnection is the signed-in user's connection, or nil. Having none is
// the ordinary state of an account, so it is not an error.
func (s *Server) currentConnection(r *http.Request) (*store.Connection, error) {
	user := userFrom(r.Context())
	connection, err := s.db.ConnectionForUser(r.Context(), user.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return connection, err
}

// requestedBaseURL is what to connect to: what the form said, or what is
// already connected, or the app this one is usually pointed at.
func (s *Server) requestedBaseURL(r *http.Request) (string, error) {
	if given := strings.TrimSpace(r.PostFormValue("base_url")); given != "" {
		return given, nil
	}
	connection, err := s.currentConnection(r)
	if err != nil {
		return "", err
	}
	if connection != nil {
		return connection.BaseURL, nil
	}
	return defaultBaseURL, nil
}

// tokenExpiry is the "token expires … from now" half of the facts line, and is
// empty for a token the other app gave no expiry for.
func (s *Server) tokenExpiry(connection *store.Connection) string {
	if connection.TokenExpiresAt.IsZero() {
		return ""
	}
	return distanceOfTimeInWords(s.now(), connection.TokenExpiresAt)
}

// scopeList is Connection#scope_list: the scopes as prose. They are stored the
// way the other app sends them — "search,visit" — which is not how a comma is
// written in a sentence. scopeList describes a token with none of them at
// all, rather than leaving it blank.
func scopeList(scopes string) string {
	var named []string
	for scope := range strings.SplitSeq(scopes, ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			named = append(named, scope)
		}
	}
	if len(named) == 0 {
		return "full access"
	}
	return strings.Join(named, ", ")
}

// pendingGrant reads the grant in flight, and reports none for a cookie that
// is absent, unreadable or past its expiry.
//
// It drops a grant here that ran out while the tab sat open, rather than
// anywhere else. That is why this takes a ResponseWriter: the page that
// notices is the page that clears it.
func (s *Server) pendingGrant(w http.ResponseWriter, r *http.Request) *pendingGrant {
	value, err := s.readSignedCookie(r, connectionGrantCookie)
	if err != nil {
		return nil
	}

	var grant pendingGrant
	if err := json.Unmarshal([]byte(value), &grant); err != nil || grant.DeviceCode == "" {
		s.clearPendingGrant(w)
		return nil
	}
	if !grant.ExpiresAt.After(s.now()) {
		s.clearPendingGrant(w)
		return nil
	}
	return &grant
}

func (s *Server) setPendingGrant(w http.ResponseWriter, grant pendingGrant) error {
	value, err := json.Marshal(grant)
	if err != nil {
		return err
	}
	// noExpiry, and the expiry checked on the way out instead. A cookie's
	// lifetime relies on the browser's clock. If that clock runs a few
	// minutes behind, the browser keeps polling a grant that this app has
	// already given up on. Carrying the deadline inside the value instead
	// means the server decides. The cookie is dropped the moment a read
	// notices it has passed.
	s.setSignedCookie(w, connectionGrantCookie, string(value), noExpiry)
	return nil
}

func (s *Server) clearPendingGrant(w http.ResponseWriter) {
	s.deleteCookie(w, connectionGrantCookie)
}
