package web

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
)

// moveParams is what a move request carries — column, position, group_id — in
// whichever of the two encodings the editor uses.
//
// The forms on the page post application/x-www-form-urlencoded, and so did the
// Rails controller tests these handlers were ported from. But a move never
// comes from a form: lib/start_page_moves.js sends JSON, and Rails read that
// through the same params hash without anyone asking it to. Go does not, which
// is how the move handlers passed every test and failed the page.
//
// Values are kept as strings whichever way they arrived — a group id comes out
// of a data attribute as "7" and a position out of a DOM index as 7, and the
// handler that reads them does not care which — and presence is tracked apart
// from value, because the item move tells "into another group" from "somewhere
// else in this one" by whether group_id was sent at all.
type moveParams map[string]string

// readMoveParams decodes the request body by its Content-Type. A body that
// claims to be JSON and is not is a request the page never made; it is
// reported so the handler can answer 400 rather than move something to
// position zero.
func readMoveParams(r *http.Request) (moveParams, error) {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		params := moveParams{}
		for _, key := range []string{"column", "position", "group_id"} {
			if values, ok := r.PostForm[key]; ok {
				params[key] = values[0]
			}
		}
		return params, nil
	}

	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding a JSON move: %w", err)
	}
	params := moveParams{}
	for key, value := range raw {
		switch v := value.(type) {
		case string:
			params[key] = v
		case float64:
			// JSON numbers decode as float64; a position is a small integer
			// and formats as one. Anything with a fraction is not a position,
			// and Atoi in the handler refuses it the same way it refuses "x".
			params[key] = strconv.FormatFloat(v, 'f', -1, 64)
		case nil:
			// {"group_id": null} is a group_id that was not sent.
		default:
			return nil, fmt.Errorf("decoding a JSON move: %s is not a string or a number", key)
		}
	}
	return params, nil
}
