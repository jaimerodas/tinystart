package web

import (
	"maps"
	"net/http"
	"slices"
)

// addRoutes lists every URL this app answers, in one function, on purpose:
// there is exactly one place to look to find out what the surface is, and a
// route that is not here does not exist.
//
// The patterns are net/http's own, method and path in one string, matched by
// specificity rather than by declaration order — so "/passwords/new" wins over
// "/passwords/{token}" without anything having to be arranged.
func addRoutes(mux *http.ServeMux, s *Server) {
	// Health check. kamal-proxy polls it; logRequests keeps it out of the log.
	mux.HandleFunc("GET "+healthCheckPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n")) //nolint:errcheck // nothing to do if the prober hangs up
	})

	// Fingerprinted CSS and JavaScript, and the handful of files Rails served
	// straight out of public/ at the root.
	mux.Handle("GET /assets/", s.assets.handleAsset())
	// Sorted so registration is deterministic: ServeMux does not care, but a
	// test that prints the routes does, and so does anyone reading a diff.
	for _, path := range slices.Sorted(maps.Keys(s.assets.public)) {
		mux.Handle("GET "+path, s.handlePublicFile(path))
	}

	// Sign in and out. new and create are the two pages an anonymous visitor
	// may reach; destroy is behind the wall, because signing out is something
	// only a signed-in person can do.
	mux.Handle("GET /session/new", s.handleSessionNew())
	mux.Handle("POST /session", s.rateLimited(s.signIn, s.handleSessionCreate()))
	mux.Handle("DELETE /session", s.requireAuthentication(s.handleSessionDestroy()))

	// Sign up. Both actions turn a signed-in visitor away — there is nothing
	// on this page for someone who already has an account.
	mux.Handle("GET /sign_up", s.handleUserNew())
	mux.Handle("POST /sign_up", s.rateLimited(s.signUp, s.handleUserCreate()))

	// Password reset. Every action is open: the whole point is to be usable by
	// someone who cannot sign in. PUT and PATCH are the same handler because
	// the form says method="put" through _method and a Turbo submission would
	// say PATCH.
	mux.Handle("GET /passwords/new", s.handlePasswordNew())
	mux.Handle("POST /passwords", s.handlePasswordCreate())
	mux.Handle("GET /passwords/{token}/edit", s.handlePasswordEdit())
	mux.Handle("PUT /passwords/{token}", s.handlePasswordUpdate())
	mux.Handle("PATCH /passwords/{token}", s.handlePasswordUpdate())

	// The start page. "/{$}" is the root and only the root: without the {$}
	// the pattern would be a prefix and would swallow every URL below it.
	mux.Handle("GET /{$}", s.requireAuthentication(s.handleStartPage()))

	// The editor. There is deliberately no GET /start — the start page is
	// served at "/" and nowhere else — so a visit there falls through to the
	// catch-all and gets a 404. What survives of /start is this PATCH and the
	// prefix every group and item route hangs off.
	mux.Handle("GET /start/edit", s.requireAuthentication(s.handleStartEdit()))
	mux.Handle("PATCH /start", s.requireAuthentication(s.handleStartUpdate()))
	mux.Handle("PUT /start", s.requireAuthentication(s.handleStartUpdate()))

	// Groups. PUT alongside PATCH because Rails' resource routes accepted
	// both, and the form says method="patch" through _method while a Turbo
	// submission would say PATCH outright.
	mux.Handle("POST /start/groups", s.requireAuthentication(s.handleGroupCreate()))
	mux.Handle("PATCH /start/groups/{id}", s.requireAuthentication(s.handleGroupUpdate()))
	mux.Handle("PUT /start/groups/{id}", s.requireAuthentication(s.handleGroupUpdate()))
	mux.Handle("DELETE /start/groups/{id}", s.requireAuthentication(s.handleGroupDestroy()))
	mux.Handle("POST /start/groups/{id}/move", s.requireAuthentication(s.handleGroupMove()))

	// Tiles. visit is the one write with nothing to say back: the grid
	// records a click and carries on to the link.
	mux.Handle("POST /start/items", s.requireAuthentication(s.handleItemCreate()))
	mux.Handle("PATCH /start/items/{id}", s.requireAuthentication(s.handleItemUpdate()))
	mux.Handle("PUT /start/items/{id}", s.requireAuthentication(s.handleItemUpdate()))
	mux.Handle("DELETE /start/items/{id}", s.requireAuthentication(s.handleItemDestroy()))
	mux.Handle("POST /start/items/{id}/move", s.requireAuthentication(s.handleItemMove()))
	mux.Handle("POST /start/items/{id}/visit", s.requireAuthentication(s.handleItemVisit()))

	// Still to come, each in its own phase:
	//   GET  /search, POST /visits
	//   /settings, /settings/password, /settings/import_export
	//   /settings/connections and its poll
	//   /settings/admin/users and the two admin actions

	// Anything else, GET /start included.
	mux.Handle("/", s.handleNotFound())
}

// handlePublicFile serves one of the files that used to live in public/.
//
// They are not fingerprinted — /icon.png is named in the layouts and in other
// people's bookmarks — so they get a short cache instead of a permanent one: a
// changed favicon should reach people the same day.
func (s *Server) handlePublicFile(path string) http.Handler {
	body := s.assets.public[path]
	contentType := contentTypeFor(path)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(body) //nolint:errcheck // nothing to do if the client hangs up
	})
}

// handleNotFound is Rails' 404: the static page out of public/, with the
// status to match.
func (s *Server) handleNotFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serveErrorPage(w, http.StatusNotFound, "/404.html")
	})
}

// rateLimited turns the sign-in and sign-up limits into middleware. Both
// policies answer the same way, which is what SessionsController and
// UsersController did: back to the sign-in form, with an alert that says
// nothing about which limit was hit or how long it lasts.
func (s *Server) rateLimited(l *limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(remoteIP(r)) {
			s.redirect(w, r, "/session/new", flashAlert, "Try again later.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
