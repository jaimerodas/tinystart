package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The messages the group actions leave behind. They are here rather than
// inline because the tests name them and because a flash and a stream have to
// agree about what happened.
const (
	groupCreated  = "Group created successfully."
	groupUpdated  = "Group updated successfully."
	groupDeleted  = "Group deleted successfully."
	groupMoved    = "Group moved successfully."
	groupNotMoved = "Failed to move group."
)

// handleGroupCreate is POST /start/groups.
//
// Position comes from the column, not the form: the add-group form sits at the
// bottom of a column, and where the group lands follows from that alone.
func (s *Server) handleGroupCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		name := r.PostFormValue("start_page_group[name]")
		raw := r.PostFormValue("start_page_group[column]")
		column, _ := strconv.Atoi(raw)

		group, err := s.db.CreateGroup(ctx, user.ID, name, column)
		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, groupCreated)
				return
			}
			stream, err := s.columnStream(ctx, user.ID, group.Column)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.writeTurboStream(w, http.StatusOK, stream)
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		if !wantsTurboStream(r) {
			s.redirect(w, r, "/start/edit", flashAlert, "Failed to create group: "+invalid.Error())
			return
		}

		// A form that sent no column at all would address a slot that does not
		// exist, and Turbo applies a stream with no target to nothing — the
		// error would vanish. Fall back to the first column, as Rails did.
		target := column
		if raw == "" {
			target = 1
		}

		stream, err := s.newGroupStream(newGroupView{
			Column: target,
			Open:   true,
			Form: groupForm{
				Name:      name,
				Typed:     true,
				Column:    target,
				Errors:    invalid.FullMessages(),
				NameError: invalid.On("name"),
			},
		})
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, stream)
	})
}

// handleGroupUpdate is PATCH/PUT /start/groups/{id}. A rename touches no
// sibling, so only the group itself has to be redrawn.
func (s *Server) handleGroupUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}
		name := r.PostFormValue("start_page_group[name]")

		group, err := s.db.UpdateGroup(ctx, user.ID, id, name)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, groupUpdated)
				return
			}
			view, err := s.groupViewFor(ctx, *group)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			stream, err := s.groupStream(view)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.writeTurboStream(w, http.StatusOK, stream)
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		if !wantsTurboStream(r) {
			s.redirect(w, r, "/start/edit", flashAlert, "Failed to update group: "+invalid.Error())
			return
		}

		// The group comes back as it is stored — the header and the labels
		// around it describe saved state — with the form open over it holding
		// what was typed, so the errors stay with the values that caused them.
		stored, err := s.db.GroupByID(ctx, user.ID, id)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		view, err := s.groupViewFor(ctx, *stored)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		view.FormOpen = true
		view.Form.Typed = true
		view.Form.Name = name
		view.Form.Errors = invalid.FullMessages()
		view.Form.NameError = invalid.On("name")

		stream, err := s.groupStream(view)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, stream)
	})
}

// handleGroupDestroy is DELETE /start/groups/{id}. The store removes the
// group, its tiles and the gap it leaves in its column as one unit.
func (s *Server) handleGroupDestroy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}

		// Read first: the column the group was in is what has to be redrawn,
		// and it is gone once the delete lands.
		group, err := s.db.GroupByID(ctx, user.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		if err := s.db.DeleteGroup(ctx, user.ID, id); err != nil {
			s.serverError(w, r, err)
			return
		}

		if !wantsTurboStream(r) {
			s.redirect(w, r, "/start/edit", flashNotice, groupDeleted)
			return
		}
		stream, err := s.columnStream(ctx, user.ID, group.Column)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusOK, stream)
	})
}

// handleGroupMove is POST /start/groups/{id}/move.
func (s *Server) handleGroupMove() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}
		params, err := readMoveParams(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		column, _ := strconv.Atoi(params["column"])
		position, _ := strconv.Atoi(params["position"])

		group, err := s.db.GroupByID(ctx, user.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		source := group.Column

		err = s.db.MoveGroup(ctx, user.ID, id, column, position)
		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, groupMoved)
				return
			}
			// A move renumbers the column the group left and the column it
			// landed in, and nothing else — so those are what get redrawn.
			streams, err := s.columnStreams(ctx, user.ID, column, source)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.writeTurboStream(w, http.StatusOK, streams...)
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		if !wantsTurboStream(r) {
			s.redirect(w, r, "/start/edit", flashAlert, groupNotMoved)
			return
		}

		notice, err := s.noticeStream(groupNotMoved)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		// The client moved the group before it asked, so saying no is not
		// enough: the columns have to be redrawn from what is actually stored,
		// or the page keeps a placement the database never accepted and the
		// next move takes its index from it.
		//
		// Only the columns that exist, and in that order. A move is refused
		// precisely when the column asked for is off the end of the grid, and
		// there is no node on the page to redraw for a column the user has not
		// got.
		streams := []streamAction{notice}
		for _, number := range dedupe(source, column) {
			if number < 1 || number > user.Columns {
				continue
			}
			stream, err := s.columnStream(ctx, user.ID, number)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			streams = append(streams, stream)
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, streams...)
	})
}

// columnStreams redraws the destination column, and the source as well when
// the two are different. Destination first, which is the order Rails built
// them in and so the order the page applies them.
func (s *Server) columnStreams(ctx context.Context, userID int64, destination, source int) ([]streamAction, error) {
	var streams []streamAction
	for _, number := range dedupe(destination, source) {
		stream, err := s.columnStream(ctx, userID, number)
		if err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	return streams, nil
}

// dedupe is Ruby's Array#uniq for the two-element case both move handlers
// have: keep the order, drop the repeat.
func dedupe(first, second int) []int {
	if first == second {
		return []int{first}
	}
	return []int{first, second}
}
