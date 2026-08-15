package web

import (
	"net/http"
	"strings"
)

// The two kinds of flash message, which are also the CSS class on the card and
// the reason one of them gets a tick icon.
const (
	flashNotice = "notice"
	flashAlert  = "alert"
)

// flashMessage is one line of feedback, carried from the request that caused
// it to the page that reports it.
type flashMessage struct {
	Type    string
	Message string
}

// setFlash stashes a message for the next render. It is written just before a
// redirect and cleared by the page the redirect lands on, which is what makes
// "one shot" true: a reload of that page shows nothing, exactly as Rails'
// flash did.
//
// The encoding is "type:message". A message can contain a colon and a type
// cannot, so cutting at the first one is unambiguous, and the signature makes
// the whole thing unforgeable — worth having, since the message is rendered
// into the page.
func (s *Server) setFlash(w http.ResponseWriter, kind, message string) {
	s.setSignedCookie(w, flashCookie, kind+":"+message, noExpiry)
}

// takeFlash reads the pending message and clears it in the same breath. The
// delete goes out with the response that shows the message, so the browser
// stops sending it from the next request onwards.
func (s *Server) takeFlash(w http.ResponseWriter, r *http.Request) []flashMessage {
	value, err := s.readSignedCookie(r, flashCookie)
	if err != nil {
		return nil
	}
	s.deleteCookie(w, flashCookie)

	kind, message, found := strings.Cut(value, ":")
	if !found || (kind != flashNotice && kind != flashAlert) {
		return nil
	}
	return []flashMessage{{Type: kind, Message: message}}
}

// redirect is the pair every write handler ends with: leave a message, then
// send the browser somewhere.
//
// 303 rather than 302 because the redirect after a POST has to become a GET,
// and 302 only does that by convention. Rails sends 302 for a redirect_to
// after a form post and browsers have always treated it as a 303; saying 303
// is saying the same thing without relying on the habit.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, url, kind, message string) {
	if message != "" {
		s.setFlash(w, kind, message)
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
