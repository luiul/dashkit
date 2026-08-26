# dashkit

Shared Go helper packages for [canopy](https://github.com/luiul/canopy)
(agent-session dashboard) and [understory](https://github.com/luiul/understory)
(git-worktree dashboard) — the two terminal dashboards that grow this
kit. Both are Bubble Tea apps built around a `bubbles/table`, both need
the exact same handful of things from it (mouse column-resize, row
coloring/highlighting, open-or-focus-a-window on Enter), so those live
here once instead of being written twice and quietly drifting apart in
each tree.

Each package below is self-contained and independently importable:

```go
import (
	"github.com/luiul/dashkit/trellis"
	"github.com/luiul/dashkit/loam"
	"github.com/luiul/dashkit/mycelium"
)
```

## trellis — mouse column-resize

A tiny Go library for resizing a [bubbles/table](https://github.com/charmbracelet/bubbles)
view's columns with the mouse: click and drag a column's border to widen
or narrow it, the same way a spreadsheet or file manager does.

A trellis, in the garden sense, is the lattice of fixed bars a climbing
plant is trained against — a structure whose whole point is that its grid
can be adjusted without disturbing what grows on it. That's this
package's one job too: rearrange a table's column widths in place, in
response to a mouse drag, without needing to know anything about what a
caller actually put in those columns.

### What it does

- **`Model` + `Handle`** — tracks one in-progress drag at a time. Feed it
  every `tea.MouseMsg` your `Update` receives; it tells you the resulting
  column widths and whether they changed. A drag can only *start* on the
  table's own header row (the same row a spreadsheet gives you a resize
  handle on), but once started keeps tracking the mouse regardless of
  which row it wanders into afterward — exactly like dragging a real
  window border.
- **A `flex` column absorbs every change** — resizing any other column
  takes width from (or gives it back to) whichever column you designate
  as `flex`, so the table's total width never changes, only how it's
  divided up. That's the same "whatever's left over" column
  canopy/understory's own column-width functions already compute
  (Location/Path respectively) — trellis just keeps it in sync with
  whatever the user just dragged.
- **`mins`** — a floor per column below which a drag won't shrink it, so
  dragging never truncates a column's own title or its shortest realistic
  content out of legibility.
- **`Apply`** — a small helper that copies a `[]table.Column` with new
  widths spliced in, for the `table.SetColumns` call right after a
  `Handle` that reported a change.

### Usage

```go
resizer := trellis.New()   // in your Model's own zero-value construction

// In Update, alongside your other tea.Msg cases:
case tea.MouseMsg:
    cols := m.table.Columns()
    mins := []int{6, 6, 8, 8, 9, 20} // one per column, in the same order
    flex := colPath                  // the column that fills whatever's left
    _, originY := m.renderHeader()   // however many lines precede the table
    if widths, changed := m.resizer.Handle(msg, cols, mins, flex, 0, originY); changed {
        m.table.SetColumns(trellis.Apply(cols, widths))
    }
    return m, nil
```

Mouse events only reach `Update` once the program itself is built with
mouse support enabled:

```go
p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
```

A resized column's width is a live UI decision, not model data: nothing
here persists it to disk. If your own dashboard rebuilds columns from
scratch on every poll or terminal resize (as both canopy and understory
do — content-driven column widths, a `flex` column recomputed from
whatever's left of the terminal width), keep the user's last resize
around yourself (e.g. a `map[int]int` of column index → override width,
in your own `Model`) and reapply it each time you rebuild columns, the
same way both dashboards' own README documents.

## loam — row/column coloring and highlighting

A tiny Go library for coloring and highlighting rows in a
[bubbles/table](https://github.com/charmbracelet/bubbles) view by
post-processing its already-rendered text, rather than putting
ANSI-styled strings into `table.Row` values directly.

### Why post-process instead of styling `table.Row` directly

bubbles/table v1's cell truncation (`runewidth.Truncate`) is not
ANSI-aware: escape codes get counted as extra visible width and sliced
mid-sequence, corrupting the row (verified empirically against
bubbles/table v1.0.0 — a styled `"unmerged"` in a 9-wide column gets
truncated with a dangling escape code). Post-processing the table's
already-rendered plain-text view instead sidesteps that entirely: the
widths/padding/truncation the table computes are always over plain
text, and only the final display string gets colored.

### What it does

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

### Usage

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

## mycelium — open-or-focus a window

A tiny Go library that opens, or focuses if one is already open, an app
window (VS Code or a Ghostty terminal) on a given filesystem path —
without ever risking a duplicate window for a path that's already open
somewhere.

### Why this needs to exist at all

`code --reuse-window <path>` alone isn't enough to get real
switch-or-create behavior out of the `code` CLI: it only reuses the
right window when one already has that exact folder open, and silently
hijacks whichever window was last active otherwise, rather than opening
a fresh one. Confirmed both empirically and in upstream reports
([microsoft/vscode#121926](https://github.com/microsoft/vscode/issues/121926),
[#216602](https://github.com/microsoft/vscode/issues/216602),
[#215749](https://github.com/microsoft/vscode/issues/215749)).

`mycelium.OpenVSCode` checks for an already-open window itself first,
via each window's title over AppleScript (System Events), and only ever
falls through to the CLI once that's ruled out — forcing a genuinely new
window (`-n`) instead of handing `--reuse-window` a chance to guess
wrong. That makes it safe to call repeatedly on the same
never-before-seen path: the already-open check finds the window
`OpenVSCode` itself just created on every subsequent call, so nothing
stacks up duplicate windows.

`mycelium.OpenGhostty` does the equivalent for a bare Ghostty tab,
matching by working directory (Ghostty's `tty`/`pid` AppleScript
properties don't reliably work as of Ghostty 1.3.1; working directory
does).

### Usage

```go
import "github.com/luiul/dashkit/mycelium"

result := mycelium.OpenVSCode("/Users/you/code/some-repo")
// result.OK, result.Message

result = mycelium.OpenGhostty("/Users/you/code/some-repo")
```

Both currently macOS-only: window detection shells out to `osascript`.
Ghostty's own scripting dictionary and VS Code's `System Events` window
titles are both macOS-specific; there's no equivalent implementation for
other platforms yet.

### Errors

A failed AppleScript call surfaces as `*mycelium.AutomationError`, or
more specifically `*mycelium.AutomationPermissionError` when it looks
like macOS's Automation permission for scripting the target app hasn't
been granted yet (System Settings → Privacy & Security → Automation).
`Result.Message` is already a human-readable rendering of either, meant
to be shown straight to a user (e.g. as a TUI notification) — callers
don't need to inspect the error types themselves unless they want to
branch on them.

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```

## History

`trellis`, `loam`, and `mycelium` started as three separate repos, each
imported independently by canopy and understory. They were merged into
this one repo (with each package's original commit history preserved
under its own subdirectory) once it became clear all three existed for
the same single reason — shared dashboard behavior with exactly two
consumers — and were paying triple the go.mod/go.sum/README/LICENSE/tag
overhead for it. The three original repos are archived; see each for
their pre-merge history.

## License

MIT
