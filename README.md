# dashkit

Shared Go helper packages for [canopy](https://github.com/luiul/canopy)
(agent-session dashboard) and [understory](https://github.com/luiul/understory)
(git-worktree dashboard) — the two terminal dashboards that grow this
kit. Both are Bubble Tea apps built around a `bubbles/table`, both need
the exact same handful of things from it (mouse column-resize, row
coloring/highlighting, open-or-focus-a-window on Enter, the destructive-
action confirmation modal), so those live here once instead of being
written twice and quietly drifting apart in each tree.

Each package below is self-contained and independently importable:

```go
import (
	"github.com/luiul/dashkit/trellis"
	"github.com/luiul/dashkit/loam"
	"github.com/luiul/dashkit/mycelium"
	"github.com/luiul/dashkit/confirm"
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
- **Every border behaves the same way** — drag it, and the two columns
  it sits between trade width between themselves; nothing else moves, so
  the table's total width never changes no matter which border is
  dragged. There's no dedicated "flex" column that silently absorbs
  every other column's drag — an earlier version of this package worked
  that way, but it meant one column's own border stopped responding to
  a drag while every other border secretly resized that one distant
  column instead of its actual neighbor. Which column (if any) fills
  whatever's left over after a *terminal* resize is a separate policy
  entirely, applied by the caller outside Handle (see each dashboard's
  own resizeColumns/worktreeColumns).
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
    _, originY := m.renderHeader()   // however many lines precede the table
    if widths, changed := m.resizer.Handle(msg, cols, mins, 0, originY); changed {
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
do — content-driven column widths, one column recomputed from whatever's
left of the terminal width), keep the user's last resize around yourself
(e.g. a `map[int]int` of column index → override width, in your own
`Model`, recording *every* index in the widths Handle returned — a drag
always changes two of them at once, not only the one at DragColumn()—
see its own doc) and reapply it each time you rebuild columns, the same
way both dashboards' own README documents.

Users still need some way to *see* where a border actually is before they
can grab it — see loam's `DrawHeaderBorders` below, which marks each of
these same border positions with a visible divider on the table's header
row.

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
- **`DrawHeaderBorders`** — marks each internal column border with a
  visible divider (`BorderGlyph`, "│") on the table's header row, so a
  user actually has something to aim a mouse drag at (see trellis above)
  instead of an invisible 2-space gap. Header-row-only, deliberately: a
  data row may already carry other ANSI from `RecolorWord`/`HighlightRow`
  (see the coloring hazard both of those already have to work around
  above), where the header line never does in either dashboard — so
  marking only the header sidesteps that hazard entirely rather than
  needing its own ANSI-aware column walk.

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

// View: recolor Worktree/Merge, highlight whichever row is tagged, then
// mark the header's own column borders so there's something to drag.
view := loam.ColorizeRows(table.View(), table.Columns(), []loam.WordColumn{
	{Index: colWorktree, Style: worktreeStatusStyle},
	{Index: colMerge, Style: mergeStatusStyle},
}, rowHighlightStyle)
view = loam.DrawHeaderBorders(view, table.Columns(), subtleStyle)
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

## confirm — the destructive-action confirmation modal

The shared state machine behind both dashboards' confirmation prompts
(understory's `x/X/P/M` worktree removal, canopy's `x/X/D` session
signaling): one answer discipline, one auto-cancel timeout, one
poll-revalidation hook, so the two modals cannot drift apart.

Unlike the other packages, the name is plainly descriptive rather than a
garden metaphor: the package is exactly what it says.

### What it does

- **`Classify`** — the one answer set, in one place: `y` confirms; `n`,
  `esc`, or `enter` cancel (honoring the prompt's `[y/N]`); every other
  key is swallowed; `ctrl+c` quits, as it does from everywhere.
- **`State[T]`** — the modal half of a prompt: the armed payload (the
  caller's own type: a worktree batch, a process list) plus the token
  its auto-cancel tick must match. `Arm` opens and schedules the tick,
  `Resolve` closes and invalidates it, `Tick` fires the timeout only for
  the prompt it was scheduled for.
- **`Timeout` + `TimeoutText`** — an unanswered prompt cancels itself
  after 10s, with both apps showing the same notification text.
- **`Refresh`** — the poll-revalidation hook: re-stamps an armed
  prompt's targets against each fresh poll, dropping the ones that
  vanished in the meantime, so a prompt never fires at rows that no
  longer mean what the user thought.

See [confirm/README.md](confirm/README.md) for the full discipline and a
usage walkthrough.

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
their pre-merge history. `confirm` joined later, born here, when the two
dashboards' confirmation modals were aligned on one discipline (see
[understory#4](https://github.com/luiul/understory/issues/4)) and the
machinery was extracted so they cannot drift again.

## License

MIT
