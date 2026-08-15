package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// turboStreamMIME is the media type Turbo asks for and answers to. A browser
// that has loaded the Turbo runtime adds it to Accept on every form it
// submits, which is the switch this app runs on: with it, a write answers with
// the pieces of the page that changed; without it — a form with
// data-turbo="false", or a browser with JavaScript off — the same write
// redirects and the whole page is drawn again.
const turboStreamMIME = "text/vnd.turbo-stream.html"

// wantsTurboStream reports whether the caller will understand a stream.
//
// Matching on a substring rather than parsing Accept properly is deliberate:
// Turbo puts the type in the header verbatim, q-values never appear on it, and
// the alternative — a full Accept parser — would be fifty lines to answer a
// yes-or-no question that has one possible source.
func wantsTurboStream(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), turboStreamMIME)
}

// streamAction is one instruction to the Turbo runtime: do this, to the
// element with this id, with this markup. The ids are the ones StartPageHelper
// names, so a stream always replaces the smallest node that can have changed.
type streamAction struct {
	Action string
	Target string
	HTML   template.HTML
}

// The actions this app uses. Turbo defines more; these are the three the
// editor needs, and naming them means a typo is a compile error.
const (
	streamReplace = "replace"
	streamUpdate  = "update"
	streamRemove  = "remove"
)

// replaceStream swaps the element itself; updateStream swaps only its
// contents. The difference matters for #start_page_notice, which is a live
// region: a screen reader announces a change inside a region it already knows
// about, and not the arrival of a region with the text already in it. So the
// notice is always updated and never replaced.
func replaceStream(target string, html template.HTML) streamAction {
	return streamAction{Action: streamReplace, Target: target, HTML: html}
}

func updateStream(target string, html template.HTML) streamAction {
	return streamAction{Action: streamUpdate, Target: target, HTML: html}
}

// removeStream takes an element off the page. It carries no markup, and Turbo
// ignores the <template> — but Rails emitted an empty one, so this does too.
func removeStream(target string) streamAction {
	return streamAction{Action: streamRemove, Target: target}
}

// writeTurboStream sends one or more actions as a stream response.
//
// The wire format is exactly what turbo-rails' turbo_stream_action_tag
// produced, down to the empty <template> on a remove, because the parity check
// diffs these bodies too.
func (s *Server) writeTurboStream(w http.ResponseWriter, status int, actions ...streamAction) {
	var body bytes.Buffer
	for i, action := range actions {
		if i > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, `<turbo-stream action=%q target=%q><template>%s</template></turbo-stream>`,
			action.Action, action.Target, action.HTML)
	}

	w.Header().Set("Content-Type", turboStreamMIME+"; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body.Bytes()) //nolint:errcheck // the client hanging up is not something this can act on
}

// renderPartial renders one named template into a string, which is how a
// stream action gets its markup: the same partial the full page uses, so the
// two can never drift.
func (s *Server) renderPartial(page, name string, data any) (template.HTML, error) {
	set := s.templates.pages[page]
	if set == nil {
		return "", fmt.Errorf("no template named %q", page)
	}
	var out bytes.Buffer
	if err := set.ExecuteTemplate(&out, name, data); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil //nolint:gosec // the template escaped its own inputs
}
