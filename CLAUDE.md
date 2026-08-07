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
| User | Auth; theme and color preferences; first user is bootstrapped as admin |
| Session | Auth sessions with expiration |
| StartPage | One per user: name + column count |
| StartPageGroup | A named group of tiles, placed at a column + position |
| StartPageItem | A tile: owns its own `url` and `title` (no pointer to tinylinks) |

### The tinylinks relationship

TinyStart is standalone: its own database, its own users, its own tiles. The
single integration point is **search**. The command bar filters local tiles
instantly, then federates a debounced query to the tinylinks API and shows those
results in a second section.

That call goes **server-side**: the browser hits TinyStart's own `/search.json`,
and Rails calls tinylinks with a bearer token. No token in the browser, no CORS.

The token is obtained once at setup through tinylinks' OAuth 2.0 device flow
(RFC 8628), requesting the `search` and `visit` scopes, and stored in TinyStart's
database — **not** in Rails credentials. It renews itself on use and only expires
after 90 days of inactivity.

## Patterns & Conventions

Inherited deliberately from tinylinks; keep them.

### Services
- Constructor takes dependencies, `call` method does the work
- Return empty array/hash on failure (graceful degradation)
- Use `Rails.logger` for debugging

### Frontend
- Stimulus controllers in `app/javascript/controllers/`
- Turbo Frames for async loading
- CSS in `app/assets/stylesheets/` — `stylesheet_link_tag :app` bundles every
  file in that directory, so a new `.css` file needs no wiring
- The design system pivots on one variable, `--base-accent` in `colors.css`,
  plus native `light-dark()`. Themes and colors are `data-theme` / `data-color`
  attributes on `<html>`.

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
