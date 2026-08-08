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
  in `tokens.css` if it ever needs revisiting.

- **Known gap, not a decision: reordering is pointer-only.** The up/down move
  buttons were removed once drag and drop could reorder within a group and
  within a column, so there is currently no keyboard path to reorder a tile or a
  group. Full keyboard access to editing is wanted and has not been designed
  yet — treat this as debt to pay, not a settled trade-off. Everything else on
  the page (add, edit, rename, delete) is reachable by keyboard.

<!-- TODO: Customize these rules for your project -->
