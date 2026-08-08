# TinyStart

Personal browser start page: a command bar plus a grid of hand-curated tiles,
organised into groups across columns.

**Live**: not deployed yet — will be https://start.pati.to

> **Status: scaffold only.** The app boots and CI is green, but none of the
> features below exist yet. It is being extracted from `tinylinks`, where the
> start page currently lives. Build order and design decisions are in the plan
> at `~/.claude/plans/more-and-more-i-m-lively-pebble.md` — **read it before
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

## Planned architecture

Nothing here exists yet. Recorded so the shape is clear.

| Model | Purpose |
|-------|---------|
| User | Auth; theme, color and `columns` preferences (columns defaults to 1); first user is bootstrapped as admin |
| Session | Auth sessions with expiration |
| StartPageGroup | A named group of tiles belonging to a user, placed at a column + position |
| StartPageItem | A tile: owns its own `url` and `title` (no pointer to tinylinks) |

There is no `StartPage` record. It only ever held a column count and a name
nobody read, and it was already 1:1 with its user, so `columns` lives on
`users` and groups belong to the user directly. The consequence worth
remembering: **a start page is never created**, it exists from signup — so
there is no "no start page yet" branch anywhere, and the column count is
edited on the main Settings page alongside theme and color.

### The tinylinks relationship

TinyStart is standalone: its own database, its own users, its own tiles. The
single integration point is **search**. The command bar filters local tiles
instantly, then federates a debounced query to the tinylinks API and shows those
results in a second section.

That call goes **server-side**: the browser hits TinyStart's own `/search.json`,
and Rails calls tinylinks with a bearer token. No token in the browser, no CORS.

The token is obtained through tinylinks' OAuth 2.0 device flow (RFC 8628),
requesting the `search` and `visit` scopes, from **Settings → TinyLinks**. It's
stored in TinyStart's database — **not** in Rails credentials — so rotating it
never needs a redeploy. It renews itself on use and only expires after 90 days
of inactivity.

**Connections are per-user.** A tinylinks token grants access to exactly one
tinylinks account, so `TinylinksConnection belongs_to :user` and every lookup
goes through `current_user.tinylinks_connection`. Never reach for an app-wide
"the connection" — that leaks one person's archive into another's command bar,
and there's a regression test in `search_controller_test.rb` pinning it.

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
  controllers, partials and tests all name them from one place.
- CSS in `app/assets/stylesheets/` — `stylesheet_link_tag :app` bundles every
  file in that directory, so a new `.css` file needs no wiring
- The design system pivots on one variable, `--base-accent` in `colors.css`,
  plus native `light-dark()`. Themes and colors are `data-theme` / `data-color`
  attributes on `<html>`. Sizing tokens live in `tokens.css` — `--control-size`
  is the one height every control in the start page editor shares.

### Testing
- Minitest + Mocha for mocking, SimpleCov for coverage
- Fixtures in `test/fixtures/`
- Mock external APIs, don't call them in tests — especially the tinylinks client

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
