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

## Usage

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

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```
