# Gotchas

Things that cost time once and are not obvious from one file. Most of the
reasoning in this app lives in a comment at the line it applies to. This page
holds only what cuts across files, or what bites before you know where to look.

## Where the app came from

TinyStart was a Rails app until August 2026. It idled at about 150 MB, and
almost all of that was Rails and Ruby's heap. The Go rewrite runs at 16 to
19 MB on the same droplet. The cutover was 2026-08-15 and Rails left the repo
on 2026-08-22. The full plan, the measurements and the phase-by-phase findings
are in git history: `git log -- docs/go-rewrite-plan.md`.

Two things survive from Rails on purpose:

- **The database.** `CLAUDE.md` lists it as an invariant and
  `store/schema.sql` explains it.
- **The markup.** The templates reproduce what the Rails views produced,
  because the JS and CSS bind to it. Form fields are still named
  `user[columns]` and `start_page_group[name]`, and the forms still carry a
  hidden `_method` field that the method-override middleware reads.

## The gate

- **govulncheck turns red with no commit.** It reports every standard library
  CVE the pinned toolchain is behind on. The fix is to bump the `toolchain`
  line in `go.mod`. The weekly CI run exists to catch this.
- **A new test package must call `store.UseCheapPasswordHashing()` from
  `TestMain`.** bcrypt at cost 12 takes 2.7 s under `-race`.

## Requests and cookies

- **There are no CSRF tokens.** `http.NewCrossOriginProtection` reads
  `Sec-Fetch-Site` instead. The JS still sends `X-CSRF-Token`. It is harmless.
- **`Accept: text/vnd.turbo-stream.html` decides between a stream and a
  redirect with a flash.** A handler test that forgets the header tests the
  other branch.
- **Rate limits are per IP and in memory**: sign-in 10 per 3 minutes, sign-up
  2 per 5 minutes. A restart clears them.
- **No CSP is set.** The layouts link Google Fonts.

## Templates

- **`html/template` drops HTML comments.** Use `htmlComment` for one that is
  markup.
- **A `{{/* comment */}}` on its own line leaves its indentation and newline in
  the output.** Put template comments above the `{{define}}`, never inside the
  markup.
- **`value=""` and no `value` attribute are different.** A fresh form has no
  attribute. A rejected save that cleared the field has `value=""`. The form
  structs carry a `Typed` flag to say which.
- **Two `<turbo-stream>` elements are joined with nothing.** A newline between
  them is a text node.
- **The export's date is UTC**, in the header and in the filename. An export
  made in the evening in Mexico City carries the next day's date.

## Browser tests

`browser_test.go` explains each of these where it happens. Read this list
before you write a new test:

- The suite shares one Chrome and opens one tab per test. `TestMain` closes
  the browser, not `t.Cleanup`.
- Each tab is brought to the front. A background tab is not a focused
  document, and the browser skips autofocus for one.
- Chrome cannot reach the network. A tile can point anywhere, and a failed
  resolution is instant where a timeout is not.
- An uncaught page exception fails the test that saw it.
- HTML5 drag does not start from synthesised mouse events. `dragTo` dispatches
  the drag events with a real `DataTransfer`.
- `innerText` is what was rendered. The command bar's section headers are
  uppercase in CSS, so a test asserts on `FROM 127.0.0.1`.
- The colour radios are `opacity: 0` behind their swatches. Click the label.
- Ask about visibility with `checkVisibility({ checkVisibilityCSS: true })`.
  Without the option, `visibility: hidden` counts as visible.

## Deploy

- **`storage/` is not Rails furniture.** `TINYSTART_DB` defaults to
  `storage/development.sqlite3`.
- **`kamal` is the gem in mise's global Ruby.** The repo has no Gemfile and
  needs none.
- **The image owns `/data` as uid 1000**, and the build stage cross-compiles
  so an amd64 build on an arm64 laptop runs Go natively. The `Dockerfile` says
  why for both.
