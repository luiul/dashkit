package trellis

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// threeCols mirrors a small slice of a real dashboard's columns: A/B
// fixed-ish, C the flex column that absorbs whatever A/B's drags change.
// Offsets (see loam.ColumnOffsets): A starts at 1 width 5 (border at 6),
// B starts at 8 width 5 (border at 13), C starts at 15 width 10.
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
	widths, changed := m.Handle(press(6, 5), cols, nil, 2, 0, 0)
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
	_, changed := m.Handle(press(2, 0), cols, nil, 2, 0, 0)
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
	_, changed := m.Handle(press(6, 0), cols, nil, 2, 0, 0)
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
	_, changed := m.Handle(press(7, 0), cols, nil, 2, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if got := m.DragColumn(); got != 0 {
		t.Fatalf("DragColumn() = %d, want 0 (off-by-one click should still grab it)", got)
	}
}

func TestHandleDragWidensColumnAndShrinksFlexByTheSameAmount(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)

	widths, changed := m.Handle(motion(9, 0), cols, nil, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{8, 5, 7}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (dragCol +3, flex -3)", got, want)
	}
}

func TestHandleDragNarrowsColumnAndGrowsFlex(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)

	widths, changed := m.Handle(motion(4, 0), cols, nil, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{3, 5, 12}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (dragCol -2, flex +2)", got, want)
	}
}

func TestHandleDragClampsAtDragColumnsMinimum(t *testing.T) {
	m := New()
	cols := threeCols()
	mins := []int{4, 0, 0} // column 0 can't go below 4
	m.Handle(press(6, 0), cols, mins, 2, 0, 0)

	// Ask to shrink column 0 by 5 (well past its floor of 4, i.e. width 0).
	widths, changed := m.Handle(motion(1, 0), cols, mins, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths[0], 4; got != want {
		t.Fatalf("widths[0] = %d, want %d (clamped at its own minimum)", got, want)
	}
	if got, want := widths[2], 11; got != want {
		t.Fatalf("widths[2] = %d, want %d (flex only grew by what column 0 actually gave up)", got, want)
	}
}

func TestHandleDragClampsAtFlexColumnsMinimum(t *testing.T) {
	m := New()
	cols := threeCols()
	mins := []int{0, 0, 9} // flex can't go below 9 (only 1 to give)
	m.Handle(press(6, 0), cols, mins, 2, 0, 0)

	widths, changed := m.Handle(motion(20, 0), cols, mins, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths[2], 9; got != want {
		t.Fatalf("widths[2] = %d, want %d (clamped at flex's own minimum)", got, want)
	}
	if got, want := widths[0], 6; got != want {
		t.Fatalf("widths[0] = %d, want %d (dragCol only grew by what flex actually gave up)", got, want)
	}
}

func TestHandleDragAccumulatesAcrossMultipleMotionEvents(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)

	// Real usage applies each motion's returned widths back onto cols (via
	// Apply) before the next event, the same as SetColumns keeping the live
	// table in sync — so this mirrors that instead of calling Handle twice
	// against the same stale cols.
	widths, _ := m.Handle(motion(8, 0), cols, nil, 2, 0, 0)
	cols = Apply(cols, widths)

	widths, changed := m.Handle(motion(9, 0), cols, nil, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true")
	}
	if got, want := widths, []int{8, 5, 7}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v (two +2/+1 motions should sum to +3 total)", got, want)
	}
}

func TestHandleDragIgnoresRowLeavingTheHeaderOnceStarted(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)

	// Motion reported at a row well below the header: an in-progress drag
	// must keep tracking the mouse, the same way dragging a real window
	// border doesn't cancel just because the cursor wanders off some
	// particular row.
	widths, changed := m.Handle(motion(9, 5), cols, nil, 2, 0, 0)
	if !changed {
		t.Fatalf("changed = false, want true (drag should not cancel on row change)")
	}
	if got, want := widths[0], 8; got != want {
		t.Fatalf("widths[0] = %d, want %d", got, want)
	}
}

func TestHandleReleaseStopsTheDrag(t *testing.T) {
	m := New()
	cols := threeCols()
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)
	if !m.Dragging() {
		t.Fatalf("Dragging() = false after press, want true")
	}

	m.Handle(release(9, 0), cols, nil, 2, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true after release, want false")
	}

	// A subsequent motion with no fresh press must not resume resizing.
	widths, changed := m.Handle(motion(20, 0), cols, nil, 2, 0, 0)
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
	m.Handle(press(6, 0), cols, nil, 2, 0, 0)

	// A motion event that lost the left button (e.g. focus left the
	// terminal without ever delivering a release) must not leave dragCol
	// stuck on for the next, unrelated drag to inherit.
	lost := tea.MouseMsg{X: 9, Y: 0, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
	m.Handle(lost, cols, nil, 2, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (button was lost)")
	}
}

func TestHandleIgnoresNonLeftButtonPress(t *testing.T) {
	m := New()
	cols := threeCols()
	right := tea.MouseMsg{X: 6, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
	m.Handle(right, cols, nil, 2, 0, 0)
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (right-click should not start a drag)")
	}
}

func TestHandleWheelEventsAreNoOps(t *testing.T) {
	m := New()
	cols := threeCols()
	wheel := tea.MouseMsg{X: 6, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	widths, changed := m.Handle(wheel, cols, nil, 2, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if got, want := widths, []int{5, 5, 10}; !equal(got, want) {
		t.Fatalf("widths = %v, want %v", got, want)
	}
}

func TestHandleNeverGrabsTheFlexColumnsOwnBorder(t *testing.T) {
	m := New()
	// Four columns; flex is column 2 (not last), so it does have its own
	// right-hand border at some X. Offsets: A(1,5) border 6, B(8,5)
	// border 13, C(15,5) border 20, D(22,5).
	cols := []table.Column{
		{Title: "A", Width: 5},
		{Title: "B", Width: 5},
		{Title: "C", Width: 5},
		{Title: "D", Width: 5},
	}
	_, changed := m.Handle(press(20, 0), cols, nil, 2, 0, 0)
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if m.Dragging() {
		t.Fatalf("Dragging() = true, want false (flex's own border must not be grabbable)")
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
