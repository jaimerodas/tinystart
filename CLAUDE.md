# TinyStart

Personal browser start page: a command bar plus a grid of hand-curated tiles,
organised into groups across columns. Live at https://start.pati.to.

## Stack

- Go 1.26 (`go.mod` pins the exact toolchain), standard library first:
  `net/http` (1.22 method+path patterns), `html/template`, `database/sql`.
- Three runtime dependencies: `modernc.org/sqlite` (pure Go, so the binary is
  static), `golang.org/x/crypto/bcrypt`, `go.yaml.in/yaml/v3`. `chromedp` is
  test-only. Don't add a fourth without a reason worth a commit message.
- SQLite, the same file the Rails app wrote — see Invariants.
- Hotwire (Turbo + Stimulus) vendored as `.min.js`, an importmap generated at
  boot; no Node, no bundler. Assets are embedded in the binary and served with
  content digests.
- Kamal 2 → DigitalOcean, sharing a droplet with `tinylinks` and `gastitos`.
- Was Rails until August 2026. `docs/go-rewrite-plan.md` is the plan, the
  measurements, and everything found on the way; read it before wondering
  "why is this like that".

## Commands

```bash
TINYSTART_SECRET_KEY=$(openssl rand -hex 32) go run ./cmd/tinystart   # local server on :3000
go run ./cmd/tinystart set-password you@example.com                     # password from stdin
./script/test        # the gate: gofmt, vet, staticcheck, govulncheck, go test -race, browser tests
kamal deploy         # ship (kamal setup the first time)
```

Nothing runs `./script/test` for you — no git hook is installed. Run it before
every commit and keep it green; a govulncheck or staticcheck finding is a
blocker, not a note. Until phase 9 of the plan every Go command run by hand
needs `GOFLAGS=-mod=readonly` (Rails' empty `vendor/` puts Go in vendor mode);
`script/test` sets it.

## Map

| Package | |
|---|---|
| `cmd/tinystart` | `run()`: env → config, open + migrate the DB, serve, graceful shutdown; the `set-password` subcommand |
| `internal/store` | the only package that knows SQL. `db.go` (one connection, WAL, `tx` helper), `migrate.go` + `schema.sql`, `time.go` (Rails' timestamp format), `errors.go` (`ErrNotFound`, `ErrConflict`, `ValidationError` with Rails' wording), users / sessions / groups / items / connections / startpage |
| `internal/startpage` | the YAML interchange format: `Layout`, `Import`, `Export`; `psych.go` reproduces Psych's quoting so exports stay byte-identical |
| `internal/tinylinks` | `Client` (search, visit) and `DeviceFlow`; reports outcomes, the web layer records them |
| `internal/postmark` | `Send` |
| `internal/web` | `server.go` + `routes.go` (every URL, one function), `middleware.go`, `auth.go`, `cookies.go` (signed cookies: session, flash, return-to, pending grant), `ratelimit.go`, `render.go` + `turbo.go`, `assets.go`, `startpage.go` (dom ids, shortcut lists, view structs), `handle_*.go` per screen, `templates/`, `static/` |

| Feature | Where |
|---|---|
| Start page and editor | `handle_start.go`, `handle_editor.go`, `handle_groups.go`, `handle_items.go`, `templates/pages/start_*.html`, `templates/startpage/` |
| Command bar, federated search | `static/js/controllers/command_bar_controller.js`, `handle_search.go`, `internal/tinylinks` |
| Connections (OAuth device flow) | `handle_connections.go`, `tinylinks.DeviceFlow`, `/settings/connections` |
| Import & export | `internal/startpage`, `store.ReplaceStartPage`, `handle_import_export.go` |
| Auth, password reset | `auth.go`, `handle_sessions.go`, `handle_users.go`, `handle_passwords.go`, `passwordreset.go` |
| Backups | `bin/backup_db`, installed by `.kamal/hooks/post-deploy`, weekly |

## Invariants

Violate one of these and the app breaks quietly rather than loudly. Each is
explained at the place it lives; this is the list, not the reasoning.

- **The database is Rails' database.** Same schema statement for statement
  (`store/schema.sql`), same `schema_migrations` table, timestamps written the
  way ActiveRecord wrote them (`store/time.go`), booleans as 0/1. Nothing may
  change that without a migration recorded the same way.
- **`internal/store` is the only package with SQL in it.** Everything above it
  gets structs and typed errors. `DB` keeps its `*sql.DB` unexported on purpose.
- **There is no start page record.** `columns` is a column on `users` and
  groups belong to the user. A start page exists from signup, so there is no
  "no start page yet" branch anywhere.
- **The start page is served at `/` and nowhere else.** `/start` survives as
  the `PATCH` target for the column count and as the prefix every group and
  item route hangs off; a `GET` there is a 404 on purpose (a test pins it).
- **The column count is edited on `/start/edit`, not in Settings.**
  `handle_settings.go` deliberately ignores `user[columns]`; a test pins it.
- **Connections are per-user.** Always the current user's connection, never an
  app-wide lookup — that leaks one person's results into another's command
  bar. `TestSearchNeverUsesAnotherUsersConnection` pins it.
- **Turbo Streams, never Frames.** Writes on `/start/edit` replace the smallest
  node that can have changed (`column_N`, `group_N`, `item_N`), with the ids
  built in `web/startpage.go`. Never widen a target back to `start_page_grid` —
  it carries the drag and keyboard controllers.
- **`#start_page_notice` is `update`d, never `replace`d.** It is a live region.
- **The editor's moves arrive as JSON, not forms** (`lib/start_page_moves.js`);
  `web/params.go` reads both. A test sends exactly what the browser sends.
- **Pointer and keyboard reordering stay strangers.**
  `drag_drop_controller.js` and `grid_keyboard_controller.js` share
  `lib/start_page_moves.js` and nothing else.
- **The Kamal volume stays `tinystart_storage`**, mounted at `/data`, and the
  process runs as uid 1000 (`Dockerfile` says why).

## Conventions

- **Shape**: `run(ctx, args, getenv, stdin, stdout)`; `NewServer(cfg, deps…)
  http.Handler`; handlers are `func (s *Server) handleX() http.Handler`
  closures; middleware is `func(http.Handler) http.Handler`. No framework, no
  ORM, no DI container, no globals but the embedded FS.
- **Errors**: `errors.Is/As` against `store.ErrNotFound`, `store.ErrConflict`,
  `store.ValidationError` (whose messages are what the page shows, word for
  word). Failure to reach another service degrades — empty results, a logged
  line — rather than 500s.
- **Templates**: `internal/web/templates/`, parsed once at boot; layouts
  `application` / `start` / `session`; a `<turbo-stream>` response is
  `turbo.go`. Markup is what the Rails views produced — the JS and CSS bind to
  it. `html/template` elides HTML comments; `htmlComment` exists for the one
  that matters.
- **CSS**: one file per concern in `internal/web/static/css/`; every file is
  linked, alphabetically, so a new file needs no wiring. The palette pivots on
  `--base-accent` in `colors.css` plus native `light-dark()`; theme and colour
  are `data-theme` / `data-color` on `<html>`. Measurements live in
  `tokens.css`. `.action-button` in `buttons.css` is the one button shape in
  Settings, with a `.danger` modifier.
- **Tests**: table-driven `testing`; `httptest` for the outside world (fake
  tinylinks, fake Postmark), never mocks; `newTestServer(t)` in
  `web_test.go`; a temp SQLite file per test; browser tests are `browser_*_test.go`
  behind `//go:build browser` (chromedp, 1400×1400, Capybara-shaped helpers).
  Never call a real external API.
- **Comments explain why**, in the voice of the rest of the code. Spanish
  commit messages, committed straight to `main` — solo repo.

## Read before touching

- `docs/start-page-format.md` — the import/export YAML spec, and the contract
  `internal/startpage` implements.
- `docs/go-rewrite-plan.md` — decisions and findings; the "Found while
  building" sections are the gotchas.
- `.claude/rules/` — `done.md` (verified, not assumed), `testing.md` (test
  first), `ui-design.md` (the editor's keyboard model, the visual standards, and
  the deliberate exceptions to them).

## Transition (until phase 9 of the plan)

The Rails tree (`app/`, `config/*.rb`, `db/`, `test/`, `Gemfile*`, `bin/rails*`,
`vendor/`) is still in the repo and no longer deployed. Do not edit it; it is
reference material for `script/parity` (Rails-vs-Go screen diff) and
`static_parity_test.go` (embedded JS/CSS equal `app/`), and `script/test_rails`
runs its suite. `config/deploy.yml` keeps `RAILS_MASTER_KEY` and a second
volume mount so `kamal rollback` to the Rails image still works. Phase 9
deletes all of it in one commit and switches `script/test` back to `go tool`.
