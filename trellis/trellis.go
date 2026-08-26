// Package trellis is the shared mouse-driven column-resize logic canopy
// and understory both grow their bubbles/table dashboards from: click and
// drag a column's border to widen or narrow it, the same way a spreadsheet
// or file manager lets you resize a column with the mouse.
//
// A trellis, in the garden sense, is the lattice of fixed bars a climbing
// plant is trained against — a structure whose whole point is that its
// grid can be adjusted without disturbing what grows on it. That's the
// same shape as this package's one job: rearrange a bubbles/table's
// column widths in place, in response to a mouse drag, without needing to
// know anything about what a caller actually put in those columns.
//
// This is a peer to the loam package (the row/column rendering
// substrate both dashboards already share) and the mycelium package
// (the open-or-focus-a-window logic both dashboards' Enter key already
// shares): the same kind of small, focused extraction, once two
// independent trees needed the identical behavior rather than two copies
// of it quietly drifting apart. It depends on loam for one thing only —
// ColumnOffsets, so it doesn't need to re-derive bubbles/table's fixed
// 1-space cell padding on its own — and is otherwise self-contained.
//
// Every border between two adjacent columns behaves identically: drag it
// and those two columns trade width between themselves, nothing else
// moves. An earlier version of this package instead routed every drag
// through a single caller-designated "flex" column, on the idea that it
// was the same "whatever's left over" column a caller's own terminal-
// resize logic already computes — but that only reads as one border
// among many when the flex column happens to be last (nothing to its
// right, so it never has a border of its own to seem inconsistent
// about). The moment a caller's flex column sits in the middle (more
// columns to its right), that design meant *every* drag anywhere in the
// table silently resized that one distant column instead of the two
// columns actually straddling the border being dragged, and the flex
// column's own right-hand border didn't respond to a drag at all — both
// surprising, given every other border did respond, and both gone now
// that a drag only ever touches its own two adjacent columns. A
// terminal-resize-driven "give the leftover space to this one column"
// policy is still entirely reasonable; it just isn't Handle's concern —
// see each app's own resizeColumns/worktreeColumns for where that now
// lives on its own, decoupled from mouse dragging entirely.
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

// DragColumn returns the index of the column currently being resized, or
// -1 if Dragging is false. A drag always changes exactly two adjacent
// columns at once (DragColumn and DragColumn+1; see Handle's own doc),
// so a caller that persists a resize as an override (see canopy/
// understory's own colOverrides map) should record both of the widths
// Handle just returned, not only the one at this index — DragColumn is
// mostly useful for tests and diagnostics that only care which border
// is currently being dragged.
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
// without singling any one column out as a dedicated sink the way an
// earlier version of this package did — see this package's own doc for
// why that design didn't hold up once a caller's flex column wasn't
// also its last one. Which column (if any) absorbs the *leftover* space
// from a terminal resize is a separate policy entirely, applied by the
// caller outside Handle (see each app's own resizeColumns/
// worktreeColumns); Handle only ever moves width between two adjacent
// columns in response to a drag.
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
//	if widths, changed := resizer.Handle(msg, cols, mins, flex, 0, originY); changed {
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
func applyDelta(widths []int, dragCol, delta int, mins []int) int {
	neighbor := dragCol + 1
	switch {
	case delta > 0:
		if room := widths[neighbor] - minOf(mins, neighbor); delta > room {
			delta = room
		}
	case delta < 0:
		if room := widths[dragCol] - minOf(mins, dragCol); -delta > room {
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
