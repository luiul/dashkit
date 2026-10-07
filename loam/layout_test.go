package loam_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/luiul/dashkit/loam"
	"github.com/luiul/dashkit/trellis"
	"github.com/muesli/termenv"
)

func TestAllocatedTableBordersAndHighlightReachRightEdge(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
	policies := []trellis.ColumnPolicy{
		{Minimum: 6, HardMinimum: 6, Preferred: 6},
		{Minimum: 20, HardMinimum: 8, Preferred: 40, Weight: 1},
		{Minimum: 20, HardMinimum: 8, Preferred: 50, Weight: 2},
	}
	for _, viewport := range []int{28, 29, 100, 110, 120, 140, 180, 240} {
		widths, fits := trellis.Allocate(viewport, policies)
		if !fits {
			t.Fatalf("viewport %d should fit", viewport)
		}
		cols := trellis.Apply([]table.Column{{Title: "State"}, {Title: "模型"}, {Title: "Path"}}, widths)
		model := table.New(table.WithColumns(cols), table.WithHeight(2))
		styles := table.DefaultStyles()
		styles.Selected = lipgloss.NewStyle()
		model.SetStyles(styles)
		model.SetRows([]table.Row{{loam.Tag("idle", true), strings.Repeat("模型", 40), "~/long/path"}})
		highlight := lipgloss.NewStyle().Background(lipgloss.Color("236"))
		view := loam.ColorizeRows(model.View(), cols, []loam.WordColumn{{Index: 0, Style: func(string) lipgloss.Style {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		}}}, highlight)
		view = loam.DrawHeaderBorders(view, cols, lipgloss.NewStyle().Foreground(lipgloss.Color("240")))
		lines := strings.Split(view, "\n")
		for i := 0; i < 2; i++ {
			if got := ansi.StringWidth(lines[i]); got != viewport {
				t.Fatalf("viewport %d line %d width %d", viewport, i, got)
			}
		}
		for _, off := range loam.ColumnOffsets(cols)[:len(cols)-1] {
			border := off.Start + off.Width
			if got := ansi.Strip(ansi.Cut(lines[0], border, border+1)); got != loam.BorderGlyph {
				t.Fatalf("viewport %d border %d = %q", viewport, border, got)
			}
		}
		open, closeSeq := loam.StyleSequences(highlight)
		if !strings.HasSuffix(lines[1], " "+closeSeq) || !strings.Contains(lines[1], closeSeq+open) {
			t.Fatalf("highlight lost right padding or nested color at viewport %d: %q", viewport, lines[1])
		}
		if strings.Contains(view, loam.Sentinel) {
			t.Fatal("cursor marker leaked")
		}
	}
}
