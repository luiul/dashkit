# loam

A tiny Go library for coloring and highlighting rows in a
[bubbles/table](https://github.com/charmbracelet/bubbles) view by
post-processing its already-rendered text, rather than putting
ANSI-styled strings into `table.Row` values directly.

This is the rendering substrate shared by [canopy](https://github.com/luiul/canopy)
(agent-session dashboard) and [understory](https://github.com/luiul/understory)
(git-worktree dashboard): both need the same two things from their
table view — recolor certain columns based on each cell's own word, and
highlight whichever row is currently selected — so it lives here once
instead of being duplicated (and drifting) in both trees, the same way
[mycelium](https://github.com/luiul/mycelium) already holds the
open-or-focus-a-window logic both dashboards' Enter key needs.

## Why post-process instead of styling `table.Row` directly

bubbles/table v1's cell truncation (`runewidth.Truncate`) is not
ANSI-aware: escape codes get counted as extra visible width and sliced
mid-sequence, corrupting the row (verified empirically against
bubbles/table v1.0.0 — a styled `"unmerged"` in a 9-wide column gets
truncated with a dangling escape code). Post-processing the table's
already-rendered plain-text view instead sidesteps that entirely: the
widths/padding/truncation the table computes are always over plain
text, and only the final display string gets colored.

## What it does

- **`WordColumn` + `ColorizeRows`** — recolor one or more columns of an
  already-rendered view, each cell picking its style from its own
  (trimmed) word. A `WordColumn.Style` can vary by word (e.g. `"dirty"`
  vs `"clean"`), always return the same style regardless of content
  (e.g. a Since/Updated column), or do its own pre-processing first
  (e.g. strip a trailing blink-marker suffix before deciding the style)
  — it's just a `func(string) lipgloss.Style`, so any of that is a
  caller-side closure, not something `loam` needs to know about.
- **`Sentinel` + `Tag`** — mark whichever row should get a full-line
  highlight by prepending a zero-width Unicode tag to any one of its
  cells (Since/Updated-style columns are a good choice: always
  populated, never blanked for grouping, never truncated in practice).
  `ColorizeRows` finds that row from the *rendered* text — no need to
  track bubbles/table's internal scroll offset, which v1 doesn't expose
  anyway — highlights it, then strips the tag back out before
  returning, so it never reaches the terminal.
- **`HighlightRow` + `StyleSequences`** — the primitive the row
  highlight is built on: wraps an entire line in a style even when that
  line already contains other ANSI (from `RecolorWord`, applied first).
  A naive `open + line + close` wrap breaks the moment `line` contains
  its own reset code, since every `lipgloss` render ends with a full
  SGR reset regardless of which attributes were opened — so
  `HighlightRow` reapplies its own opening sequence right after every
  such inner reset it finds, keeping the outer style in effect up to
  the real, final close.
- **`ColumnOffsets` + `RecolorWord` + `DisplayColumnToByteOffset`** —
  the lower-level pieces: computing each column's start/width within a
  rendered line (accounting for bubbles/table's fixed padding), and
  recoloring one column's span by *display* column rather than byte
  offset, so a multi-byte rune in an earlier column (a truncation
  ellipsis, or a genuinely unicode name) never misaligns a later
  column's recoloring.

## Usage

```go
cols := []table.Column{
	{Title: "Updated", Width: 8},
	{Title: "Worktree", Width: 8},
	{Title: "Merge", Width: 9},
}

// Row building: tag the selected row via loam.Tag.
row := table.Row{
	loam.Tag(humanizeSince(e.CommitTime), i == cursor),
	worktreeStatusLabel(e),
	mergeStatusLabel(e),
}

// View: recolor Worktree/Merge, and highlight whichever row is tagged.
view := loam.ColorizeRows(table.View(), table.Columns(), []loam.WordColumn{
	{Index: colWorktree, Style: worktreeStatusStyle},
	{Index: colMerge, Style: mergeStatusStyle},
}, rowHighlightStyle)
```

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```
