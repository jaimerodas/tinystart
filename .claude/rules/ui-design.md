# UI Design Rules

Visual standards for this project, and the exceptions a reviewer should not
re-flag. The design system lives in the code; this points at it.

## Design System

- **Palette**: `colors.css`, pivoting on `--base-accent` plus native
  `light-dark()`. Theme and colour are `data-theme` / `data-color` on `<html>`;
  the eight accents are `User::VALID_COLORS`.
- **Measurements**: `tokens.css`. `--control-size` (2rem) is every control in the
  start page editor; `--button-height` (2.75rem) is every button in Settings.
- **Buttons**: `.action-button` in `buttons.css`, with a `.danger` modifier — the
  only button shape in Settings. `.button-link` is the start page header's.
- **Type**: SN Pro, 16px base (`application.css`).
- One file per concern in `app/assets/stylesheets/`; `stylesheet_link_tag :app`
  bundles the directory, so a new file needs no wiring.

## Screenshot Capture

System tests drive a real headless Chrome at 1400×1400, so looking at a page
means a scratch system test — `visit`, then `page.save_screenshot`, plus
`evaluate_script` for geometry. Delete the scratch test afterwards.

Safari MCP is the other route, against `bin/dev` by hand. Either way, **trust
`evaluate_javascript`/`evaluate_script` over a screenshot** for anything
measurable: a downscaled screenshot has misled me about both colour and spacing
here, and computed styles have not.

## Navigation

| Views | Page |
|---|---|
| `start_pages/show`, `_grid`, `_column`, `_group`, `_item` | `/` |
| `start_pages/edit`, `_column_count`, `_keyboard_legend`, `_shortcuts_dialog` | `/start/edit` |
| `settings/show` | `/settings` |
| `settings/import_export/` | `/settings/import_export` |
| `settings/connections/` | `/settings/connections` |
| `settings/users/` | `/settings/admin/users` |
| `sessions/`, `users/`, `passwords/` | `/session/new`, `/sign_up` |

## Visual Standards

- Minimum contrast ratio: 4.5:1 (WCAG AA)
- Spacing, colour and type come from `tokens.css` / `colors.css` — no magic
  numbers
- Consistent alignment and visual rhythm across related screens
- **Desktop pointer and keyboard. Mobile is not a target** — never justify work
  here with touch targets.

## Accessibility Requirements

- Every icon-only control carries an `aria-label`, and where a control has
  visible text the accessible name begins with it (WCAG 2.5.3, Label in Name).
- Colour is never the only thing carrying a state.
- Everything is reachable by keyboard, and focus is always visible
  (`:focus-visible`, not `:focus` — a pointer user should not see the roving
  highlight).
- Pointer-only affordances are `aria-hidden`: the drag handles are a second way
  to do what the keyboard already does, so in the tree they would be controls
  that do nothing.

## Exceptions / Intentional Violations

- **The editor's controls are 2rem, not 44px.** `/start/edit` is a dense desktop
  grid; the value is `--control-size` in `tokens.css`. Covers the toolbar's
  column-count select, sized to match so the grid does not shift when the legend
  swaps halves.
- **Keyboard moves are not announced.** Nothing says "picked up" or "position 3
  of 6 in Work". Offered when the keyboard model was built and deliberately
  declined — don't build it, or re-flag it, unasked.
- **Entering the grid announces "`?` for all the shortcuts", not the key list.**
  The legend has to stay one line or the grid shifts under it; the full list is
  one keypress away in a modal `<dialog>`.
- **Up/down move buttons are not coming back.** Removed deliberately for a less
  cluttered row; drag and the keyboard are the two ways to reorder.

## The start page editor's keyboard model

`/start/edit` is a composite widget, not a run of tab stops: the grid is one Tab
stop with a roving highlight. The rows are `.item-row`, `.group-header` and the
"Add link" / "Add group" triggers, all `tabindex="-1"` with exactly one promoted
to `0` by `grid_keyboard_controller.js`. The icon buttons inside a row are
reached by key, not by Tab.

| Key | Highlighted | Carrying |
|---|---|---|
| `↑` `↓` | previous / next row in the column | move one position |
| `←` `→` | nearest row in the adjacent column | move to the adjacent column |
| `Home` `End` | first / last row in the column | — |
| `Enter` | edit | drop and save |
| `Space` | pick up | drop and save |
| `Delete` `Backspace` | delete | — |
| `Esc` | (belongs to the inline forms) | cancel, put it back |
| `Tab` | leave the grid | drop first, then leave |

Page chords live in `start_shortcuts_controller.js`: `⌥E` opens the editor, `⌥S`
goes back, `?` lists every shortcut on either page. The lists are data in
`StartPageHelper` (`grid_shortcuts`, `show_page_shortcuts`, `editor_shortcuts`)
so the dialog cannot drift from the keys the controllers implement.

**Keyboard mode is a state the page shows.** The grid carries `.keyboard-mode`
while focus is in it; the legend swaps for the key list and the drag handles
withdraw. Both are pure CSS off that one class, so the controller never writes
outside its own element.

Three rules to keep if this is ever reworked — the controllers explain each one
at the line that implements it:

- **A carried row moves in the DOM and nowhere else until it is dropped.**
- **Letting go commits.** Tab, clicking away and a page chord all drop and save;
  `Esc` is the only way to abandon a move.
- **A refusal has to redraw, not just report.** The client moves the row before
  it asks, so a failed `move` streams the affected columns or groups back.
