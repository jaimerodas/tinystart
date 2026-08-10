# UI Design Rules

Visual quality standards for this project. The UI design reviewer reads
this file and uses it to evaluate screenshots and code changes.

## Design System

<!-- Describe your project's design system, tokens, component library -->
<!-- Example:
- Color palette: blue-500 primary, gray-100 background
- Typography: SF Pro (iOS), Inter (web), 16px base
- Spacing: 4px grid system
- Component library: SwiftUI native / Tailwind / Material / custom
-->

## Screenshot Capture

<!-- Optional: provide hints for how the reviewer should capture the UI. -->
<!-- The reviewer will auto-detect your project type, but explicit commands -->
<!-- here take priority. Examples: -->

<!-- iOS Simulator:
```bash
xcrun simctl io booted screenshot /tmp/prove_it_screenshot.png
xcrun simctl io booted recordVideo /tmp/prove_it_recording.mov
```
-->

<!-- Web (Playwright):
```bash
npx playwright screenshot --url http://localhost:3000 /tmp/prove_it_screenshot.png
```
-->

## Navigation

<!-- Optional: map file patterns to app routes or screens so the reviewer -->
<!-- knows where to navigate when those files change. -->
<!-- Example:
- src/views/Settings.* → /settings
- Sources/App/Profile/** → Profile tab
-->

## Visual Standards

- Minimum touch target: 44x44pt (iOS) / 48x48dp (Android)
- Minimum contrast ratio: 4.5:1 (WCAG AA)
- Use design tokens for spacing, color, and typography — no magic numbers
- Consistent alignment and visual rhythm across related screens

## Accessibility Requirements

- All images and icons must have accessibility labels or alt text
- Interactive elements must have accessibility hints where purpose is non-obvious
- Support Dynamic Type / font scaling
- Color must not be the sole means of conveying information

## Exceptions / Intentional Violations

<!-- List items the reviewer has flagged that are intentional design choices. -->
<!-- The reviewer will not re-flag items listed here. -->

- The start page editor's controls are `--control-size` (2rem / 32px), under the
  44pt minimum above. `/start/edit` is a dense grid of columns meant for a
  desktop pointer, and a 44px row would make it roughly 40% taller for the sake
  of a target nobody reaches with a thumb. Chosen deliberately; the value lives
  in `tokens.css` if it ever needs revisiting. This covers the toolbar above the
  grid too — the column-count select is sized to `--control-size` so the toolbar
  row's height is fixed by the control rather than by the legend, which is what
  keeps the grid from shifting when the legend swaps halves.

- **Known gap, not a decision: a keyboard move is silent.** Every action on
  `/start/edit` is now reachable by keyboard (see the model below), but the
  moves are only shown, never announced — nothing says "picked up", "position 3
  of 6 in Work" or "move cancelled". The live region they would speak through
  already exists (`#start_page_notice`, `role="status"`), and
  `grid_keyboard_controller` already knows the position and group at every
  step, so this is a small addition rather than a design question. Treat it as
  debt to pay: until it lands, the editor is keyboard-operable but not fully
  accessible.

- **Entering the grid announces a pointer, not the keys.** `#start_page_grid`
  points `aria-describedby` at the legend's keyboard-mode half, which now reads
  "`?` for all the shortcuts" rather than reciting six keys. That is less said
  on the way in, and it is deliberate: the legend has to stay one line or the
  grid moves under it when the halves swap, and `⌥S` is a page shortcut that
  never fitted there at all. The full list is one keypress away and the dialog
  is a modal `<dialog>`, so it is reachable and readable by keyboard alone.

## Moving between the two pages

The start page and its editor are one thing in two states, so a chord moves
between them rather than a trip to the header:

| Key | On `/` | On `/start/edit` |
|---|---|---|
| `⌥E` | open the editor | — |
| `⌥S` | — | back to the start page |
| `?` | list every shortcut | list every shortcut |

Four things about this that are decisions, not accidents:

- **Matched on `event.code`, never `event.key`.** On a Mac `⌥E` is a dead key
  and `⌥S` is `ß`; the character a chord produces says nothing about the key
  that was pressed.
- **Swallowed on both pages, including the one where it does nothing.** The
  command bar is autofocused, so a chord that falls through types into the
  search box.
- **`?` is a shortcut only when nothing is being typed into**, or it could
  never be searched for. Which is why `Esc` on an empty command bar now steps
  out of it: the bar holds focus from page load, and nothing else on the page
  was reachable by keyboard until it let go.
- **Leaving by chord is leaving.** A row still carried in the editor is dropped
  and saved before the visit is asked for — `start_shortcuts` announces
  `start-page:leaving` on the window and `grid_keyboard` answers with the save
  it started, so the two stay strangers and the visit can still wait for it.
  Firing the POST first is not the same as it being processed first: both
  requests read the same table, and landing before the move commits renders the
  order it was about to replace.
- **Opening the list is not leaving.** `showModal()` takes focus out of the
  grid exactly the way a click outside it does, which is what commits a move —
  but half of what the list says is how to move a carried row, and `Esc` is in
  there as the way to change your mind. `leave()` bails when focus has gone
  into a `dialog[open]`, so a carried row is still carried when the list
  closes, and the mode does not flicker for the round trip.
- **The list is closed before Turbo can photograph it.** `showModal()` sets the
  `open` attribute; a snapshot taken with it set restores the panel rendered
  inline — out of the top layer, so no backdrop, no focus trap, and `Esc` no
  longer reaches it. Hence `turbo:before-cache`.

The shortcut lists live in `StartPageHelper` as data (`grid_shortcuts`,
`show_page_shortcuts`, `editor_shortcuts`) so the dialog cannot drift from the
keys the controllers implement.

## The start page editor's keyboard model

`/start/edit` is a composite widget, not a run of tab stops. The grid is one
Tab stop with a roving highlight; the rows are `.item-row`, `.group-header` and
the "Add link" / "Add group" triggers, each rendered `tabindex="-1"` with
exactly one promoted to `0` by `grid_keyboard_controller.js`. The icon buttons
inside a row are `tabindex="-1"` and are reached by key, not by Tab — three
stops per tile is what made crossing a full page take ~100 presses.

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

**Keyboard mode is a state the page shows.** None of the above does anything
until focus is in the grid, so the grid carries a `.keyboard-mode` class while
it is, and the page says so two ways: the legend above it swaps `Tab to enter
keyboard mode` for the key list, and the drag handles withdraw. Both are pure
CSS off that one class (`:has()` reaches the legend), so the controller never
writes outside its own element.

The handles go because in keyboard mode they are a second way to move a row
that may already be carried, and a way no keyboard can reach. They hide with
`visibility`, not `display` — collapsing a `--control-size` handle would shift
every row on the page sideways on each Tab in and out.

Entering is decided by `:focus-visible`, not by focus alone: clicking a row
focuses it but must not cost a pointer user their handles. Once in, only focus
leaving the grid turns it off — a programmatic `focus()` after a move need not
match `:focus-visible`, and the handles must not flicker back mid-carry. A move
or a delete is not "leaving" either, though focus does sit on `<body>` for the
round trip, so the sync is suppressed while one is outstanding.

Three rules worth keeping if this is ever reworked:

- **A carried row moves in the DOM and nowhere else until it is dropped.** That
  is what makes `Esc` a real cancel and a five-position move one save rather
  than five.
- **Letting go commits.** Tab drops rather than trapping, so there is no
  keyboard trap, and so does clicking away — a move must never be left dangling
  on screen to be committed by whatever the user does next, or lost when they
  navigate. `Esc` is the only way to abandon one.
- **A refusal has to redraw, not just report.** The client moves the row before
  it asks, so a `move` that fails streams the affected groups or columns back
  alongside the message. Reporting alone would leave the page showing an order
  the database refused — and the next move takes its position from the page.

<!-- TODO: Customize these rules for your project -->
