package trellis

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// threeCols mirrors a small slice of a real dashboard's columns. Offsets
// (see loam.ColumnOffsets): A starts at 1 width 5 (border at 6), B
// starts at 8 width 5 (border at 13), C starts at 15 width 10.
func threeCols() []table.Column {
	return []table.Column{
		{Title: "A", Width: 5},
		{Title: "B", Width: 5},
		{Title: "C", Width: 10},
	}
}

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}

func release(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease}
}

func TestHandleIgnoresPressOffTheHeaderRow(t *testing.T) {
	m := New()
	cols := threeCols()
	widths, changed := m.Handle(press(6, 5), cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false (wrong Y)")
	}
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (wrong Y)")
	}
	if got, want := widths, []int{5, 5, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

func TestHandleIgnoresPressFarFromAnyBorder(t *testing.T) {
	m := New()
	cols := threeCols()
	_, changed := m.Handle(press(2, 0), cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false (not near a border)")
	}
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (not near a border)")
	}
}

func TestHandleStartsDragOnHeaderRowNearABorder(t *testing.T) {
	m := New()
	cols := threeCols()
	_, changed := m.Handle(press(6, 0), cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false (press alone never changes widths)")
	}
	if !m.Dragging() {
		t.Fatalf("Dragging() = false, want true")
	}
	if got := m.DragColumn(); got != 0 {
		t.Fatalf("DragColumn() = %d, want 0", got)
	}
}

func TestHandleGrabWidthToleratesAnOffByOneClick(t *testing.T) {
	m := New()
	cols := threeCols()
	// Border for column 0 is at x=6; GrabWidth is 1, so x=7 should still grab it.
	_, changed := m.Handle(press(7, 0), cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if got := m.DragColumn(); got != 0 {
		t.Fatalf("DragColumn() = %d, want 0 (off-by-one click should still grab it)", got)
	}
}

func TestHandleDragWidensColumnAndNarrowsItsRightNeighbor(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)

	widths, changed := m.Handle(motion(9, 0), cols, nil, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	// The requested +3 clamps to +2: B's own floor (DefaultMinWidth, 3,
	// since mins is nil here) only has 2 to give before B itself would go
	// under it.
	if got, want := widths, []int{7, 3, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (dragCol +2, its right neighbor -2, C untouched)", got, want)
	}
}

func TestHandleDragNarrowsColumnAndWidensItsRightNeighbor(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)

	widths, changed := m.Handle(motion(4, 0), cols, nil, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{3, 7, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (dragCol -2, its right neighbor +2, C untouched)", got, want)
	}
}

func TestHandleDragOnlyEverTouchesTheTwoColumnsStraddlingTheDraggedBorder(t *testing.T) {
	m := New()
	cols := threeCols()
	// Border for column 1 (between B and C) is at x=13.
	m.Handle(press(13, 0), cols, nil, 0, 0)

	widths, changed := m.Handle(motion(16, 0), cols, nil, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{5, 8, 7}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (A untouched, B +3, C -3)", got, want)
	}
}

func TestHandleDragClampsAtDragColumnsMinimum(t *testing.T) {
	m := New()
	cols := threeCols()
	mins := []int{4, 0, 0} // column 0 can't go below 4
	m.Handle(press(6, 0), cols, mins, 0, 0)

	// Ask to shrink column 0 by 5 (well past its floor of 4, i.e. width 0).
	widths, changed := m.Handle(motion(1, 0), cols, mins, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths[0], 4; got != want {
		t.Fatalf("widths[0] = %d, want %d (clamped at its own minimum)", got, want)
	}
	if got, want := widths[1], 6; got != want {
		t.Fatalf("widths[1] = %d, want %d (right neighbor only grew by what column 0 actually gave up)", got, want)
	}
	if got, want := widths[2], 10; got != want {
		t.Fatalf("widths[2] = %d, want %d (not the dragged border's neighbor: untouched)", got, want)
	}
}

func TestHandleDragClampsAtItsRightNeighborsMinimum(t *testing.T) {
	m := New()
	cols := threeCols()
	mins := []int{0, 4, 0} // column 1 (the drag's right neighbor) can't go below 4, only 1 to give
	m.Handle(press(6, 0), cols, mins, 0, 0)

	widths, changed := m.Handle(motion(20, 0), cols, mins, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths[1], 4; got != want {
		t.Fatalf("widths[1] = %d, want %d (clamped at its own minimum)", got, want)
	}
	if got, want := widths[0], 6; got != want {
		t.Fatalf("widths[0] = %d, want %d (dragCol only grew by what its neighbor actually gave up)", got, want)
	}
	if got, want := widths[2], 10; got != want {
		t.Fatalf("widths[2] = %d, want %d (untouched)", got, want)
	}
}

func TestHandleDragTowardANeighborAlreadyBelowItsMinimumIsANoOp(t *testing.T) {
	// A caller's own layout pass can legitimately leave a column below
	// its drag minimum (e.g. understory lets its Path column dip below
	// minPathWidth on a terminal too narrow for everything else). That
	// column has nothing to give: dragging its left-hand border toward
	// it must be a no-op, NOT clamp the delta to the negative "room" and
	// thereby invert the drag (widening the starved column by shrinking
	// the dragged one — the exact regression this test pins down).
	m := New()
	cols := threeCols()
	cols[2].Width = 6 // C sits below its own minimum of 10 already
	mins := []int{0, 0, 10}
	m.Handle(press(13, 0), cols, mins, 0, 0) // the B/C border

	// Drag right, asking to widen B at C's expense: C has nothing to
	// give, so nothing may move — least of all in the opposite direction.
	widths, changed := m.Handle(motion(18, 0), cols, mins, 0, 0)
	if changed {
		t.Fatalf("changed = true (widths %v), want a no-op: C is already below its minimum", widths)
	}
	if got, want := widths[1], 5; got != want {
		t.Fatalf("widths[1] = %d, want %d (unchanged)", got, want)
	}
	if got, want := widths[2], 6; got != want {
		t.Fatalf("widths[2] = %d, want %d (unchanged)", got, want)
	}
}

func TestHandleDragAwayFromANeighborBelowItsMinimumStillWorks(t *testing.T) {
	// The mirror gesture: the dragged column itself is the one sitting
	// below its minimum. Dragging it narrower still must not invert —
	// but dragging it wider (away from the starved direction) works
	// normally, taking from a neighbor that does have room.
	m := New()
	cols := threeCols()
	cols[1].Width = 3 // B sits below its own minimum of 5 already; the B/C border sits at 8+3=11
	mins := []int{0, 5, 0}
	m.Handle(press(11, 0), cols, mins, 0, 0) // the B/C border

	// Drag left, asking to shrink B further: no room, no-op.
	widths, changed := m.Handle(motion(10, 0), cols, mins, 0, 0)
	if changed {
		t.Fatalf("changed = true (widths %v), want a no-op: B is already below its minimum", widths)
	}

	// Drag right: B widens at C's expense, as usual (C has 7 to give).
	widths, changed = m.Handle(motion(17, 0), cols, mins, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true (C has room to give)")
	}
	if got, want := widths[1], 9; got != want {
		t.Fatalf("widths[1] = %d, want %d (B widened by the drag)", got, want)
	}
	if got, want := widths[2], 4; got != want {
		t.Fatalf("widths[2] = %d, want %d (C gave up what B gained)", got, want)
	}
}

func TestHandleDragAccumulatesAcrossMultipleMotionEvents(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)

	// Real usage applies each motion's returned widths back onto cols (via
	// Apply) before the next event, the same as SetColumns keeping the live
	// table in sync — so this mirrors that instead of calling Handle twice
	// against the same stale cols.
	// B's own floor (DefaultMinWidth, 3, since mins is nil here) only has
	// 2 to give up in total, so these two +1 motions are chosen to land
	// exactly on that limit rather than needing a third, clamped one.
	widths, _ := m.Handle(motion(7, 0), cols, nil, 0, 0)
	cols = Apply(cols, widths)

	widths, changed := m.Handle(motion(8, 0), cols, nil, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{7, 3, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (two +1 motions should sum to +2 total)", got, want)
	}
}

func TestHandleDragIgnoresRowLeavingTheHeaderOnceStarted(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)

	// Motion reported at a row well below the header: an in-progress drag
	// must keep tracking the mouse, the same way dragging a real window
	// border doesn't cancel just because the cursor wanders off some
	// particular row.
	widths, changed := m.Handle(motion(9, 5), cols, nil, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true (drag should not cancel on row change)")
	}
	// Requested +3 clamps to +2 for the same reason as in
	// TestHandleDragWidensColumnAndNarrowsItsRightNeighbor.
	if got, want := widths[0], 7; got != want {
		t.Fatalf("widths[0] = %d, want %d", got, want)
	}
}

func TestHandleReleaseStopsTheDrag(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)
	if !m.Dragging() {
		t.Fatalf("Dragging() = false after press, want true")
	}

	m.Handle(release(9, 0), cols, nil, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true after release, want false")
	}

	// A subsequent motion with no fresh press must not resume resizing.
	widths, changed := m.Handle(motion(20, 0), cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false (no drag in progress)")
	}
	if got, want := widths, []int{5, 5, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

func TestHandleMotionWithoutTheLeftButtonStopsAnInProgressDrag(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 0, 0)

	// A motion event that lost the left button (e.g. focus left the
	// terminal without ever delivering a release) must not leave dragCol
	// stuck on for the next, unrelated drag to inherit.
	lost := tea.MouseMsg{X: 9, Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
	m.Handle(lost, cols, nil, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (button was lost)")
	}
}

func TestHandleIgnoresNonLeftButtonPress(t *testing.T) {
	m := New()
	cols := threeCols()
	right := tea.MouseMsg{X: 6, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
	m.Handle(right, cols, nil, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (right-click should not start a drag)")
	}
}

func TestHandleWheelEventsAreNoOps(t *testing.T) {
	m := New()
	cols := threeCols()
	wheel := tea.MouseMsg{X: 6, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	widths, changed := m.Handle(wheel, cols, nil, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if got, want := widths, []int{5, 5, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

func TestHandleGrabsEveryInternalBorderRegardlessOfWhichColumnUsedToBeFlex(t *testing.T) {
	// Four columns; under the old flex-based design, a caller could
	// designate any one of these as the sink for every other column's
	// drag, and that column's own right-hand border (if it had one)
	// would stop responding to a drag at all. There's no such column any
	// more: every one of the three internal borders here must be
	// grabbable, and every drag must only ever move the two columns
	// straddling the border actually grabbed — see
	// TestHandleDragOnlyEverTouchesTheTwoColumnsStraddlingTheDraggedBorder
	// for that half of the guarantee. Offsets: A(1,5) border 6, B(8,5)
	// border 13, C(15,5) border 20, D(22,5).
	cols := []table.Column{
		{Title: "A", Width: 5},
		{Title: "B", Width: 5},
		{Title: "C", Width: 5},
		{Title: "D", Width: 5},
	}
	for _, borderX := range []int{6, 13, 20} {
		m := New()
		_, changed := m.Handle(press(borderX, 0), cols, nil, 0, 0)
		if changed {
			t.Fatalf("border at x=%d: changed = true, want false (press alone never changes widths)", borderX)
		}
		if !m.Dragging() {
			t.Fatalf("border at x=%d: Dragging() = false, want true (every internal border must be grabbable)", borderX)
		}
	}
}

func TestApplyReturnsACopyWithUpdatedWidths(t *testing.T) {
	cols := threeCols()
	out := Apply(cols, []int{1, 2, 3})
	if got, want := out[0].Width, 1; got != want {
		t.Fatalf("out[0].Width = %d, want %d", got, want)
	}
	if got, want := out[0].Title, "A"; got != want {
		t.Fatalf("out[0].Title = %q, want %q (Apply must not touch Title)", got, want)
	}
	if cols[0].Width != 5 {
		t.Fatalf("Apply mutated the original cols slice")
	}
}

func TestApplyIgnoresExtraOrMissingWidths(t *testing.T) {
	cols := threeCols()
	out := Apply(cols, []int{1})
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (Apply must return one entry per column)", len(out))
	}
	if out[1].Width != 5 || out[2].Width != 10 {
		t.Fatalf("out[1]/out[2] widths changed even though widths had no entry for them: %+v", out)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
