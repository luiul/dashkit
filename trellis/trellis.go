// Package trellis shares full-width column allocation and mouse resizing across
// bubbles/table dashboards. Apps provide plain content targets and readable floors;
// trellis owns padding accounting, weighted growth, and manual resize proportions.
//
// Every border trades width between exactly its two neighbors. Allocation is
// separate from mouse tracking so a poll cannot move borders during a live drag.
// Rendering and header offsets remain in loam; app labels and data stay in callers.
package trellis

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/luiul/dashkit/loam"
)

// GrabWidth is how many terminal columns on either side of a column
// border still count as "close enough" to grab with the mouse. Terminal
// mouse coordinates are cell-granular, never sub-cell, so without some
// slack a user would have to land the click on the exact single-column
// gap between two cells — technically possible, but unforgivingly
// precise for a mouse drag.
const GrabWidth = 1

// DefaultMinWidth is a reasonable floor for a column that doesn't have a
// more specific minimum of its own: narrow enough to still show most
// short words, wide enough that a column doesn't visually disappear.
const DefaultMinWidth = 3

// Model tracks an in-progress mouse-driven column-border drag. The zero
// value is not ready to use; construct one with New.
type Model struct {
	dragCol int // index of the column currently being widened/narrowed; -1 when idle
	lastX   int // most recently applied mouse X, for incremental per-motion deltas
}

// New returns a Model with no drag in progress.
func New() Model {
	return Model{dragCol: -1}
}

// Dragging reports whether a resize gesture is currently in progress.
func (m Model) Dragging() bool {
	return m.dragCol >= 0
}

// Cancel ends the current gesture without changing any saved preferences.
// Call it before projecting a changed terminal width, not for height changes.
func (m *Model) Cancel() {
	m.dragCol = -1
	m.lastX = 0
}

// DragColumn returns the left neighbor of the active border, or -1 when idle.
// Pass it to Preferences.Capture after a changed motion to mark both neighbors.
func (m Model) DragColumn() int {
	return m.dragCol
}

// Handle processes one mouse event against cols — in the same order and
// with the same widths the table was last rendered with — and returns the
// resulting widths (a copy; cols itself is never mutated) plus whether
// they actually changed, i.e. whether the caller needs to call
// table.SetColumns (see Apply) with them.
//
// originX/originY is where cols' own header row starts in the terminal:
// almost always X 0 (neither canopy nor understory indent their table),
// Y is however many lines of title/summary/warning text the caller's own
// View renders above the table — see each app's renderHeader helper,
// which both View and this call share so the two can never drift out of
// sync with each other. A drag can only *start* on that exact header
// row — the same row a spreadsheet gives you a resize handle on — but
// once started keeps tracking the mouse regardless of which row it
// wanders into afterward, exactly like dragging a real window border
// does; canceling a drag never requires the mouse to return to that
// exact row.
//
// mins gives each column's own minimum width, same length/order as cols
// (use DefaultMinWidth for any column that doesn't need a more specific
// one — e.g. one whose shortest possible content is longer than that).
// Every internal border — there are len(cols)-1 of them — is grabbable
// and, once dragged, trades width with exactly the one column next to
// it: the column to the border's left widens by however much the column
// to its right narrows, and vice versa. That keeps the table's own
// total width unchanged no matter which border moves, the same
// spreadsheet-or-file-manager behavior a column border always has,
// without singling any one column out as a dedicated sink. Allocation
// after terminal resize is separate from this gesture. Use Allocate or
// Preferences.Allocate outside Handle.
func (m *Model) Handle(msg tea.MouseMsg, cols []table.Column, mins []int, originX, originY int) ([]int, bool) {
	widths := currentWidths(cols)

	switch msg.Action {
	case tea.MouseActionRelease:
		m.dragCol = -1
		return widths, false

	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return widths, false
		}
		if msg.Y != originY {
			return widths, false
		}
		if i, ok := borderAt(cols, msg.X-originX); ok {
			m.dragCol = i
			m.lastX = msg.X
		}
		return widths, false

	case tea.MouseActionMotion:
		if !m.Dragging() || msg.Button != tea.MouseButtonLeft {
			// Cell-motion mouse mode (the mode both dashboards enable) only
			// ever reports motion while some button is held, so losing the
			// left button mid-drag means it was released somewhere this
			// program didn't get a matching release event for (e.g. focus
			// left the terminal entirely). Treat it the same as a release
			// rather than leaving dragCol stuck on, which would otherwise
			// make the *next* unrelated left-button drag silently resume
			// resizing whatever column this one left behind.
			m.dragCol = -1
			return widths, false
		}
		delta := msg.X - m.lastX
		if delta == 0 {
			return widths, false
		}
		applied := applyDelta(widths, m.dragCol, delta, mins)
		m.lastX += applied
		return widths, applied != 0
	}

	return widths, false
}

// Apply returns a copy of cols with each Width replaced by the
// corresponding entry of widths (same order/length), leaving every other
// field (Title, ...) untouched. A convenience for the common call
// pattern right after Handle reports a change:
//
//	if widths, changed := resizer.Handle(msg, cols, mins, 0, originY); changed {
//		table.SetColumns(trellis.Apply(cols, widths))
//	}
func Apply(cols []table.Column, widths []int) []table.Column {
	out := make([]table.Column, len(cols))
	for i, c := range cols {
		out[i] = c
		if i < len(widths) {
			out[i].Width = widths[i]
		}
	}
	return out
}

func currentWidths(cols []table.Column) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = c.Width
	}
	return widths
}

// borderAt returns the index of whichever column's right-hand border sits
// within GrabWidth of x (the mouse's X position, already relative to the
// table's own left edge), preferring the closest one if more than one
// qualifies. Every column but the last has one (the last has nothing to
// its right to trade width with), so every border in cols is grabbable
// — there's no column singled out as ineligible any more; see Handle's
// own doc for why.
func borderAt(cols []table.Column, x int) (int, bool) {
	if len(cols) < 2 {
		return 0, false
	}
	offsets := loam.ColumnOffsets(cols)

	best := -1
	bestDist := GrabWidth + 1
	for i := 0; i < len(cols)-1; i++ {
		border := offsets[i].Start + offsets[i].Width
		dist := x - border
		if dist < 0 {
			dist = -dist
		}
		if dist <= GrabWidth && dist < bestDist {
			best = i
			bestDist = dist
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// applyDelta widens or narrows widths[dragCol] by delta, taking (or
// giving back) exactly that much width from its right-hand neighbor,
// widths[dragCol+1] — always in range, since borderAt only ever returns
// an index strictly less than len(widths)-1 — so their combined width,
// and therefore the table's total width, never changes. delta is
// clamped, in magnitude, to whichever side has less room to give before
// hitting its own minimum (mins[dragCol] shrinking, mins[dragCol+1]
// growing), and the actual (possibly clamped) delta applied is returned
// so the caller's own drag-tracking (Model.lastX) stays in sync with
// what really happened rather than what was requested.
//
// Room is clamped at zero, never negative: a caller's own layout pass
// may legitimately have a column sitting below its drag minimum already
// (e.g. understory's worktreeColumns lets Path dip below minPathWidth
// on a terminal too narrow for everything else — the column has nothing
// left to give). Without the floor, widths[neighbor]-mins[neighbor]
// comes out negative there, and clamping delta to that negative "room"
// would INVERT the drag: dragging right to widen a column would shrink
// it (and fatten its already-starved neighbor) instead of doing
// nothing.
func applyDelta(widths []int, dragCol, delta int, mins []int) int {
	neighbor := dragCol + 1
	switch {
	case delta > 0:
		if room := max(widths[neighbor]-minOf(mins, neighbor), 0); delta > room {
			delta = room
		}
	case delta < 0:
		if room := max(widths[dragCol]-minOf(mins, dragCol), 0); -delta > room {
			delta = -room
		}
	}
	if delta == 0 {
		return 0
	}
	widths[dragCol] += delta
	widths[neighbor] -= delta
	return delta
}

func minOf(mins []int, i int) int {
	if i >= 0 && i < len(mins) && mins[i] > 0 {
		return mins[i]
	}
	return DefaultMinWidth
}
