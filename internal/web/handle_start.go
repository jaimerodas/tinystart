package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The two start page templates, named so a typo is a compile error rather than
// a 500 the first time a stream is rendered.
const (
	pageStartShow = "start_show"
	pageStartEdit = "start_edit"
)

// startShowData is the start page: the command bar's payload, the grid, and
// what to say about the connection the bar federates to.
type startShowData struct {
	// LinksJSON is every tile the user owns, embedded so the bar can filter
	// without a round trip.
	LinksJSON string
	// HasTiles decides the "nothing here yet" line. It is about tiles and not
	// about groups: a page of empty groups still looks broken.
	HasTiles bool
	// Federation is what the bar does about its "All Links" section, and
	// Source names the host it asks. Reconnect and Hostname are the
	// notice above the bar, which exists because a lapsed token and an empty
	// archive look identical from the results alone.
	Federation string
	Source     string
	Reconnect  bool
	Hostname   string

	Shortcuts   []shortcut
	ColumnCount int
	Columns     []showColumn
}

// showColumn and showGroup are the read-only grid. It shares nothing with the
// editor's columnView: this one has no ids, no forms and no drag targets,
// because the page it draws has none.
type showColumn struct {
	Groups []showGroup
}

type showGroup struct {
	Name  string
	Items []store.Item
}

// handleStartPage is GET /. The start page is served here and nowhere else —
// /start survives as the PATCH target and as the prefix the group and item
// routes hang off, and a GET there is a 404 on purpose.
func (s *Server) handleStartPage() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		links, err := s.db.LinksForCommandBar(ctx, user.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		encoded, err := json.Marshal(links)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		byColumn, err := s.db.GroupsByColumn(ctx, user.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		connection, err := s.connectionFor(ctx, user.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		data := startShowData{
			LinksJSON:   string(encoded),
			HasTiles:    len(links) > 0,
			Federation:  federationState(connection),
			Shortcuts:   showPageShortcuts,
			ColumnCount: user.Columns,
		}
		if connection != nil {
			data.Source = connection.Hostname()
			data.Reconnect = connection.NeedsReconnect()
			data.Hostname = connection.Hostname()
		}

		for column := 1; column <= user.Columns; column++ {
			shown := showColumn{}
			for _, group := range byColumn[column] {
				items, err := s.db.ItemsInGroup(ctx, group.ID)
				if err != nil {
					s.serverError(w, r, err)
					return
				}
				shown.Groups = append(shown.Groups, showGroup{Name: group.Name, Items: items})
			}
			data.Columns = append(data.Columns, shown)
		}

		s.render(w, r, http.StatusOK, layoutStart, pageStartShow, data)
	})
}

// connectionFor is "the user's connection, or none". Having no connection is
// the ordinary state of an account that has not linked anything, so
// ErrNotFound is not an error here — it is the answer.
//
// It is per user and never app-wide: the token grants access to exactly one
// account over there, and an app-wide lookup leaks one person's results into
// another's command bar.
func (s *Server) connectionFor(ctx context.Context, userID int64) (*store.Connection, error) {
	connection, err := s.db.ConnectionForUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return connection, nil
}
