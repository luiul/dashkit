# trellis

Shared column layout and mouse resizing for [bubbles/table](https://github.com/charmbracelet/bubbles), used by [Canopy](https://github.com/luiul/canopy) and [Understory](https://github.com/luiul/understory).

## Why this needs to exist

Both dashboards need compact status fields, useful text that fits before blank space grows, and borders that stay stable after a mouse drag. One allocator keeps their padding, rounding, and terminal resize rules consistent. App labels and data stay in the apps. Rendering stays in [loam](../loam/README.md).

## Automatic layout

`Allocate(viewport, policies)` returns content widths and a `fits` flag. The viewport includes two padding cells per column, shared with `loam.ColumnOffsets` through `loam.CellPadding`.

Each `ColumnPolicy` has:

- `Minimum`: the normal readable target.
- `HardMinimum`: the smallest readable width, always at least one cell.
- `Preferred`: the full rendered content width, measured with `ContentWidth` before tagging or styling rows.
- `Weight`: the surplus growth weight. Zero keeps a field compact.
- `ShrinkPriority`: higher values yield space first and receive preferred content last.

The allocator funds compact fields and normal text targets first. It then funds measured content in priority order. Higher-priority useful labels can reclaim a lower-priority text column's normal target down to its hard floor. Equal priorities share their content deficits. Only after useful content fits does it split surplus by growth weight. Largest-remainder rounding breaks ties by column order, so no cell is lost.

On tight terminals, text gives up its normal target before compact fields shrink. All columns keep their hard floors. Below the combined hard minimum, `fits` is false and the returned widths exceed the viewport. Show a terminal-too-narrow notice and keep the clipped fallback. Do not hide columns or claim they fit. If no column has a growth weight, the last column takes surplus to keep the viewport exact.

Measure the full unfiltered row set. Use display labels, including shortened home paths and suffixes. `ContentWidth` counts display cells and removes ANSI and the selection sentinel. Empty-state messages must not affect preferred widths.

## Mouse preferences

`Model.Handle` tracks a gesture. Every internal border trades width between exactly its two neighbors. Total width stays constant. `mins` supplies their hard floors. A press must be on the header row. Later motion can leave that row.

`Preferences` has an automatic zero value. After a changed motion, call `Capture(widths, policies, resizer.DragColumn())`. This records a touched-column mask and integer desired proportions for the stretch pool. Untouched compact fields stay compact. Narrowed compact fields keep their chosen targets. Widened compact fields join the pool.

Use `Preferences.Allocate` on later polls and width changes. Manual proportions take priority over new text lengths, including untouched stretch columns. Longer labels may truncate. Minimum violations are clamped and the deficit is redistributed. Projection never changes the original desired proportions, so wide/narrow/wide cycles have no drift.

While dragging, update rows with frozen columns. Reallocate on release or lost-button motion. On a changed terminal width, call `Model.Cancel` before projection. A same-width or height-only update leaves the gesture and widths unchanged.

Preferences stay in memory. Restart returns to automatic sizing. There is no reset key or saved configuration.

## Usage

```go
// In the app model:
resizer := trellis.New()
preferences := trellis.Preferences{}

// Before rendering, unless a gesture is active:
widths, fits := preferences.Allocate(viewport, policies)
t.SetColumns(trellis.Apply(t.Columns(), widths))
// If !fits, show a too-narrow notice.

// In Update:
case tea.MouseMsg:
    cols := m.table.Columns()
    wasDragging := m.resizer.Dragging()
    _, originY := m.renderHeader()
    if widths, changed := m.resizer.Handle(msg, cols, hardMinimums, 0, originY); changed {
        m.preferences.Capture(widths, policies, m.resizer.DragColumn())
        m.table.SetColumns(trellis.Apply(cols, widths))
    }
    if wasDragging && !m.resizer.Dragging() {
        m.resizeColumns()
    }
    return m, nil
```

`originY` must count every line above the table, including notices and blank separators. Guard mouse events while help or confirmation overlays are open. Keep safe row/column update ordering when rebuilding tables.

Enable mouse support with `tea.WithMouseCellMotion()`. Mark header borders with `loam.DrawHeaderBorders`, which uses the same column offsets as mouse hit testing.

## Development

```bash
go build ./...
go vet ./...
go test -race ./...
gofmt -l .
```
