package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jaimerodas/tinystart/internal/store"
)

// startEditData is the editor: the ? dialog's rows, the toolbar's column
// picker, and the grid itself.
type startEditData struct {
	Shortcuts []shortcut
	Picker    columnCountView
	Grid      gridView
}

// gridView is #start_page_grid. No stream ever targets it — it carries the
// drag and keyboard controllers, and replacing it would drop the keyboard
// highlight on every move — so it is only ever drawn as part of a whole page.
type gridView struct {
	ColumnCount int
	Columns     []columnView
}

// columnCountView is the toolbar's column picker: every count on offer, and
// the one that is stored. 1 has to be on offer or the browser would preselect
// the first option and nobody could get back to a single column.
type columnCountView struct {
	Selected int
	Options  []int
}

func newColumnCountView(selected int) columnCountView {
	options := make([]int, 0, store.MaxColumns)
	for n := range store.MaxColumns {
		options = append(options, n+1)
	}
	return columnCountView{Selected: selected, Options: options}
}

// handleStartEdit is GET /start/edit.
func (s *Server) handleStartEdit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		byColumn, err := s.db.GroupsByColumn(ctx, user.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		grid := gridView{ColumnCount: user.Columns}
		for column := 1; column <= user.Columns; column++ {
			view, err := s.columnViewFrom(ctx, column, byColumn[column])
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			grid.Columns = append(grid.Columns, view)
		}

		s.render(w, r, http.StatusOK, layoutStart, pageStartEdit, startEditData{
			Shortcuts: editorShortcuts,
			Picker:    newColumnCountView(user.Columns),
			Grid:      grid,
		})
	})
}

// handleStartUpdate is PATCH /start: the column count, and nothing else — the
// rest of the user's preferences are Settings'.
func (s *Server) handleStartUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		// A field that is not a number reads as zero, which the store refuses
		// with "must be greater than 0" — the same answer Rails gave, by the
		// same route: it cast the parameter before it validated it.
		columns, _ := strconv.Atoi(r.PostFormValue("user[columns]"))

		err := s.db.UpdateColumns(ctx, user.ID, columns)
		if err == nil {
			// A full visit rather than a stream: every column moves, and
			// redrawing them one by one would mean replacing
			// #start_page_grid, the node that carries the drag and keyboard
			// controllers.
			s.redirect(w, r, "/start/edit", "", "")
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		// Only ever one message: the store checks the bounds first and the
		// stranded groups only when the bounds are fine, so joining is a
		// formality here rather than a difference from Rails' to_sentence.
		message := invalid.Error()

		if !wantsTurboStream(r) {
			// Without a stream to apply, the refusal has nowhere to land but
			// a flash — the same refusal, said the only way the page can hear
			// it.
			s.redirect(w, r, "/start/edit", flashAlert, message)
			return
		}

		notice, err := s.noticeStream(message)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		// The select is already showing the value the database refused, so
		// reporting is not enough — it has to be sent back as well, showing
		// the count that is actually stored.
		picker, err := s.renderPartial(pageStartEdit, "column_count", newColumnCountView(user.Columns))
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		s.writeTurboStream(w, http.StatusUnprocessableEntity,
			notice, replaceStream(columnCountDOMID, picker))
	})
}

// noticeStream is StartPageNotice#notice_stream, and the one place a refused
// move can say so.
//
// update, not replace: the region is a live one, and it is only announced for
// changes made inside it while it is already in the accessibility tree.
// Replacing it would hand the reader a region that already had its text, which
// is the shape screen readers stay quiet about.
func (s *Server) noticeStream(message string) (streamAction, error) {
	html, err := s.renderPartial(pageStartEdit, "error_message", message)
	if err != nil {
		return streamAction{}, err
	}
	return updateStream(noticeDOMID, html), nil
}

// The stream builders. Each renders the smallest node that can have changed,
// which is the rule the whole editor runs on: never widen a target back to
// #start_page_grid.

func (s *Server) columnStream(ctx context.Context, userID int64, column int) (streamAction, error) {
	view, err := s.columnViewFor(ctx, userID, column)
	if err != nil {
		return streamAction{}, err
	}
	html, err := s.renderPartial(pageStartEdit, "start_column", view)
	if err != nil {
		return streamAction{}, err
	}
	return replaceStream(view.DOMID(), html), nil
}

func (s *Server) groupStream(view groupView) (streamAction, error) {
	html, err := s.renderPartial(pageStartEdit, "start_group", view)
	if err != nil {
		return streamAction{}, err
	}
	return replaceStream(view.DOMID(), html), nil
}

func (s *Server) itemStream(view itemView) (streamAction, error) {
	html, err := s.renderPartial(pageStartEdit, "start_item", view)
	if err != nil {
		return streamAction{}, err
	}
	return replaceStream(view.DOMID(), html), nil
}

func (s *Server) newGroupStream(view newGroupView) (streamAction, error) {
	html, err := s.renderPartial(pageStartEdit, "start_new_group", view)
	if err != nil {
		return streamAction{}, err
	}
	return replaceStream(view.DOMID(), html), nil
}

func (s *Server) newItemStream(view newItemView) (streamAction, error) {
	html, err := s.renderPartial(pageStartEdit, "start_new_item", view)
	if err != nil {
		return streamAction{}, err
	}
	return replaceStream(view.DOMID(), html), nil
}

// groupStreamFor is groupStream for a group that has to be read back first —
// what every write that adds, removes or reorders tiles answers with, since a
// group owns its tile rows and their positions.
func (s *Server) groupStreamFor(ctx context.Context, userID, groupID int64) (streamAction, error) {
	group, err := s.db.GroupByID(ctx, userID, groupID)
	if err != nil {
		return streamAction{}, err
	}
	view, err := s.groupViewFor(ctx, *group)
	if err != nil {
		return streamAction{}, err
	}
	return s.groupStream(view)
}

// notFound is what an id that does not address one of this user's rows gets:
// the static page a bad URL gets, because "not yours" and "not there" have to
// be indistinguishable from the outside.
func (s *Server) notFound(w http.ResponseWriter) {
	s.serveErrorPage(w, http.StatusNotFound, "/404.html")
}

// pathID reads the {id} out of a route pattern. A path that cannot be a row id
// is not an error worth a page of its own — nothing has that id, so it is a
// 404 like any other.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
