package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The messages the tile actions leave behind, for the same reason the group
// ones are named: a flash and a stream have to agree about what happened.
const (
	itemCreated  = "Tile added."
	itemUpdated  = "Tile updated."
	itemDeleted  = "Tile removed."
	itemMoved    = "Item moved successfully."
	itemNotMoved = "Failed to move item."
)

// handleItemCreate is POST /start/items.
//
// The group is named by a parameter of its own rather than by the path,
// because the form that sends it is the one inside that group. Position comes
// from the group, not the form, for the same reason a new group's comes from
// its column.
func (s *Server) handleItemCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		groupID, err := strconv.ParseInt(r.PostFormValue("group_id"), 10, 64)
		if err != nil {
			s.notFound(w)
			return
		}
		group, err := s.db.GroupByID(ctx, user.ID, groupID)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		title := r.PostFormValue("start_page_item[title]")
		itemURL := r.PostFormValue("start_page_item[url]")

		_, err = s.db.CreateItem(ctx, user.ID, groupID, title, itemURL)
		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, itemCreated)
				return
			}
			view, err := s.groupViewFor(ctx, *group)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			// Left open so the next link can be typed straight away.
			view.NewItem.Open = true
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
			s.redirect(w, r, "/start/edit", flashAlert, "Failed to add tile: "+invalid.Error())
			return
		}

		stream, err := s.newItemStream(newItemView{
			GroupID:   group.ID,
			GroupName: group.Name,
			Open:      true,
			Form: itemForm{
				GroupID:    group.ID,
				Typed:      true,
				Title:      title,
				URL:        itemURL,
				Errors:     invalid.FullMessages(),
				TitleError: invalid.On("title"),
				URLError:   invalid.On("url"),
			},
		})
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, stream)
	})
}

// handleItemUpdate is PATCH/PUT /start/items/{id}. A tile owns its own title
// and url and there is no metadata to re-fetch, so a typo has to be fixable.
func (s *Server) handleItemUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}
		title := r.PostFormValue("start_page_item[title]")
		itemURL := r.PostFormValue("start_page_item[url]")

		item, err := s.db.UpdateItem(ctx, user.ID, id, title, itemURL)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, itemUpdated)
				return
			}
			stream, err := s.itemStream(itemViewFor(*item))
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
			s.redirect(w, r, "/start/edit", flashAlert, "Failed to update tile: "+invalid.Error())
			return
		}

		// The row comes back describing what is stored, with the form open
		// over it holding what was typed.
		stored, err := s.db.ItemByID(ctx, user.ID, id)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		view := itemViewFor(*stored)
		view.FormOpen = true
		view.Form.Typed = true
		view.Form.Title = title
		view.Form.URL = itemURL
		view.Form.Errors = invalid.FullMessages()
		view.Form.TitleError = invalid.On("title")
		view.Form.URLError = invalid.On("url")

		stream, err := s.itemStream(view)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, stream)
	})
}

// handleItemDestroy is DELETE /start/items/{id}. The store removes the tile
// and closes the gap it leaves as one unit. The group is what gets redrawn,
// because a group owns its tile rows and their positions.
func (s *Server) handleItemDestroy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := userFrom(ctx)

		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}
		item, err := s.db.ItemByID(ctx, user.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		if err := s.db.DeleteItem(ctx, user.ID, id); err != nil {
			s.serverError(w, r, err)
			return
		}

		if !wantsTurboStream(r) {
			s.redirect(w, r, "/start/edit", flashNotice, itemDeleted)
			return
		}
		stream, err := s.groupStreamFor(ctx, user.ID, item.GroupID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusOK, stream)
	})
}

// handleItemVisit is POST /start/items/{id}/visit: fire and forget from the
// grid. Bump the counter without touching validations, and say nothing back.
func (s *Server) handleItemVisit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}
		err := s.db.IncrementVisitCount(r.Context(), userFrom(r.Context()).ID, id)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// handleItemMove is POST /start/items/{id}/move.
//
// A group_id means "into that group". Its absence means "somewhere else in
// the one it is already in". The two are different calls with different
// ideas of a position — see the store's comments — and the distinction is
// the parameter's presence, not its value.
func (s *Server) handleItemMove() http.Handler {
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
		position, _ := strconv.Atoi(params["position"])
		rawGroup := params["group_id"]

		item, err := s.db.ItemByID(ctx, user.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			s.notFound(w)
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		source := item.GroupID
		destination := source

		if rawGroup != "" {
			groupID, parseErr := strconv.ParseInt(rawGroup, 10, 64)
			if parseErr != nil {
				s.notFound(w)
				return
			}
			// Asked for first, so that another user's group id is a 404
			// rather than a move that quietly does nothing.
			_, lookupErr := s.db.GroupByID(ctx, user.ID, groupID)
			if errors.Is(lookupErr, store.ErrNotFound) {
				s.notFound(w)
				return
			}
			if lookupErr != nil {
				s.serverError(w, r, lookupErr)
				return
			}
			destination = groupID
			err = s.db.MoveItemToGroup(ctx, user.ID, id, groupID, position)
		} else {
			err = s.db.MoveItem(ctx, user.ID, id, position)
		}

		if err == nil {
			if !wantsTurboStream(r) {
				s.redirect(w, r, "/start/edit", flashNotice, itemMoved)
				return
			}
			streams, err := s.groupStreams(ctx, user.ID, destination, source)
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
			s.redirect(w, r, "/start/edit", flashAlert, itemNotMoved)
			return
		}

		notice, err := s.noticeStream(itemNotMoved)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		// The client moved the tile before it asked. A refusal that only
		// says so leaves the page showing an order the database never
		// accepted. The next move then takes its index from that page.
		// So both groups come back as they are stored. Both exist here,
		// unlike a refused group move: the only way a tile move fails is
		// the destination already holding the link.
		streams, err := s.groupStreams(ctx, user.ID, destination, source)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.writeTurboStream(w, http.StatusUnprocessableEntity, append([]streamAction{notice}, streams...)...)
	})
}

// groupStreams redraws the destination group, and the source as well when the
// two are different. Destination first, as Rails built them.
func (s *Server) groupStreams(ctx context.Context, userID, destination, source int64) ([]streamAction, error) {
	ids := []int64{destination}
	if source != destination {
		ids = append(ids, source)
	}

	var streams []streamAction
	for _, id := range ids {
		stream, err := s.groupStreamFor(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	return streams, nil
}
