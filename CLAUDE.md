# TinyStart

Personal browser start page: a command bar plus a grid of hand-curated tiles,
organised into groups across columns.

**Live**: not deployed yet — will be https://start.pati.to

> **Status: built, not yet deployed.** Auth, the start page, the editor and
> federated search all work and CI is green; what's left is the deploy. It was
> extracted from `tinylinks`, where the start page used to live. The history and
> the design decisions behind it are in the plan at
> `~/.claude/plans/more-and-more-i-m-lively-pebble.md` — **read it before
> starting work**, it records decisions that were already settled and shouldn't
> be relitigated.

## Tech Stack

- Ruby 4.0.6 (`.tool-versions`) / Rails 8.1
- SQLite (single-server), `schema_format :ruby`
- Hotwire (Turbo + Stimulus, importmap), Propshaft
- Custom session-based auth (not Devise), ported from tinylinks
- Kamal 2 → DigitalOcean (same droplet as tinylinks, different service/volume)

## Quick Reference

```bash
bin/dev              # Start local server
bin/rails test       # Unit/controller/integration tests
./script/test        # What the pre-commit hook runs: rails test:all + rubocop + brakeman
bin/rubocop          # Code quality
bin/brakeman         # Security scan (must be clean)
bin/rails db:migrate # Run migrations
```

## Architecture

| Model | Purpose |
|-------|---------|
| User | Auth; theme, color and `columns` preferences (columns defaults to 1); first user is bootstrapped as admin |
| Session | Auth sessions with expiration |
| StartPageGroup | A named group of tiles belonging to a user, placed at a column + position |
| StartPageItem | A tile: owns its own `url` and `title` (no pointer anywhere else) |
| Connection | One user's credential for another app, for federated search |

There is no `StartPage` record. It only ever held a column count and a name
nobody read, and it was already 1:1 with its user, so `columns` lives on
`users` and groups belong to the user directly. The consequence worth
remembering: **a start page is never created**, it exists from signup — so
there is no "no start page yet" branch anywhere.

**The column count is edited on `/start/edit`, not in Settings.** It sits in
the editor's toolbar (`_column_count.html.erb` → `StartPagesController#update`)
because shrinking it can be refused — `User#columns_leave_no_group_stranded`
names the group that would be hidden — and the editor is the only page where
that group is on screen. `SettingsController` deliberately does **not** permit
`:columns`; there's a test pinning that.

### Connections (federated search)

TinyStart is standalone: its own database, its own users, its own tiles. The
single integration point is **search**. The command bar filters local tiles
instantly, then federates a debounced query to a connected app and shows those
results in a second section. In practice that app is tinylinks, but nothing
user-facing says so — the section is **Settings → Connections**, and every
message names the *host* it's talking to.

That call goes **server-side**: the browser hits TinyStart's own `/search.json`,
and Rails calls the other app with a bearer token. No token in the browser, no
CORS.

The token is obtained through the other app's OAuth 2.0 device flow (RFC 8628),
requesting the `search` and `visit` scopes. It's stored in TinyStart's database
— **not** in Rails credentials — so rotating it never needs a redeploy. It
renews itself on use and only expires after 90 days of inactivity.

The pieces: `Connection` (model), `ConnectionClient` (search + visit, degrades
to `[]` on every failure), `DeviceFlow` (the RFC 8628 dance),
`Settings::ConnectionsController` at `/settings/connections`.

**Connections are per-user, and this matters.** A token grants access to
exactly one account on the other app, so `Connection belongs_to :user` and every
lookup goes through `current_user.connection`. Never reach for an app-wide "the
connection" — that leaks one person's archive into another's command bar, and
there's a regression test in `search_controller_test.rb` pinning it (verified
to fail when an app-wide lookup is reintroduced).

`User#connection` reads like a database handle but isn't one: ActiveRecord
dropped its `#connection` instance method in Rails 8, so the name is free.

## Patterns & Conventions

Inherited deliberately from tinylinks; keep them.

### Services
- Constructor takes dependencies, `call` method does the work
- Return empty array/hash on failure (graceful degradation)
- Use `Rails.logger` for debugging

### Frontend
- Stimulus controllers in `app/javascript/controllers/`
- **Turbo Streams, not Frames** — there are no Turbo Frames in the app. Writes
  on `/start/edit` replace the smallest node that can have changed (`column_N`,
  `group_N`, `item_N`); the ids are built by helpers in `StartPageHelper` so the
  controllers, partials and tests all name them from one place. This includes
  the two `move` actions: they redraw the source and destination column (or
  group) only, on success *and* on failure — the client moves the row before it
  asks, so a refusal has to send the truth back or the page keeps an order the
  database never accepted. Never widen them back to `start_page_grid` — that
  node carries the drag and keyboard controllers, and replacing it drops the
  keyboard highlight on every move.
- **`#start_page_notice` is `update`d, never `replace`d.** It is a live region,
  and one is only announced for changes made inside it while it is already in
  the accessibility tree; replacing it delivers a region that arrives with its
  text already in place, which readers stay silent about.
- **Two ways to reorder, one way to save it.** Pointer drag lives in
  `drag_drop_controller.js`, the keyboard in `grid_keyboard_controller.js`, and
  neither knows about the other beyond sharing `lib/start_page_moves.js`. The
  keyboard model is written down in `.claude/rules/ui-design.md`; the short
  version is that the grid is one Tab stop with a roving highlight, and a
  carried row moves in the DOM and nowhere else until it is dropped.
- CSS in `app/assets/stylesheets/` — `stylesheet_link_tag :app` bundles every
  file in that directory, so a new `.css` file needs no wiring
- The design system pivots on one variable, `--base-accent` in `colors.css`,
  plus native `light-dark()`. Themes and colors are `data-theme` / `data-color`
  attributes on `<html>`. Sizing tokens live in `tokens.css` — `--control-size`
  is the one height every control in the start page editor shares.

### Testing
- Minitest + Mocha for mocking, SimpleCov for coverage
- Fixtures in `test/fixtures/`
- Mock external APIs, don't call them in tests — especially `ConnectionClient`

### Code Style
- RuboCop Rails Omakase
- Prefer vanilla Rails over gems
- Spanish commit messages
- Keep it simple — this is a personal project

## Notes for AI

- Personal project — prefer simple over clever
- Tests must pass before considering work complete; the pre-commit hook runs
  `./script/test` and a Brakeman warning blocks the commit
- Rules in `.claude/rules/` apply: `done.md` (verified, not assumed),
  `testing.md` (test first), `ui-design.md`
