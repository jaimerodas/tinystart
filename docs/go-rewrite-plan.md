# TinyStart in Go: the rewrite plan

## Context

TinyStart idles at ~150 MB in production. I measured where that goes (numbers
below); the short version is that it is Rails + Ruby's heap, not anything the
app does — gem trimming buys ~5 MB, turning YJIT off ~15–20, and the floor for
a Rails process in this image is ~105 MB. You chose the large tier: rewrite in
Go, and make the result an exemplary, learnable Go application — stdlib-first,
small, obvious. Constraints that stay: Kamal deploy, the same SQLite database
(no migration, `kamal rollback` to the Rails image must keep working), the same
APIs (tinylinks device flow + search, Postmark), and a pixel-identical UI.

Target: ~15–25 MB RSS (measured 12 MB warmed for a same-shape Go toy vs 138 MB
for the Rails app).

## What was measured (kept for the record)

Production image built and run locally, warmed with 300 requests, RSS from
`/proc/*/status`:

| Variant                                              | at boot | after 300 req |
|------------------------------------------------------|--------:|--------------:|
| Rails as deployed (`ruby`, YJIT, jemalloc)           | 108 MB  | 138 MB        |
| + `thrust` process                                   |   2 MB  |  12 MB        |
| Rails, `config.yjit = false`                         | 111 MB  | 123 MB        |
| Rails, no YJIT + allocator tuning / 1 thread         |         | 118–121 MB    |
| Ruby: Roda + Sequel + Puma toy                       |  45 MB  |  66 MB        |
| Go: net/http + html/template + modernc sqlite toy    |   2 MB  |  12 MB        |

Rails frameworks alone are ~50 MB; removing Action Cable + Active Job: −1 MB;
`device_detector`: ~6 MB once Settings is opened. Local images
`tinystart-measure`, `roda-measure`, `go-measure` are still in Docker — the
first is the baseline for the final before/after; `docker rmi` the others.

## Decisions (made; say if you disagree)

- **In place, same repo.** Go lands beside Rails; Rails stays until parity is
  proven, then is deleted in one commit. History, `config/deploy.yml`,
  `docs/`, `.claude/rules/` all survive.
- **Module** `github.com/jaimerodas/tinystart`, Go 1.26 (installed).
- **Dependencies: three.** `modernc.org/sqlite` (pure Go → `CGO_ENABLED=0`
  static binary), `golang.org/x/crypto/bcrypt` (verifies Rails' `$2a$`
  digests as-is), `go.yaml.in/yaml/v3` (the maintained continuation of
  gopkg.in/yaml.v3; verify at `go get` time) for import/export. Everything
  else is the standard library: `net/http` with 1.22 method+path patterns,
  `html/template`, `database/sql`, `embed`, `log/slog`, `crypto/hmac`,
  `net/http.NewCrossOriginProtection` (1.25) instead of CSRF tokens,
  `testing` + `httptest`.
- **No framework, no ORM, no DI container.** The Mat Ryer shape:
  `run(ctx, args, getenv, stdout) error` in `main`, `NewServer(deps)
  http.Handler`, one `addRoutes(mux, deps)` that lists every route, handlers as
  `func(deps…) http.Handler` closures, middleware as `func(http.Handler)
  http.Handler`.
- **Same DOM, same names.** Templates reproduce today's markup 1:1 (form field
  names `user[columns]`, `start_page_group[name]`, ids from
  `StartPageHelper`, `button_to`'s `<form class="button_to">` + `_method`
  hidden field, `data-*` hooks). The JS and CSS ship byte-identical. That is
  what makes "looks the same" checkable by diff rather than by eye.
- **Sessions stay in the `sessions` table**; the cookie becomes
  `id:base64(HMAC-SHA256(id))`. Everyone logs in once after cutover.
- **Rails-format timestamps.** ActiveRecord writes SQLite datetimes as
  `"2006-01-02 15:04:05.999999"` UTC text and booleans as 0/1; the store
  reads and writes exactly that so both images agree on the data.
- **Browser tests in Go via chromedp** (Chrome already on the machine), behind
  `//go:build browser`. The four Capybara system tests encode the keyboard
  model and drag; they are ported, not dropped.
- **Image:** `debian:bookworm-slim` + `ca-certificates` (HTTPS to Postmark and
  tinylinks) + `sqlite3` CLI (so `bin/backup_db` keeps working with a path
  change) + the binary. Volume becomes `tinystart_storage:/data`,
  DB `/data/production.sqlite3`. Listens on 80, answers `/up`.

## Layout

```
go.mod / go.sum
cmd/tinystart/main.go            run(): env → config, open DB, migrate, serve, graceful shutdown
internal/store/                  database/sql over sqlite; the only package that knows SQL
  db.go                          Open (WAL, busy_timeout, foreign_keys), tx helper
  migrate.go + schema.sql        empty DB → full schema + Rails-format schema_migrations rows
  users.go sessions.go groups.go items.go connections.go
                                 including MoveGroup / MoveItem / Reorder* as transactions
internal/startpage/              pure domain, no DB, no HTTP: Column/Group/Item types,
  layout.go import.go export.go  the YAML format in docs/start-page-format.md
internal/tinylinks/              DeviceFlow (Start/Check) + Client (Search/RecordVisit)
internal/postmark/               Send(ctx, Message) over the HTTPS API
internal/web/                    the HTTP app
  server.go routes.go            NewServer, addRoutes — every path in one place
  middleware.go                  request id + slog, HSTS, method override (_method), cross-origin
  auth.go cookies.go             session cookie, signed cookies (flash, return_to, pending grant)
  ratelimit.go                   fixed-window per-IP limiter for sign-in / sign-up
  render.go turbo.go             template funcs, layouts, <turbo-stream> responses
  assets.go                      fingerprint embedded static files, importmap JSON
  handle_start.go handle_editor.go handle_sessions.go handle_passwords.go
  handle_settings.go handle_connections.go handle_import_export.go handle_admin.go
  templates/*.html               1:1 with app/views (layouts, partials, mail)
  static/                        css/, js/ (controllers, lib), icons/, vendor/ (turbo, stimulus)
script/test                      gofmt -l, go vet, staticcheck, govulncheck, go test ./...
script/test_rails                the Rails gate, until Rails goes
go.Dockerfile → Dockerfile at cutover; config/deploy.yml, bin/backup_db (path
only), .kamal/secrets
```

## Phases — each one small, tested first, verified before the next

**0. Scaffold + measurement baseline** — `go.mod`, `cmd/tinystart` serving
`/up`, Dockerfile (multi-stage, static binary), `script/test` for Go. Verify:
`docker build`, run, RSS.

**1. store** — `Open`, `migrate` (fresh DB gets `schema.sql` and all eleven
`db/migrate` versions written to `schema_migrations` + `ar_internal_metadata`,
existing DB is a no-op), then table-by-table with tests against a temp file DB:
users (bcrypt authenticate, first-user bootstrap, normalise email, columns
validation incl. "no group stranded"), sessions (create/find active/expire),
groups + items (create appends at end, unique name/url per scope, URL
validation, `MoveGroup`, `MoveItem` within/across groups, `Reorder*` closing
gaps — port the model tests in `test/models/`), connections. Verify: open a
copy of `storage/development.sqlite3` and read it; open a fresh DB and confirm
`schema_migrations` matches Rails'.

**2. startpage** — types + `Import`/`Export` per `docs/start-page-format.md`,
table-driven tests ported from `test/services/`, including the header-count
warning and every refusal path.

**3. tinylinks + postmark** — clients tested against `httptest.NewServer`
fakes (timeouts 2s/4s, JSON-that-isn't, rejected token recorded on the
connection, device flow states approved/pending/denied/expired/unreachable).

**4. web: skeleton** — `NewServer`, middleware, cookies, auth (require, resume,
start, terminate, refresh-if-<7-days), rate limiter, render (layouts
`application` / `start` / `session`, `icon`, `asset`, importmap), static assets
with fingerprints + immutable cache. Tests: `httptest` end to end with a temp
DB and a `newTestServer(t)` helper.

**5. web: screens, in this order, each with handler tests ported from
`test/controllers` + `test/integration`:** sign in / sign up / log out →
start page `/` (links JSON, federation state) → editor `/start/edit` + `PATCH
/start` (columns; stream failure replaces `column_count` + notice) → groups
create/update/destroy/move → items create/update/destroy/move/visit → search
`/search.json` + `/visits` → settings (theme/colour), password change →
password reset (mail via postmark; token = HMAC over user id, digest,
expiry, 15 min like Rails) → connections (device flow, pending grant in a
signed cookie, poll) → import/export (512 KB cap, UTF-8 check) → admin users
(approve toggle, reset mail). Turbo Stream responses replace exactly the ids
`StartPageHelper` names; `#start_page_notice` is `update`d, never `replace`d;
failed moves redraw the affected columns/groups.

**6. Parity harness** — the mechanical "looks the same" check: seed one DB
from `test/fixtures`, run Rails (`bin/rails s -e test`) and the Go binary
against copies of it, fetch every page and every stream response as the same
user, normalise (strip `authenticity_token`, csrf meta, asset digests,
whitespace) and `diff`. Zero diff on `/`, `/start/edit`, `/settings/*`,
`/session/new`, `/sign_up`, `/passwords/*`, and on the stream bodies of
create/update/destroy/move success and failure. A throwaway script; not kept.

**7. Browser tests** — port `test/system/*` to chromedp behind `//go:build
browser`: keyboard model (arrows/Home/End/Space/Enter/Delete/Esc/Tab, roving
tabindex, `.keyboard-mode`), drag (CDP mouse events on the handles), page
chords, import/export UI. Risky spot: drag; if CDP drag proves flaky, drive
the same `lib/start_page_moves.js` path the keyboard uses and keep one smoke
drag.

**8. Cutover** — Dockerfile → Go image, `config/deploy.yml` (`app_port`,
volume `/data`, `env.clear` for `TINYSTART_DB`), `.kamal/secrets`
(`TINYSTART_SECRET_KEY` replaces `RAILS_MASTER_KEY`), `bin/backup_db` path,
`docker-entrypoint` gone (migrate runs at boot). `kamal deploy`; smoke by hand:
log in, edit, drag, search, connection poll, export/import, reset mail. Measure
RSS on the droplet (`cat /proc/$(pidof tinystart)/status | grep VmRSS`).
Rollback path stays `kamal rollback` — same DB, same schema table.

**9. Delete Rails** — `app/`, `bin/rails*`, `config/*.rb`, `Gemfile*`, `db/`
(after copying migration versions into `schema.sql`'s comment), `test/`,
`.rubocop.yml`; rewrite `CLAUDE.md`, `.claude/rules/*` (commands, map,
invariants — most survive verbatim: no StartPage record, `/` only, streams not
frames, notice `update`d, pointer and keyboard strangers, volume name),
`docs/start-page-format.md` implementation notes.

## Idioms to lean on (the "learn Go" part)

`run()` returning an error and `main` doing only `os.Exit`; handlers built by
functions that receive their dependencies; `context` through every DB and HTTP
call; `errors.Is`/`As` with sentinel errors from `store` (`ErrNotFound`,
`ErrConflict`, a `ValidationError` carrying the message the page shows);
`defer tx.Rollback()`; `embed.FS` for templates and static; `slog` with a
request id; table-driven tests with `t.Run` and `t.Context()`;
`httptest.NewServer` for the outside world instead of mocks; `range over int`,
`slices`, `maps`, `min`/`max`; no globals except the embedded FS.

## Gotchas already found

- `Accept: text/vnd.turbo-stream.html` decides stream vs redirect+flash;
  `format.json` on moves is unused by the JS — drop it.
- `data-turbo-confirm`, `button_to` + `_method`: keep the markup, add the
  method-override middleware, drop `authenticity_token` (cross-origin
  protection via `Sec-Fetch-Site` needs no token; the JS still sends
  `X-CSRF-Token` — harmless).
- `stylesheet_link_tag :app` links every CSS file in the directory,
  alphabetically — do the same. Importmap `pin_all_from` for `controllers/`
  and `lib/`; `stimulus-loading.js` reads it to eager-load controllers, so the
  JSON must list each controller.
- `allow_browser versions: :modern` (Rails' UA gate) is not carried over.
- Google Fonts `<link>` stays; no CSP is set today (the initializer is all
  comments), so none is added.
- Rate limits: sign-in 10 / 3 min, sign-up 2 / 5 min, per IP, in memory.

Found while building phase 0:

- **Rails' empty `vendor/` turns Go's vendoring on**, and then fails on the
  missing `vendor/modules.txt`. `script/test` exports `GOFLAGS=-mod=readonly`;
  a bare `go build` or `go test` at the repo root needs the same. `go tool`
  takes no `-mod` flag at all, so the linters run through `go run` — both
  workarounds disappear in phase 9 with the directory.
- **The Go Dockerfile is `go.Dockerfile`, not `Dockerfile.go`.** A `.go`
  suffix makes it a source file to gofmt, vet and build, all of which fail on
  the first `#`.
- **`go.mod` pins `toolchain go1.26.6`.** govulncheck counts every standard
  library CVE the toolchain is behind on, so the gate stays red until the pin
  is current; bumping that line is the fix, and it will need bumping again.

- Rails' empty `vendor/` switches Go into vendor mode. Until phase 9 every Go
  command needs `GOFLAGS=-mod=readonly` (`script/test` exports it) and the
  linters run via `go run` instead of `go tool`. Phase 9 deletes `vendor/`,
  swaps back to `go tool`, and renames `go.Dockerfile` → `Dockerfile`.
- `go.mod` pins `toolchain go1.26.6` so govulncheck is clean; bump it when
  govulncheck starts naming stdlib CVEs again.

Found while building phase 4:

- **A signed cookie's value contains dots**, because the flash carries "Try
  another email address or password." — so the value and its signature are
  split at the *last* dot, not the first.
- **bcrypt at Rails' cost 12 takes 2.7 s under `-race`.** `store` grew
  `UseCheapPasswordHashing()` for the test suites of the packages above it;
  every one of them should call it from `TestMain`.
- **Rails in development annotates every rendered view** with
  `<!-- BEGIN app/views/… -->` comments (`annotate_rendered_view_with_filenames`
  in `config/environments/development.rb`). The parity normaliser has to strip
  them, along with the csrf meta tags, the hidden `authenticity_token` inputs
  and the asset digests. With those five substitutions the four authentication
  pages diff to nothing.
- **Propshaft's digest is not reproducible from the file's bytes** — it folds
  in the logical path — so the Go side uses the first eight hex digits of the
  SHA-256 instead. Same shape, same place, and the parity check normalises it
  away either way.

Found while building phase 5a (the start page, the editor, groups and items):

- **Rails joins two `<turbo-stream>` elements with nothing at all.** `turbo.go`
  had guessed a newline; the capture says otherwise, and a newline there is a
  text node between the two elements.
- **The blank line after the flash block was one short in all three layouts.**
  ERB's `<%= render %>` line contributes its own newline on top of the
  partial's, so a page carrying a flash has one more blank line than a page
  without. Only visible once a capture had a flash on it.
- **`html/template` elides HTML comments.** The start page has one that is
  markup rather than a note — the placeholder inside
  `.command-bar-suggestions` — so `render.go` grew an `htmlComment` func to put
  it back.
- **A Go template comment on its own line is not free.** Unlike ERB, which
  swallows the whole line, `{{/* … */}}` leaves the indentation and the newline
  around it. Comments live above the `{{define}}`, never inside the markup.
- **`value=""` and no `value` attribute are different**, and Rails picks between
  them on nil vs empty string: a fresh add form has no attribute, a rejected
  save that cleared the field has `value=""`. The forms carry a `Typed` flag to
  say which.
- **`&#34;` versus `&quot;`.** `html/template` escapes a double quote one way
  and ERB the other, everywhere — most visibly in the command bar's embedded
  JSON. The same character, the same DOM; the parity normaliser folds them
  together rather than either side changing.
- **Capturing Rails for the diff:** `bin/rails runner` with an integration
  session, `RAILS_DEVELOPMENT_HOSTS=www.example.com`, forgery protection and
  view annotations off, `ActiveRecord::QueryLogs.tags = []`, and every model
  call wrapped in `Rails.application.executor.wrap` — a request leaves
  `ExecutionContext` cleared and the query log tags blow up without it.

Found while building phase 5b+5c (search, settings, connections, import/export,
admin):

- **A cookie value may not carry every byte, and net/http drops the ones it
  may not rather than complaining.** A flash saying `the link "Bare" was
  rejected`, or anything with an em dash in it, came back a different string
  from the one that was signed, failed to verify and vanished. `signValue` now
  base64url-encodes the value; cutting at the *first* dot became safe in the
  same change, since neither half can contain one.
- **`http.Redirect` writes a one-line body for a redirected GET and Rails
  writes none.** No browser renders either — it follows the `Location` — so
  the parity normaliser drops a redirect's body, alongside the 302/303 and
  full-URL/path substitutions it already made.
- **The export's date is the application's, not the machine's.** `Date.current`
  is UTC; `time.Now()` is local, so an export made at eight in the evening in
  Mexico City was dated the day before the one Rails would have written.
- **`POSTMARK_API_URL` was added** so a binary run from a checkout can point at
  a fake and mail nobody, which is what Rails did in development with
  letter_opener. Empty is the real API, which is what production leaves it as.
- **The pending device grant's deadline lives inside the cookie's value, not on
  the cookie.** A cookie lifetime is enforced by the browser's clock, and a
  browser a few minutes behind would keep polling a grant the app has given up
  on.
- **`distance_of_time_in_words` had to be ported**, thresholds and wording and
  all, for "member since … ago" and "token expires … from now"; it is
  `timewords.go`, checked against a table taken from a Rails console.
- **The parity harness for this phase runs a fake connected app** on a port,
  so both sides drive the real device flow, the real federated search and the
  real mailer over HTTP rather than one of them being mocked.

Found while building phase 6 (the parity harness, `script/parity/`):

- **Both apps run as servers and one script drives them.** Earlier phases
  captured Rails through an integration session and Go through curl, which
  meant the sequence existed twice and could drift. `capture.py` is one
  sequence, run twice, which is also what keeps the ids on the two sides
  identical.
- **`bin/rails server` cannot be used**: `consider_all_requests_local`,
  forgery protection, the view annotations and letter_opener all have to
  change before the middleware stack is built, and none of them has an
  environment variable. `serve.rb` requires `config/application`, adds an
  initializer of the application's own — which runs after
  `config/environments/development.rb` and before the stack is built — and
  then calls `initialize!` and serves with Puma.
- **Rails in development answers a 404 with its debug page**, which is why the
  harness sets `consider_all_requests_local = false`: then Rails serves
  `public/404.html`, which is what the Go app serves everywhere.
- **Rails' mail goes to the fake Postmark too** (`postmark_settings` takes
  `host`, `port`, `secure: false`), so the mailbox is a capture like any other
  — and it is how each side's reset token is fetched, since neither app can
  verify the other's.
- **The mailbox capture found two real differences.** The plain text body was
  missing the newline Rails' `mailer.text.erb` layout adds; fixed. The
  postmark-rails `Headers` array and the CSS comment `html/template` drops out
  of the mail layout's `<style>` are left as reported known differences.
- **`LinksForCommandBar` was ordering by position and Rails orders by id.**
  Rails' `has_many :through` has no `ORDER BY`, so the order is SQLite's:
  group by group in id order, rowid by rowid inside each. Only a start page
  whose drawing order and creation order disagree shows it, which is why five
  phases of tests missed it and the first development database with a
  rearranged group caught it.
- **The harness never opens the development database**: it copies it, `-wal`
  and all, and checks the fingerprint again at the end. That file is somebody's
  working data, and the sequence includes every destructive write the app has.
- **Three differences are known and reported rather than normalised**: `/up`
  (Rails' green health page against `ok`), and the `charset=UTF-8` against
  `charset=utf-8` on the two 404s, which comes from Rails' exception
  middleware and is case-insensitive by RFC 9110.

Found while building phase 7 (the browser suite, `internal/web/browser_*_test.go`):

- **One Chrome for the binary, one tab per test, and the browser is closed from
  `TestMain`.** A `t.Cleanup` would take it down with whichever test happened to
  start it, so `web_test.go` holds a `closeBrowser` hook the tagged files fill
  in. A tab costs milliseconds; the browser costs the better part of a second.
- **A background tab is not a focused document, and autofocus is skipped for
  one.** Every command bar test failed until the tab was `page.BringToFront()`ed
  on the way in. Nothing else in the suite depended on it, which is what made it
  look like a bug in the page.
- **HTML5 drag still does not start from synthesised mouse events** — the drag
  is begun inside the browser process, out of reach of
  `Input.dispatchMouseEvent` — so `dragTo` dispatches `dragstart`/`dragover`/
  `drop`/`dragend` with a real `DataTransfer`, which is exactly what Capybara's
  own `drag_to` shim does. Everything from the parting list to the stored
  position is the page's code; only the browser's decision to begin a drag is
  simulated. Three drag tests, no flakiness in nine runs.
- **`checkVisibility()` alone counts `visibility: hidden` as visible.** The drag
  handles withdraw exactly that way, so the harness asks with
  `{ checkVisibilityCSS: true }` — the answer a reader would give.
- **`innerText` is what was rendered, not what was written.** The command bar's
  section headers are `text-transform: uppercase`, so the federated one asserts
  on `FROM 127.0.0.1`.
- **The flash is `position: fixed; inset: 0`**, so while it is up it is what
  every click on the page lands on. A second sign-in attempt has to dismiss it
  first, which is what the Ruby suite's `dismiss_flash` was for.
- **The colour radios are `opacity: 0` behind their swatches**, so the swatch —
  the `<label for>` — is what there is to click, for a test as much as for
  anyone else.
- **Chrome is started with `--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE
  127.0.0.1`.** The layout links Google Fonts and a tile can point anywhere; the
  suite has no business leaving the machine, and a failed resolution is instant
  where a timeout is not.
- **Uncaught page exceptions fail the test that saw them**, through a
  `runtime.EventExceptionThrown` listener. That is what turns "every page
  loads" into a real assertion: the importmap eager-loads every controller by
  name, and a module that 404s only says so in a browser.
- **The suite bites.** With `params.go` reverted to reading form values only —
  the bug of commit `d96559a` — twelve tests fail: every keyboard move, all
  three drags, the refused-move notice, and ⌥S dropping a carried tile on the
  way out. Restored, the suite is green again.

Phase 8 happened on 2026-08-15: `kamal deploy` from `047192d`, healthy on the
first try, RSS 16–19 MB on the droplet against ~150 MB before; `main` was
fast-forwarded and pushed the same evening. CI moved to `script/test` on
GitHub Actions (`.github/workflows/ci.yml`) and Dependabot to `gomod`.

Found while preparing phase 8:

- **A fresh named volume mounted at `/data` is root-owned unless the image
  owns the mount point.** The binary runs as uid 1000 and could not create the
  database on a new volume; the Dockerfile now `mkdir /data && chown` it, which
  a new volume copies. Production's existing volume was always uid 1000 (the
  Rails image made it so), so the uid must stay 1000.
- **The build stage cross-compiles** (`--platform=$BUILDPLATFORM`, `GOOS`/
  `GOARCH` from `TARGETOS`/`TARGETARCH`) so Kamal's amd64 build on an arm64
  laptop runs Go natively instead of under QEMU.
- **Rollback stays real for a while.** `kamal rollback` boots the Rails image
  with the *current* config, so `config/deploy.yml` keeps `RAILS_MASTER_KEY`
  and mounts the volume at `/rails/storage` as well as `/data` until the Go
  image has earned its keep; both are marked "transition only" and go together.
- `go.Dockerfile` is now `Dockerfile`; the Rails one is gone. `asset_path` is
  gone (assets live in the binary). `bin/backup_db` reads `/data`. The
  `console`/`dbc` aliases became `set-password`/`sqlite3`.

## Execution: Opus agents, in a worktree

- **Worktree first.** `EnterWorktree` (branch `go-rewrite`, worktree under
  `.claude/worktrees/`) before anything is written. All commits land on
  `go-rewrite`; `main` stays the deployable Rails app until cutover. Nixing the
  experiment is `git worktree remove` + `git branch -D go-rewrite`. Cutover
  (phase 8) is a merge to `main` — the only time the plan touches `main`.
- **One Opus agent per phase**, `subagent_type: general-purpose`,
  `model: opus`, run sequentially because each phase builds on the last. Every
  agent prompt carries: the worktree path, this plan's Decisions/Layout/Idioms
  sections verbatim, the phase's scope, the Rails files to port from (paths),
  the rule "tests first, `script/test` green, then commit on `go-rewrite` with
  a Spanish message", and "report what you verified and what you could not".
- **I review between phases** — read the code the agent wrote against the
  Decisions, run `script/test` and the phase's verification myself, and only
  then start the next agent. Phase 5 (screens) is split into three agents:
  auth + start page + editor/groups/items; search/visits/settings/passwords;
  connections/import-export/admin. Phase 6 (parity harness) and 7 (browser
  tests) each get their own agent; 8 (cutover) is done by me with you, since
  it deploys.
- Nothing runs `kamal deploy` or touches the droplet before phase 8, and
  phase 8 waits for your go.

## Verification (definition of done)

- `script/test` green: gofmt, vet, staticcheck, govulncheck, `go test ./...`
  (+ `-tags browser` locally).
- Parity harness: zero normalised diff across every page and stream.
- Fresh-DB boot produces the same `schema_migrations` as Rails; existing DB
  opens untouched; `kamal rollback` to the previous Rails image after a Go
  deploy still serves the site.
- Production RSS after a day ≤ 30 MB, checked on the droplet; `docker stats`
  for the container ≤ 40 MB.
- Docs and rules describe the Go app; no Ruby left in the tree.
