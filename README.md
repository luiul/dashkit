# dashkit

Shared Go helper packages for [canopy](https://github.com/luiul/canopy)
(agent-session dashboard) and [understory](https://github.com/luiul/understory)
(git-worktree dashboard) — the two terminal dashboards that grow this
kit. Both are Bubble Tea apps built around a `bubbles/table`, both need
the exact same handful of things from it (mouse column-resize, row
coloring/highlighting, open-or-focus-a-window on Enter, the destructive-
action confirmation modal), so those live here once instead of being
written twice and quietly drifting apart in each tree.

[CONVENTIONS.md](CONVENTIONS.md) states the keybinding, confirmation-modal,
phrasing, rendering, testing, and releasing conventions all three repos
follow: the decisions behind the packages below, written down once.

Each package below is self-contained and independently importable:

```go
import (
	"github.com/luiul/dashkit/trellis"
	"github.com/luiul/dashkit/loam"
	"github.com/luiul/dashkit/mycelium"
	"github.com/luiul/dashkit/confirm"
	"github.com/luiul/dashkit/sieve"
)
```

## trellis: column layout and mouse resizing

Shared full-width layout and mouse resizing for [bubbles/table](https://github.com/charmbracelet/bubbles). App labels stay in the apps. Trellis owns allocation, padding accounting, and manual proportions.

- `ColumnPolicy` defines normal and hard minima, preferred content width, surplus weight, and shrink priority.
- `Allocate` fits useful content first, then shares surplus among text columns. Widths plus cell padding fill the viewport exactly when the hard floors fit.
- `ContentWidth` measures plain display cells without ANSI or cursor sentinels. Measure the full unfiltered row set, not placeholder messages.
- `Model.Handle` trades width between exactly the two neighbors of the grabbed header border. Total width stays constant.
- `Preferences.Capture` records a changed drag. `Preferences.Allocate` preserves desired proportions across polls and terminal resizes without drift.
- `Model.Cancel` stops a gesture before a width change. Keep columns frozen during a drag. Height-only updates leave the gesture intact.
- Preferences live in memory only. Restart returns to automatic sizing. Below the combined hard floor, show a terminal-too-narrow notice and keep the clipped fallback.

See [trellis usage and development](trellis/README.md) for the API and integration rules. Enable `tea.WithMouseCellMotion()` and draw header borders with `loam.DrawHeaderBorders`.

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
window (VS Code or a Ghostty terminal) on a given filesystem path,
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

`mycelium.OpenVSCode` checks for an already-open window itself first
and focuses the matched window directly, so the CLI is only ever asked
to *open*: once "no window is open on path" is established, `code -n
path` forces a genuinely new window, and `--reuse-window` (the call
that hijacks) is never made. Window identity is the window title: the
dotfiles `window.title` setting renders each title as the opened
folder's full path plus the branch, so matching is a folder-path match
over one System Events listing, and focusing is an `AXRaise` of the
exact window whose title matched. Identification and focus bind to the
same window, with no intermediary re-matching. That makes it safe to
call repeatedly on the same never-before-seen path: the already-open
check finds the window `OpenVSCode` itself just created on every
subsequent call, so nothing stacks up duplicate windows.

`mycelium.OpenGhostty` does the equivalent for a bare Ghostty tab,
matching by working directory (Ghostty's `tty`/`pid` AppleScript
properties don't reliably work as of Ghostty 1.3.1; working directory
does).

`mycelium.SnapshotVSCode` is the read-only half: one queryable snapshot
of which VS Code windows are open, for dashboards showing per-row
window state (the "VS Code open?" columns) rather than acting on one
selected row. Each `IsOpen(path)` runs the exact same match
`OpenVSCode` does, so the column says "open" precisely when Enter would
focus an existing window instead of opening a new one. An empty or
failed listing while Code runs (macOS culls the accessibility tree of a
backgrounded app) is reported by `Err()` and makes every `IsOpen`
false, so callers render "can't tell" rather than "definitely not
open".

### Usage

```go
import "github.com/luiul/dashkit/mycelium"

result := mycelium.OpenVSCode("/Users/you/code/some-repo")
// result.OK, result.Message

result = mycelium.OpenGhostty("/Users/you/code/some-repo")

snapshot := mycelium.SnapshotVSCode() // one window listing per poll
open := snapshot.IsOpen("/Users/you/code/some-repo")
```

Both currently macOS-only: window detection shells out to `osascript`
on both sides, and there is no equivalent implementation for other
platforms yet.

### Errors

A failed AppleScript call surfaces as `*mycelium.AutomationError`,
or more specifically `*mycelium.AutomationPermissionError` when it looks
like macOS's Automation permission for scripting the target app hasn't
been granted yet (System Settings → Privacy & Security → Automation).
`Result.Message` is already a human-readable rendering of either, meant
to be shown straight to a user (e.g. as a TUI notification): callers
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

## sieve — fuzzy row filtering

The one matching rule behind both dashboards' `/` row filter, the same
gesture jira-today's fzf picker uses: every character of the query must
appear in order somewhere in the row's text (a subsequence match,
case-insensitive), so `cnp` matches "canopy" without the query having
to appear verbatim anywhere. A garden sieve separates the soil you keep
from what you toss; this one separates matching rows from the rest.

The filter *input* (the textinput, the modal `/`-entered key routing)
stays in each app; `sieve.Match(query, cells...)` is only the shared
"does this row match this query" answer, run against a row's plain cell
strings, never the rendered colorized view (whose cursor sentinels and
ANSI styling are not matchable text).

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
