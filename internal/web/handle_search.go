package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
	"github.com/jaimerodas/tinystart/internal/tinylinks"
)

// The command bar's two calls to the other app, proxied here so that the token
// never reaches the browser and there is no CORS to configure.
//
// Both degrade to nothing: federated results are a bonus on top of the local
// tiles, so an app that is slow, absent or cross about the token must never
// break the command bar. Search answers an empty array and a visit answers 204
// whatever went wrong.

// handleSearch is GET /search.json (and GET /search, which Rails answered the
// same way — the controller renders JSON whatever format was asked for).
//
// The body is a bare array, not an envelope: that is the shape
// command_bar_controller.js already reads.
func (s *Server) handleSearch() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.renderJSON(w, r, s.federatedSearch(r, r.URL.Query().Get("q")))
	})
}

// handleVisitCreate is POST /visits: a click on a federated result, forwarded
// to the app the link belongs to. Local tiles are counted by
// handleItemVisit instead.
//
// Fire and forget, and 204 either way — the browser has already navigated away
// and there is nothing it can do with a failure.
func (s *Server) handleVisitCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The id arrives in the query string, which is where track_visit.js
		// puts it. Reading the form as well costs nothing and means a plain
		// form post works too.
		linkID := r.URL.Query().Get("link_id")
		if linkID == "" {
			linkID = r.PostFormValue("link_id")
		}

		connection, client := s.connectedClient(r)
		if client != nil {
			if err := client.RecordVisit(r.Context(), linkID); err != nil {
				s.recordSearchFailure(r.Context(), connection, err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// federatedSearch runs one query and applies the recording contract the
// tinylinks package doc sets out: a success clears a note left by a previous
// failure, a credential failure leaves one for the start page to show, and
// everything else is logged and forgotten.
//
// It never returns nil. `[]` and `null` are different JSON, and the command
// bar reads one of them.
func (s *Server) federatedSearch(r *http.Request, query string) []tinylinks.Link {
	ctx := r.Context()
	empty := []tinylinks.Link{}

	connection, client := s.connectedClient(r)
	if client == nil {
		return empty
	}

	links, err := client.Search(ctx, query)
	if err != nil {
		s.recordSearchFailure(ctx, connection, err)
		return empty
	}

	if err := s.db.ClearConnectionFailure(ctx, connection.ID); err != nil {
		s.log.ErrorContext(ctx, "clearing a connection failure", "error", err, "connection_id", connection.ID)
	}
	if links == nil {
		return empty
	}
	return links
}

// recordSearchFailure is the other half of that contract. A rejected token is
// the one failure worth surfacing, because a lapsed credential and an empty
// archive look identical from the command bar. Everything else is the other
// app's problem and only reaches the log.
func (s *Server) recordSearchFailure(ctx context.Context, connection *store.Connection, err error) {
	switch {
	case tinylinks.NeedsReconnect(err):
		if writeErr := s.db.RecordConnectionFailure(ctx, connection.ID, err.Error()); writeErr != nil {
			s.log.ErrorContext(ctx, "recording a connection failure", "error", writeErr, "connection_id", connection.ID)
		}
	case errors.Is(err, tinylinks.ErrEmptyQuery), errors.Is(err, tinylinks.ErrEmptyLinkID):
		// Nothing was asked, so nothing failed. The command bar fires on
		// every keystroke, backspace included.
	default:
		s.log.WarnContext(ctx, "federated search failed", "error", err, "connection_id", connection.ID)
	}
}

// connectedClient builds a client for the current user's connection, and
// returns nil when there is none.
//
// The current user's, always: a token grants access to exactly one account on
// the other app, so the lookup is by user and never "the connection that
// happens to exist". Getting that wrong put one person's archive in another's
// command bar once already.
func (s *Server) connectedClient(r *http.Request) (*store.Connection, *tinylinks.Client) {
	user := userFrom(r.Context())
	connection, err := s.db.ConnectionForUser(r.Context(), user.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.ErrorContext(r.Context(), "reading the connection", "error", err, "user_id", user.ID)
		}
		return nil, nil
	}
	return connection, tinylinks.NewClient(connection.BaseURL, connection.Token, s.cfg.HTTPClient)
}
