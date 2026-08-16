# Start page interchange format

A start page as a small YAML file. It was written to carry one out of
**tinylinks** (https://links.pati.to) and into **tinystart**, which was
extracted from it, and tinystart now both reads and writes it.

Two things produce this format: `StartPageExportService` in tinylinks (once,
for the migration) and `startpage.Export` in tinystart. One thing consumes it:
`startpage.Import`, with `store.ReplaceStartPage` doing the writing. Both ends
of tinystart's half live at **Settings → Import & Export**
(`internal/web/handle_import_export.go`, `/settings/import_export`). The
package is `internal/startpage`; the names below (`StartPageImporter`,
`StartPageExporter`) are the Rails services it replaced, kept where the
reasoning was written against them — the behaviour is the same, and the Go
export is byte-identical to the Ruby one for the same page.

It carries the layout and nothing else — no visit counts, so the command bar's
ranking starts cold after an import. It is **not a backup format**; that is
`bin/backup_db`'s job. What it is instead is small enough to read and edit by
hand, which is the expected way to fix anything you don't like before importing.

Importing **replaces**: the user's groups are destroyed and the page is built
again from the file, all in one transaction. So the loop this format is for is
*export, look at it, edit the YAML, import again* — and a file that is refused
leaves the page exactly as it was.

## The file

```yaml
# tinystart start page export - 2026-08-10
# 2 columns, 3 groups, 6 tiles
# format: see docs/start-page-format.md
---
1:
- name: Test 2
  items:
    NaN Fonts: https://nanfonts.com
    Feedbin: https://feedbin.com
2:
- name: Lo de siempre
  items:
    My Synology Admin: https://synology.local
    Fastmail: https://app.fastmail.com
- name: Otras cosas
  items:
    YouTube: https://youtube.com
    LinkedIn: https://linkedin.com
```

That file should produce, for the target user:

| Record | Values |
|---|---|
| `users.columns` | `2` |
| group | `name: "Test 2"`, `column: 1`, `position: 0` |
| group | `name: "Lo de siempre"`, `column: 2`, `position: 0` |
| group | `name: "Otras cosas"`, `column: 2`, `position: 1` |
| tiles in "Test 2" | `("NaN Fonts", https://nanfonts.com, position 0)`, `("Feedbin", …, position 1)` |
| tiles in "Lo de siempre" | `("My Synology Admin", …, position 0)`, `("Fastmail", …, position 1)` |
| tiles in "Otras cosas" | `("YouTube", …, position 0)`, `("LinkedIn", …, position 1)` |

All six tiles, matching the header's count — the row for "Lo de siempre" used to
be missing here, which made the table describe a 4-tile page.

## The rules

There are only four.

1. The top level is a **mapping of column number → ordered list of groups**.
   Keys are Integers, 1-based.
2. **A group's index in its list is its position** within that column (0-based).
3. A group is a mapping with exactly two keys, `name` (String) and `items`.
4. `items` is a mapping of **title → url**, and **its order is the tiles'
   order** within the group (0-based).

Permitted types are String, Integer, Hash and Array. No anchors, no aliases, no
tags, no dates. The file is UTF-8 — the real data is in Spanish.

Leading `#` comment lines are informational and safe to ignore entirely. See
[The header](#the-header) if you want to use them as a check.

## Mapping to the schema

| In the file | In tinystart |
|---|---|
| top-level key | `start_page_groups.column` |
| **highest top-level key** | `users.columns` |
| index in the list | `start_page_groups.position` |
| `name` | `start_page_groups.name` |
| `items` key | `start_page_items.title` |
| `items` value | `start_page_items.url` |
| index in `items` | `start_page_items.position` |
| — | `start_page_items.visit_count` — not in the file, leave it at its default of `0` |

## Importing

This is what `StartPageImporter` does, and the order is not stylistic. Steps 1
and 2 are the difference between an import that works and one that fails on its
very first group.

1. **Resolve the target user.** The file contains no user identity at all — the
   importer is told which user out of band. In tinystart that is `current_user`.
2. **Set `user.columns` to the highest top-level key, and do it before creating
   any group.** `users.columns` defaults to **1**, and
   `StartPageGroup#column_within_user_limit` rejects any group whose `column`
   exceeds it. Widening is always safe: `User#columns_leave_no_group_stranded`
   only blocks *shrinking* — and by this point the groups a shrink could strand
   have already been destroyed by step 5, so narrowing is safe here too.
   There is a test that fails with `Column cannot exceed start page column limit
   of 1` if these two are ever reordered.
3. For each column key in ascending order, for each group in list order, create
   the group with its `name` and `column` and **no `position`**.
4. For each entry in `items`, in order, create the tile with its `url` and
   `title` and **no `position`**.
5. Delete the user's existing groups first, and wrap all of it — the delete, the
   column count and every create — in one transaction. See [Re-runs](#re-runs).

Omitting `position` in steps 3 and 4 is the point. `StartPageGroup` has
`before_validation :place_at_end_of_column, on: :create` and `StartPageItem`
has `before_validation :place_at_end_of_group, on: :create`; both fill in the
next position when it is blank. Creating records in file order therefore
reproduces the file's order exactly, with no arithmetic.

### Use the literal key as the column number. Never re-index.

This is the single most likely way to get the import wrong.

Keys can be **non-contiguous**. Empty columns are omitted from the file, so:

```yaml
1:
- name: Left
  items: { ... }
3:
- name: Right
  items: { ... }
```

means *columns 1 and 3 of a three-column page*, with column 2 empty. It does
**not** mean two adjacent columns. An importer that iterates
`data.values.each_with_index` puts "Right" in column 2 and shifts the whole
layout left, silently. Read `column` from the key; derive `users.columns` from
`keys.max`. There is a test pinning this, and it fails when the loop is changed
to walk `data.values` with an index.

### Re-runs

**It replaces, it does not merge.** The user's existing `start_page_groups` are
deleted (items cascade via `dependent: :destroy`) and the page is rebuilt from
the file, inside the transaction from step 5.

Replace is trivially idempotent, which matters because the realistic workflow is
*export, look at it, edit the YAML by hand, import again*. Merging would have to
invent answers for renamed groups and removed tiles that nobody needs.

Two consequences of replacing, both of which have tests:

- **A refusal must change nothing.** Everything is validated before the first
  write, and the delete lives inside the same transaction as the creates, so a
  file that fails on its last tile leaves the page untouched. Dropping the
  transaction makes four tests fail.
- **A file with no groups in it is refused rather than obeyed.** It would be a
  legal instruction to delete everything, which is never what anybody meant by
  picking a file. Checked on the groups, not on the mapping: `1: []` is a mapping
  with a column in it and no groups anywhere, and it used to import happily and
  report "Imported 0 links" as a success.

## Constraints the import must satisfy

All of these are enforced by tinystart's models and schema. Either exporter
guarantees each one, so a file straight out of tinylinks or tinystart will pass —
but a hand-edited file can break any of them, and the importer fails loudly
rather than writing half a page. The message names the group or the tile and
repeats what the model said about it.

| Constraint | Where | Consequence of ignoring it |
|---|---|---|
| `users.columns` must be 1–6 and ≥ every group's `column` | `User` numericality + `StartPageGroup#column_within_user_limit` | Every group past column 1 fails validation |
| Group names unique **per user, across all columns** | `UNIQUE (user_id, name)` + model validation | Second group with a repeated name is rejected |
| Tile urls unique **per group** | `UNIQUE (start_page_group_id, url)` | Second tile with the same url in one group is rejected |
| `url` must parse to `URI::HTTP` / `URI::HTTPS` | `StartPageItem#valid_url` | Rejected |
| `title` and `url` both present | `StartPageItem` presence validations | Rejected |

Two of those deserve elaboration:

- **No URL normalization exists anywhere in tinystart.** Neither the model nor
  `StartPageItemsController` prepends a scheme, strips whitespace or downcases
  anything — whatever is in the file is stored verbatim, and `valid_url` is the
  only gate. A bare `example.com` is rejected outright rather than fixed up. If
  you hand-edit a url, include the scheme.
- **The url uniqueness index is case-sensitive** (plain SQLite BINARY
  collation), while tinylinks compares urls case-insensitively. So
  `https://X.com` and `https://x.com` in one group would both import and render
  as two near-identical tiles rather than being rejected. The exporter cannot
  produce that pair, but a hand-edited file can.

Note also that tinylinks caps a page at 10 groups and tinystart does not. No
import risk — just don't be surprised by the asymmetry.

## What an exporter guarantees

The importer may rely on all of these for a file it did not have to hand-edit.
Each is covered by a test in tinylinks'
`test/services/start_page_export_service_test.rb` and, for tinystart's half, in
`test/services/start_page_exporter_test.rb`.

- Every title is non-empty. A tile whose link had no title uses its url as the
  title, which is what tinylinks renders today; in tinystart `title` is a
  presence-validated column, so there is nothing to fill in.
- **Titles are unique within a group**, numbered where they weren't: a second
  tile called `Fastmail` becomes `Fastmail (2)`, a third `Fastmail (3)`. If you
  see a `(2)` in the file it is this, not corruption — and see the warning in
  the header comment. The suffix is applied to the whole title, so a tile
  genuinely named `Fastmail (2)` that collides becomes `Fastmail (2) (2)`.

  **This is the exporter's job in tinystart too, and it is not optional.**
  tinystart's unique index is `(start_page_group_id, url)`, not on the title, so
  one group really can hold two tiles called the same thing — where tinylinks
  merely made them likely, tinystart makes them legal. A YAML mapping cannot
  hold a repeated key, and Psych keeps the last of two silently, so an
  undeduped export loses a tile outright. Removing the deduping fails five
  tests.
- Group names are unique across the whole file. tinylinks enforces
  `UNIQUE (start_page_id, name)`, which is exactly what tinystart's
  `UNIQUE (user_id, name)` needs, so this holds by construction at both ends.
- Every url parses to `URI::HTTP` / `URI::HTTPS`.
- The highest column key is ≤ 6.
- Groups only ever appear in columns the page actually shows. A group stranded
  beyond the page's width blocks the export in tinylinks; in tinystart it cannot
  arise, because `columns_leave_no_group_stranded` refuses to create the
  situation.

## Parsing notes

- **Load safely.** No aliases, no tags beyond plain scalars, sequences and
  mappings. Ruby's `YAML.safe_load` did this by default; the Go importer walks
  the `yaml.Node` tree and refuses an alias or a `!!timestamp` with the same
  "could not be read as YAML" sentence, so a hand-edited file that tries either
  is turned away with the reason rather than misread.
- **Coerce item keys to text.** In a hand-edited file, an unquoted `123:` key
  is a number to the parser, and a tile can legitimately be called `123`.
  Column keys are the other way round: the importer *requires* them to be
  integers rather than coercing, because that is the version check below. An
  unquoted `2026-01-01:` anywhere is a timestamp tag and is refused before it
  reaches either.
- **Mapping-order preservation is load-bearing.** YAML mappings are formally
  unordered; this format depends on the parser handing back keys in document
  order. Psych does, because Ruby hashes are ordered; the Go importer reads the
  node tree, which is the document's order by construction. A parser that
  doesn't would scramble every tile's position.
- **Duplicate keys: last value wins, first position stays.** That is what
  Psych did silently and what the Go importer does on purpose (`pairs` in
  `import.go`), so a hand edit that repeats a title within one `items` block
  loses a tile with no error from anywhere. The header counts (below) are the
  cheap defence.

## The header

The `#` lines above the `---` marker are comments. Every YAML parser ignores
them, and the format works perfectly if you do too.

```
# tinystart start page export - 2026-08-10
# 2 columns, 3 groups, 6 tiles
# format: see docs/start-page-format.md
# The page is 3 columns wide but nothing is past column 2, so importing this file will set it to 2.
# Renamed "Fastmail" to "Fastmail (2)" in "Lo de siempre" so both tiles survive.
```

Line 1 names the app that wrote it, so a file from tinylinks and a file from
tinystart are told apart at a glance. Line 2 is the counts. Everything after
that is a warning the exporter raised, carried along so it stays with the file —
the two above are the ones tinystart can produce.

**`StartPageImporter` checks line 2 and warns when it disagrees** with what was
loaded — it imports anyway. A mismatch is the only visible symptom of a collapsed
duplicate key, so it is worth saying out loud, but it cannot be a refusal:
deleting a tile by hand lowers the count in exactly the way a collapsed key does,
and nothing in the file says which happened. Refusing would block the one
workflow this format exists for. The warning rides along with the success
message, and a file with no counts line says nothing at all.

Note what the check cannot see: a repeated **group name**. Groups are list items,
not mapping keys, so duplicating one changes no count. That is caught later by
`StartPageGroup`'s uniqueness validation, which names the group properly.

## What is deliberately absent

Do not try to reconstruct any of this; it was never in the file.

- **Visit counts.** Every tile starts at `0`, so the command bar's ranking
  starts cold. Accepted, and accepted again when tinystart started exporting:
  adding them would mean tiles become a mapping rather than `title: url`, and
  this file is for the layout. `bin/backup_db` is what a backup is.
- **Ids**, of any kind. tinystart tiles are self-contained and hold their own
  `url` and `title`; they are not pointers to tinylinks links.
- **The page name.** tinylinks had `start_pages.name` (it said "Start").
  tinystart has no `start_pages` table at all — groups belong to `users`
  directly — so there is nowhere to put it.
- **The user.** See step 1 of Importing.
- **Trailing empty columns.** Because empty columns are omitted, `keys.max` is
  the whole truth about the page's width. A page set to 3 columns with nothing
  in column 3 arrives as a 2-column page. Accepted — but now that a round trip
  is a normal thing to do rather than a one-way migration, tinystart's exporter
  says so in the header rather than letting it be discovered:
  `The page is 3 columns wide but nothing is past column 2, so importing this
  file will set it to 2.`
- **A version field.** Every top-level key in this format is an Integer, so a
  future format with an envelope (a String key like `version:`) is
  distinguishable in one line — `data.keys.all?(Integer)` means this format.
  The absence is a decision, not an oversight.

## Verifying the migration out of tinylinks

From `.claude/rules/done.md`: this is the irreversible step, and it is not done
until it has been checked by eye. The importer checks the counts itself and the
flash repeats them back, so steps 1 and 2 are a glance rather than a count.

1. Tile count in tinystart equals the header's tile count.
2. Group count and `user.columns` likewise.
3. Put tinystart's start page next to https://links.pati.to/ and compare: same
   groups, in the same columns, in the same order; same tiles, in the same
   order, with the same titles.
4. Keep the `.yml` file somewhere safe. After tinylinks' start page is deleted
   it is the only copy of this data outside a database backup.
