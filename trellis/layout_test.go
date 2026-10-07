package trellis

import (
	"reflect"
	"testing"

	"github.com/luiul/dashkit/loam"
)

func layoutPolicies() []ColumnPolicy {
	return []ColumnPolicy{
		{Minimum: 6, HardMinimum: 6, Preferred: 6},
		{Minimum: 16, HardMinimum: 8, Preferred: 24, Weight: 1, ShrinkPriority: 1},
		{Minimum: 20, HardMinimum: 8, Preferred: 35, Weight: 2, ShrinkPriority: 1},
		{Minimum: 20, HardMinimum: 8, Preferred: 45, Weight: 2, ShrinkPriority: 2},
	}
}

func assertLayout(t *testing.T, viewport int, policies []ColumnPolicy, widths []int, fits bool) {
	t.Helper()
	hard := loam.CellPadding * len(policies)
	for i, p := range policies {
		hard += max(p.HardMinimum, 1)
		if widths[i] < max(p.HardMinimum, 1) {
			t.Fatalf("column %d width %d below floor %d", i, widths[i], p.HardMinimum)
		}
	}
	if fits != (viewport >= hard) {
		t.Fatalf("viewport %d: fits = %v, hard minimum %d", viewport, fits, hard)
	}
	if fits && sum(widths)+loam.CellPadding*len(widths) != viewport {
		t.Fatalf("viewport %d: widths %v do not fill it", viewport, widths)
	}
}

func TestAllocateExactWidthAndBoundaries(t *testing.T) {
	p := layoutPolicies()
	for viewport := 1; viewport <= 300; viewport++ {
		widths, fits := Allocate(viewport, p)
		assertLayout(t, viewport, p, widths, fits)
		again, againFits := Allocate(viewport, p)
		if !reflect.DeepEqual(widths, again) || fits != againFits {
			t.Fatal("allocation is not deterministic")
		}
		if widths[0] != 6 {
			t.Fatalf("compact column grew: %v", widths)
		}
	}
}

func TestAllocateFundsContentBeforeWeightedSurplus(t *testing.T) {
	p := layoutPolicies()
	base := 6 + 24 + 35 + 45 + 4*loam.CellPadding
	widths, _ := Allocate(base, p)
	if !reflect.DeepEqual(widths, []int{6, 24, 35, 45}) {
		t.Fatalf("useful content did not fit: %v", widths)
	}
	widths, _ = Allocate(base+11, p)
	if !reflect.DeepEqual(widths, []int{6, 26, 40, 49}) {
		t.Fatalf("surplus 1:2:2 with largest remainders = %v", widths)
	}
}

func TestAllocatePrioritizesModelOverPath(t *testing.T) {
	p := []ColumnPolicy{
		{Minimum: 28, HardMinimum: 5, Preferred: 40, Weight: 1},
		{Minimum: 20, HardMinimum: 8, Preferred: 100, Weight: 1, ShrinkPriority: 1},
	}
	widths, _ := Allocate(64, p)
	if !reflect.DeepEqual(widths, []int{40, 20}) {
		t.Fatalf("path starved useful model: %v", widths)
	}
	widths, _ = Allocate(34, p)
	if !reflect.DeepEqual(widths, []int{22, 8}) {
		t.Fatalf("path must yield normal target first: %v", widths)
	}
}

func TestAllocateCanReclaimLowerPriorityNormalTargetForUsefulContent(t *testing.T) {
	p := []ColumnPolicy{
		{Minimum: 28, HardMinimum: 5, Preferred: 43, Weight: 1},
		{Minimum: 20, HardMinimum: 8, Preferred: 200, Weight: 1, ShrinkPriority: 1},
	}
	for _, viewport := range []int{55, 56} {
		widths, fits := Allocate(viewport, p)
		assertLayout(t, viewport, p, widths, fits)
		if widths[0] < 43 {
			t.Fatalf("viewport %d: path target starved model: %v", viewport, widths)
		}
	}
}

func TestAllocateShrinksTextBeforeCompact(t *testing.T) {
	p := []ColumnPolicy{
		{Minimum: 10, HardMinimum: 6, Preferred: 10, ShrinkPriority: 99},
		{Minimum: 20, HardMinimum: 8, Weight: 1},
	}
	widths, _ := Allocate(24, p)
	if !reflect.DeepEqual(widths, []int{10, 10}) {
		t.Fatalf("compact field shrank before text: %v", widths)
	}
}

func TestAllocateNormalizesInvalidPolicies(t *testing.T) {
	p := []ColumnPolicy{{}, {Minimum: -1, HardMinimum: -9, Preferred: -2, Weight: -1}}
	for viewport := -1; viewport < 20; viewport++ {
		widths, fits := Allocate(viewport, p)
		assertLayout(t, viewport, p, widths, fits)
	}
}

func TestContentWidthUsesPlainDisplayCells(t *testing.T) {
	for _, tc := range []struct {
		label string
		want  int
	}{
		{"", 0}, {"模型", 4}, {"e\u0301", 1},
		{"\x1b[31m模型\x1b[0m" + loam.Sentinel, 4},
		{"branch @ other-dir/", 19},
	} {
		if got := ContentWidth(tc.label); got != tc.want {
			t.Fatalf("ContentWidth(%q) = %d, want %d", tc.label, got, tc.want)
		}
	}
}

func TestPreferencesPreserveDesiredWeightsWithoutDrift(t *testing.T) {
	policies := layoutPolicies()
	var p Preferences
	if p.Manual() {
		t.Fatal("zero preferences must be automatic")
	}
	widths, _ := Allocate(180, policies)
	widths[1] += 13
	widths[2] -= 13
	p.Capture(widths, policies, 1)
	want := append([]int(nil), widths...)
	if !p.Manual() {
		t.Fatal("capture did not enter manual mode")
	}
	for range 20 {
		for viewport := 30; viewport <= 240; viewport++ {
			got, fits := p.Allocate(viewport, policies)
			assertLayout(t, viewport, policies, got, fits)
		}
		got, _ := p.Allocate(180, policies)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("wide/narrow/wide drift: got %v want %v", got, want)
		}
	}
	policies[1].Preferred = 500
	policies[2].Preferred = 500
	policies[3].Preferred = 500
	got, _ := p.Allocate(180, policies)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("poll content overrode manual widths: %v want %v", got, want)
	}
}

func TestPreferencesKeepCompactTargetsAndWidenedPool(t *testing.T) {
	policies := layoutPolicies()
	policies[0].Minimum, policies[0].Preferred = 10, 10
	var p Preferences
	widths, _ := Allocate(180, policies)
	widths[0] -= 2
	widths[1] += 2
	p.Capture(widths, policies, 0)
	for _, viewport := range []int{100, 110, 120, 140, 180, 240} {
		got, fits := p.Allocate(viewport, policies)
		assertLayout(t, viewport, policies, got, fits)
		if got[0] != 8 {
			t.Fatalf("narrowed compact target lost: %v", got)
		}
	}
	widths[0] += 10
	widths[1] -= 10
	p.Capture(widths, policies, 0)
	got, _ := p.Allocate(240, policies)
	if got[0] <= widths[0] {
		t.Fatalf("manually widened compact did not join pool: %v", got)
	}
}

func TestPreferencesRepeatedlyClampAndRedistribute(t *testing.T) {
	policies := []ColumnPolicy{
		{HardMinimum: 8, Weight: 1},
		{HardMinimum: 8, Weight: 1},
		{HardMinimum: 8, Weight: 1},
	}
	var p Preferences
	p.Capture([]int{8, 9, 200}, policies, 0)
	got, fits := p.Allocate(36, policies)
	assertLayout(t, 36, policies, got, fits)
	if !reflect.DeepEqual(got, []int{8, 8, 14}) {
		t.Fatalf("floor redistribution = %v", got)
	}
}

func TestPreferencesRedistributionCanIntroduceAnotherFloorViolation(t *testing.T) {
	policies := []ColumnPolicy{
		{HardMinimum: 8, Weight: 1},
		{HardMinimum: 8, Weight: 1},
		{HardMinimum: 8, Weight: 1},
	}
	var prefs Preferences
	prefs.Capture([]int{8, 80, 800}, policies, 0)
	got, fits := prefs.Allocate(96, policies)
	assertLayout(t, 96, policies, got, fits)
	if !reflect.DeepEqual(got, []int{8, 8, 74}) {
		t.Fatalf("second clamping pass = %v", got)
	}
}

func TestUnrelatedDragKeepsNarrowedCompactDesiredTarget(t *testing.T) {
	policies := layoutPolicies()
	policies[0].Minimum, policies[0].Preferred = 10, 10
	var prefs Preferences
	widths, _ := Allocate(180, policies)
	widths[0] -= 2
	widths[1] += 2
	prefs.Capture(widths, policies, 0)
	narrow, _ := prefs.Allocate(39, policies)
	if narrow[0] != 7 {
		t.Fatalf("fixture must temporarily clamp compact target: %v", narrow)
	}
	narrow[1]++
	narrow[2]--
	prefs.Capture(narrow, policies, 1)
	wide, _ := prefs.Allocate(180, policies)
	if wide[0] != 8 {
		t.Fatalf("unrelated drag erased compact target: %v", wide)
	}
}

func TestUnrelatedNarrowWindowDragKeepsWidenedCompactInPool(t *testing.T) {
	policies := layoutPolicies()
	policies[0].Minimum, policies[0].Preferred = 10, 10
	var prefs Preferences
	widths, _ := Allocate(180, policies)
	widths[0] += 8
	widths[1] -= 8
	prefs.Capture(widths, policies, 0)
	narrow, _ := prefs.Allocate(80, policies)
	if narrow[0] >= 10 {
		t.Fatalf("fixture must narrow widened compact below target: %v", narrow)
	}
	narrow[1]++
	narrow[2]--
	prefs.Capture(narrow, policies, 1)
	wide, _ := prefs.Allocate(240, policies)
	if wide[0] <= narrow[0] {
		t.Fatalf("unrelated narrow drag removed compact pool membership: %v", wide)
	}
}

func TestUnrelatedDragKeepsPreviouslyWidenedCompactInPool(t *testing.T) {
	policies := layoutPolicies()
	var prefs Preferences
	widths, _ := Allocate(180, policies)
	widths[0] += 4
	widths[1] -= 4
	prefs.Capture(widths, policies, 0)
	policies[0].Preferred = 20 // New content exceeds the chosen width.
	widths, _ = prefs.Allocate(180, policies)
	widths[1]++
	widths[2]--
	prefs.Capture(widths, policies, 1)
	wider, _ := prefs.Allocate(240, policies)
	if wider[0] <= widths[0] {
		t.Fatalf("new label removed compact column from pool: %v", wider)
	}
}

func TestPreferencesCompactOnlyTableStillFillsViewport(t *testing.T) {
	policies := []ColumnPolicy{
		{Minimum: 8, HardMinimum: 4},
		{Minimum: 8, HardMinimum: 4},
	}
	var prefs Preferences
	prefs.Capture([]int{6, 8}, policies, 0)
	got, fits := prefs.Allocate(24, policies)
	assertLayout(t, 24, policies, got, fits)
	if !reflect.DeepEqual(got, []int{6, 14}) {
		t.Fatalf("compact-only surplus = %v", got)
	}
	got, fits = prefs.Allocate(12, policies)
	assertLayout(t, 12, policies, got, fits)
	if !reflect.DeepEqual(got, []int{4, 4}) {
		t.Fatalf("compact targets must yield at hard boundary: %v", got)
	}
}

func TestPreferencesUntouchedCompactFieldsStayContentAware(t *testing.T) {
	policies := layoutPolicies()
	var prefs Preferences
	widths, _ := Allocate(180, policies)
	prefs.Capture(widths, policies, 1)
	policies[0].Preferred = 9
	got, fits := prefs.Allocate(180, policies)
	assertLayout(t, 180, policies, got, fits)
	if got[0] != 9 {
		t.Fatalf("untouched compact content not fitted: %v", got)
	}
}

func TestPreferencesCaptureCopiesCallerWidths(t *testing.T) {
	policies := layoutPolicies()
	widths, _ := Allocate(180, policies)
	var prefs Preferences
	prefs.Capture(widths, policies, 1)
	want := append([]int(nil), widths...)
	widths[1] = 999
	got, _ := prefs.Allocate(180, policies)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("caller mutation changed desired proportions: %v", got)
	}
}

func TestPreferencesIgnoreInvalidCapture(t *testing.T) {
	var p Preferences
	p.Capture([]int{5}, layoutPolicies(), 0)
	p.Capture([]int{6, 24, 35, 45}, layoutPolicies(), 3)
	if p.Manual() {
		t.Fatal("invalid capture must not enter manual mode")
	}
}

func FuzzManualFloorProjection(f *testing.F) {
	f.Add(180, 6, 8, 8, 3, 100)
	f.Add(60, 1, 30, 2, 19, 9)
	f.Fuzz(func(t *testing.T, viewport, a, b, c, weight, label int) {
		viewport = max(0, viewport%500)
		a, b, c = max(1, a%50), max(1, b%50), max(1, c%50)
		policies := []ColumnPolicy{
			{Minimum: a + 5, HardMinimum: a, Preferred: max(1, label%500)},
			{Minimum: b + 10, HardMinimum: b, Weight: max(1, weight%10)},
			{Minimum: c + 15, HardMinimum: c, Weight: 2, ShrinkPriority: 1},
		}
		widths, _ := Allocate(500, policies)
		// Put a compact field into the pool by moving a real adjacent border.
		delta := min(4, widths[1]-b)
		widths[0] += delta
		widths[1] -= delta
		var prefs Preferences
		prefs.Capture(widths, policies, 0)
		got, fits := prefs.Allocate(viewport, policies)
		assertLayout(t, viewport, policies, got, fits)
		want, _ := prefs.Allocate(500, policies)
		if !reflect.DeepEqual(want, widths) {
			t.Fatalf("manual projection failed same-width identity: %v want %v", want, widths)
		}
	})
}

func FuzzLayoutAccounting(f *testing.F) {
	f.Add(180, 24, 35, 45)
	f.Add(38, 999, 8, 1)
	f.Fuzz(func(t *testing.T, viewport, a, b, c int) {
		viewport = max(0, viewport%1000)
		p := layoutPolicies()
		p[1].Preferred, p[2].Preferred, p[3].Preferred = a%1000, b%1000, c%1000
		widths, fits := Allocate(viewport, p)
		assertLayout(t, viewport, p, widths, fits)
		if fits {
			var prefs Preferences
			prefs.Capture(widths, p, 1)
			for _, width := range []int{38, 39, 100, 240} {
				got, ok := prefs.Allocate(width, p)
				assertLayout(t, width, p, got, ok)
			}
		}
	})
}
