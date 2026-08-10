# TinyStart

Personal browser start page: a command bar plus a grid of hand-curated tiles,
organised into groups across columns. Live at https://start.pati.to.

## Stack

- Ruby 4.0.6 (`.tool-versions`) / Rails 8.1, SQLite, `schema_format :ruby`
- Hotwire (Turbo + Stimulus, importmap), Propshaft
- Custom session-based auth, not Devise
- Kamal 2 → DigitalOcean, sharing a droplet with `tinylinks` and `gastitos`

## Commands

```bash
bin/dev              # local server
bin/rails test       # everything but system tests
./script/test        # the gate: test:all + rubocop + brakeman
kamal deploy         # ship (kamal setup the first time)
```

Nothing runs `./script/test` for you — no git hook is installed. Run it before
every commit and keep it green; a Brakeman warning is a blocker, not a note.

## Map

| Model | |
|---|---|
| User | auth; `theme_preference`, `color_preference`, `columns` (defaults to 1); the first user is bootstrapped as admin |
| Session | an auth session, with expiry |
| StartPageGroup | a named group of tiles at a `column` + `position`, belonging to a user |
| StartPageItem | a tile; owns its own `title` and `url` |
| Connection | one user's token for another app, for federated search |

| Feature | Where |
|---|---|
| Start page and editor | `StartPagesController`, `StartPageGroups`/`ItemsController`, `app/views/start_pages/` |
| Command bar, federated search | `command_bar_controller.js`, `SearchController`, `ConnectionClient` |
| Connections (OAuth device flow) | `Settings::ConnectionsController`, `DeviceFlow`, `/settings/connections` |
| Import & export | `StartPageImporter`, `StartPageExporter`, `/settings/import_export` |
| Backups | `bin/backup_db`, installed by `.kamal/hooks/post-deploy`, weekly |

## Invariants

Violate one of these and the app breaks quietly rather than loudly. Each is
explained at the place it lives; this is the list, not the reasoning.

- **There is no `StartPage` record.** `columns` is a column on `users` and
  groups belong to the user. A start page is never created — it exists from
  signup, so there is no "no start page yet" branch anywhere.
- **The start page is served at `/` and nowhere else.** `/start` survives as the
  `PATCH` target for the column count and as the prefix every group and item
  route hangs off; a `GET` there is a 404 on purpose.
- **The column count is edited on `/start/edit`, not in Settings.**
  `SettingsController` deliberately does not permit `:columns`; a test pins it.
- **Connections are per-user.** Always `current_user.connection`, never an
  app-wide lookup — that leaks one person's results into another's command bar.
  A regression test in `search_controller_test.rb` pins it.
- **Turbo Streams, never Frames.** Writes on `/start/edit` replace the smallest
  node that can have changed (`column_N`, `group_N`, `item_N`), with the ids
  built by `StartPageHelper`. Never widen a target back to `start_page_grid` —
  it carries the drag and keyboard controllers.
- **`#start_page_notice` is `update`d, never `replace`d.** It is a live region.
- **Pointer and keyboard reordering stay strangers.**
  `drag_drop_controller.js` and `grid_keyboard_controller.js` share
  `lib/start_page_moves.js` and nothing else.
- **The Kamal volume stays `tinystart_storage`** (`config/deploy.yml` says why).

## Conventions

- **Services**: the constructor takes dependencies, `call` does the work, and
  failure degrades to an empty array or hash rather than raising.
- **CSS**: one file per concern in `app/assets/stylesheets/`; `stylesheet_link_tag
  :app` bundles the whole directory, so a new file needs no wiring. The palette
  pivots on `--base-accent` in `colors.css` plus native `light-dark()`; theme and
  colour are `data-theme` / `data-color` on `<html>`. Measurements live in
  `tokens.css` — `--control-size` for the editor's dense grid, `--button-height`
  for Settings. `.action-button` in `buttons.css` is the one button shape in
  Settings, with a `.danger` modifier.
- **Tests**: Minitest + Mocha, SimpleCov, fixtures in `test/fixtures/`. Never
  call a real external API — mock `ConnectionClient` and `DeviceFlow`.
- **Style**: RuboCop Rails Omakase. Vanilla Rails over gems. Simple over clever.
  Spanish commit messages, committed straight to `main` — solo repo.

## Read before touching

- `docs/start-page-format.md` — the import/export YAML spec, and the contract
  both services implement.
- `.claude/rules/` — `done.md` (verified, not assumed), `testing.md` (test
  first), `ui-design.md` (the editor's keyboard model, the visual standards, and
  the deliberate exceptions to them).
