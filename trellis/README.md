# trellis

A tiny Go library for resizing a [bubbles/table](https://github.com/charmbracelet/bubbles)
view's columns with the mouse: click and drag a column's border to widen
or narrow it, the same way a spreadsheet or file manager does.

This is the shared mouse-resize logic [canopy](https://github.com/luiul/canopy)
(agent-session dashboard) and [understory](https://github.com/luiul/understory)
(git-worktree dashboard) both grow their dashboards from: both wanted the
same click-drag-a-border behavior for their tables, so it lives here once
instead of being written twice and quietly drifting apart — the same
reasoning that already pulled [mycelium](https://github.com/luiul/mycelium)
(open-or-focus-a-window) and [loam](https://github.com/luiul/loam)
(row/column rendering) out of both trees.

A trellis, in the garden sense, is the lattice of fixed bars a climbing
plant is trained against — a structure whose whole point is that its grid
can be adjusted without disturbing what grows on it. That's this
package's one job too: rearrange a table's column widths in place, in
response to a mouse drag, without needing to know anything about what a
caller actually put in those columns.

## What it does

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
  that way, but it meant one column's own border stopped responding to a
  drag while every other border secretly resized that one distant column
  instead of its actual neighbor. Which column (if any) fills whatever's
  left over after a *terminal* resize is a separate policy entirely,
  applied by the caller outside Handle (see each dashboard's own
  resizeColumns/worktreeColumns).
- **`mins`** — a floor per column below which a drag won't shrink it, so
  dragging never truncates a column's own title or its shortest realistic
  content out of legibility.
- **`Apply`** — a small helper that copies a `[]table.Column` with new
  widths spliced in, for the `table.SetColumns` call right after a
  `Handle` that reported a change.

## Usage

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
always changes two of them at once, not only the one at DragColumn() —
see its own doc) and reapply it each time you rebuild columns, the same
way both dashboards' own README documents.

Users still need some way to *see* where a border actually is before
they can grab it — see [`loam`](../loam)'s `DrawHeaderBorders`, which
marks each of these same border positions with a visible divider on the
table's header row.

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```
