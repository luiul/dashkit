package loam

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withForcedColor forces lipgloss to emit real ANSI (tests otherwise run
// with stdout not a tty, which lipgloss auto-detects and downgrades to no
// color), restoring the original profile afterward so this doesn't leak
// into other tests.
func withForcedColor(t *testing.T) {
	t.Helper()
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
}

func fixedStyle(s lipgloss.Style) func(string) lipgloss.Style {
	return func(string) lipgloss.Style { return s }
}

// newTable builds a table the way real callers (canopy/understory) do:
// Selected overridden to empty, since the row highlight ColorizeRows
// applies is a post-render pass instead (see package doc). Without this,
// bubbles/table's own default Selected style wraps row 0 (its cursor
// defaults to 0 and can never go negative) in its own ANSI, which then
// trips ColorizeRows' "already carries its own color" skip guard for
// tests that aren't specifically about that guard.
func newTable(cols []table.Column, height int) table.Model {
	t := table.New(table.WithColumns(cols), table.WithHeight(height))
	styles := table.DefaultStyles()
	styles.Selected = lipgloss.NewStyle()
	t.SetStyles(styles)
	return t
}

func wordStyle(styles map[string]lipgloss.Style) func(string) lipgloss.Style {
	return func(word string) lipgloss.Style {
		if s, ok := styles[word]; ok {
			return s
		}
		return lipgloss.NewStyle()
	}
}

func TestTagPrependsSentinelOnlyWhenSelected(t *testing.T) {
	if got, want := Tag("12s", true), Sentinel+"12s"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := Tag("12s", false), "12s"; got != want {
		t.Fatalf("got %q, want %q unchanged", got, want)
	}
}

func TestColumnOffsetsAccountForOneSpacePaddingOnBothSidesOfEachCell(t *testing.T) {
	cols := []table.Column{{Title: "A", Width: 3}, {Title: "B", Width: 5}}

	offsets := ColumnOffsets(cols)

	if offsets[0] != (ColOffset{Start: 1, Width: 3}) {
		t.Fatalf("got %+v, want start=1 width=3", offsets[0])
	}
	// 1 (leading pad) + 3 (A) + 2 (A's trailing pad + B's leading pad) = 6
	if offsets[1] != (ColOffset{Start: 6, Width: 5}) {
		t.Fatalf("got %+v, want start=6 width=5", offsets[1])
	}
}

func TestDisplayColumnToByteOffsetAccountsForMultiByteRunes(t *testing.T) {
	line := "a-lon… dirty"
	// "a-lon…" is 6 display columns (5 ASCII + 1 for the ellipsis) but 8
	// bytes (5 + 3-byte ellipsis); column 7 (the space) should map to byte
	// 8, not byte 7.
	if got, want := DisplayColumnToByteOffset(line, 7), 9; got != want {
		t.Fatalf("got byte offset %d, want %d", got, want)
	}
}

func TestRecolorWordAppliesTheLookedUpStyle(t *testing.T) {
	withForcedColor(t)
	styles := map[string]lipgloss.Style{
		"dirty": lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
		"clean": lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
	off := ColOffset{Start: 0, Width: 8}
	got := RecolorWord("dirty   ", off, wordStyle(styles))
	want := styles["dirty"].Render("dirty") + "   "
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRecolorWordLeavesAnEmptyWordAlone(t *testing.T) {
	off := ColOffset{Start: 0, Width: 8}
	line := "        "
	if got := RecolorWord(line, off, wordStyle(nil)); got != line {
		t.Fatalf("got %q, want the blank filler line left unchanged", got)
	}
}

func TestRecolorWordSurvivesAMultiByteRuneInAnEarlierColumn(t *testing.T) {
	// Regression test: bubbles/table truncates an over-long cell with a
	// "…" ellipsis (3 UTF-8 bytes, 1 display column). A naive byte-offset
	// slice for a later column silently misaligns on any row where an
	// earlier column got truncated that way, landing on the wrong bytes.
	withForcedColor(t)
	line := "a-lon… dirty"
	off := ColOffset{Start: 7, Width: 5}
	styles := map[string]lipgloss.Style{"dirty": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}

	got := RecolorWord(line, off, wordStyle(styles))

	want := "a-lon… " + styles["dirty"].Render("dirty")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStyleSequencesSplitsOpenAndCloseAroundTheRenderedContent(t *testing.T) {
	withForcedColor(t)
	style := lipgloss.NewStyle().Reverse(true)
	open, closeSeq := StyleSequences(style)
	if open == "" || closeSeq == "" {
		t.Fatalf("got open=%q close=%q, want both non-empty with color forced on", open, closeSeq)
	}
	if got, want := style.Render("x"), open+"x"+closeSeq; got != want {
		t.Fatalf("got %q, want open+content+close to reconstruct the style's own render %q", got, want)
	}
}

func TestStyleSequencesIsEmptyWithoutColorSupport(t *testing.T) {
	// No withForcedColor: lipgloss should downgrade to NoColor here.
	open, closeSeq := StyleSequences(lipgloss.NewStyle().Reverse(true))
	if open != "" || closeSeq != "" {
		t.Fatalf("got open=%q close=%q, want both empty without color support", open, closeSeq)
	}
}

func TestHighlightRowReappliesItsOpeningSequenceAfterAnInnerReset(t *testing.T) {
	withForcedColor(t)
	style := lipgloss.NewStyle().Background(lipgloss.Color("237"))
	inner := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("dirty")
	line := "before " + inner + " after"

	got := HighlightRow(line, style)

	open, closeSeq := StyleSequences(style)
	want := open + "before " + inner + open + " after" + closeSeq
	if got != want {
		t.Fatalf("got %q, want %q (outer style reapplied right after the inner reset)", got, want)
	}
}

func TestHighlightRowIsANoOpWithoutColorSupport(t *testing.T) {
	style := lipgloss.NewStyle().Reverse(true)
	if got, want := HighlightRow("plain line", style), "plain line"; got != want {
		t.Fatalf("got %q, want %q unchanged", got, want)
	}
}

func TestColorizeRowsAppliesEachWordColumnsStyleToAnUnselectedRow(t *testing.T) {
	withForcedColor(t)
	cols := []table.Column{
		{Title: "Worktree", Width: 8},
		{Title: "Merge", Width: 9},
	}
	worktreeStyles := map[string]lipgloss.Style{"dirty": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}
	mergeStyles := map[string]lipgloss.Style{"unmerged": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}
	tbl := newTable(cols, 3)
	tbl.SetRows([]table.Row{{"clean", "-"}, {"dirty", "unmerged"}})

	got := ColorizeRows(tbl.View(), tbl.Columns(), []WordColumn{
		{Index: 0, Style: wordStyle(worktreeStyles)},
		{Index: 1, Style: wordStyle(mergeStyles)},
	}, lipgloss.NewStyle())

	wantWorktree := worktreeStyles["dirty"].Render("dirty")
	wantMerge := mergeStyles["unmerged"].Render("unmerged")
	if !strings.Contains(got, wantWorktree) {
		t.Fatalf("got %q, want it to contain the styled word %q", got, wantWorktree)
	}
	if !strings.Contains(got, wantMerge) {
		t.Fatalf("got %q, want it to contain the styled word %q", got, wantMerge)
	}
}

func TestColorizeRowsSupportsAFixedStyleColumnRegardlessOfItsWord(t *testing.T) {
	// canopy's Since column (and any similar "always this style, no
	// matter what the text is" column) is just a WordColumn whose Style
	// ignores its argument.
	withForcedColor(t)
	cols := []table.Column{{Title: "Since", Width: 6}}
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	tbl := newTable(cols, 2)
	tbl.SetRows([]table.Row{{"3d"}})

	got := ColorizeRows(tbl.View(), tbl.Columns(), []WordColumn{{Index: 0, Style: fixedStyle(subtle)}}, lipgloss.NewStyle())

	if want := subtle.Render("3d"); !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain the fixed-style word %q", got, want)
	}
}

func TestColorizeRowsHighlightsTheWholeSentinelTaggedRowButNotOthers(t *testing.T) {
	withForcedColor(t)
	cols := []table.Column{
		{Title: "Updated", Width: 8},
		{Title: "Worktree", Width: 8},
		{Title: "Merge", Width: 9},
	}
	worktreeStyles := map[string]lipgloss.Style{"dirty": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}
	highlight := lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "254", Dark: "237"})
	tbl := newTable(cols, 3)
	tbl.SetRows([]table.Row{
		{"3d", "clean", "-"},
		{Tag("12s", true), "dirty", "unmerged"},
	})

	got := ColorizeRows(tbl.View(), tbl.Columns(), []WordColumn{{Index: 1, Style: wordStyle(worktreeStyles)}}, highlight)
	lines := strings.Split(got, "\n")

	open, closeSeq := StyleSequences(highlight)
	if open == "" {
		t.Fatal("StyleSequences returned no escape codes; withForcedColor isn't taking effect")
	}
	if strings.Contains(lines[1], open) {
		t.Fatalf("got the row highlight on the non-tagged row %q, want it left alone", lines[1])
	}
	if !strings.HasPrefix(lines[2], open) || !strings.HasSuffix(lines[2], closeSeq) {
		t.Fatalf("got tagged row %q, want it wrapped start-to-end in the highlight's open/close sequences", lines[2])
	}
	if strings.Contains(got, Sentinel) {
		t.Fatalf("got %q, want Sentinel stripped out of the final output entirely", got)
	}
	// The word-column style must still be visible *inside* the row
	// highlight, not lost just because the row as a whole is now
	// ANSI-wrapped too.
	if want := worktreeStyles["dirty"].Render("dirty"); !strings.Contains(lines[2], want) {
		t.Fatalf("got tagged row %q, want it to still contain the styled word %q", lines[2], want)
	}
}

func TestColorizeRowsRecolorsRightmostColumnFirstSoEarlierOffsetsStayValid(t *testing.T) {
	withForcedColor(t)
	cols := []table.Column{
		{Title: "Worktree", Width: 8},
		{Title: "Merge", Width: 9},
	}
	tbl := newTable(cols, 2)
	tbl.SetRows([]table.Row{{"dirty", "unmerged"}})
	worktreeStyles := map[string]lipgloss.Style{"dirty": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}
	mergeStyles := map[string]lipgloss.Style{"unmerged": lipgloss.NewStyle().Foreground(lipgloss.Color("11"))}

	// Deliberately pass the leftmost column first, to prove ColorizeRows
	// (not the caller) is responsible for ordering the recolor rightmost
	// first internally.
	got := ColorizeRows(tbl.View(), tbl.Columns(), []WordColumn{
		{Index: 0, Style: wordStyle(worktreeStyles)},
		{Index: 1, Style: wordStyle(mergeStyles)},
	}, lipgloss.NewStyle())

	if want := worktreeStyles["dirty"].Render("dirty"); !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to still contain the styled word %q", got, want)
	}
	if want := mergeStyles["unmerged"].Render("unmerged"); !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to still contain the styled word %q", got, want)
	}
}

func TestColorizeRowsSkipsALineThatAlreadyCarriesItsOwnAnsi(t *testing.T) {
	withForcedColor(t)
	// ColorizeRows must leave a line that already contains an escape
	// sequence untouched rather than recolor a sub-span of it (which
	// would inject a reset code that cuts the outer style short for the
	// rest of that line). Simulate that with a table left on
	// bubbles/table's own default Selected style (real callers, e.g.
	// canopy/understory, override it to empty and so never produce one
	// in practice).
	cols := []table.Column{
		{Title: "Kind", Width: 4},
		{Title: "State", Width: 9},
	}
	tbl := table.New(table.WithColumns(cols), table.WithHeight(2))
	tbl.SetRows([]table.Row{{"pi", "done"}})
	tbl.SetCursor(0)
	rendered := tbl.View()

	got := ColorizeRows(rendered, tbl.Columns(), []WordColumn{{Index: 1, Style: fixedStyle(lipgloss.NewStyle())}}, lipgloss.NewStyle())

	if got != rendered {
		t.Fatalf("got a modified pre-styled line:\n%q\nwant it unchanged from:\n%q", got, rendered)
	}
}

func TestColorizeRowsLeavesTheHeaderLineUntouched(t *testing.T) {
	withForcedColor(t)
	cols := []table.Column{
		{Title: "Worktree", Width: 8},
		{Title: "Merge", Width: 9},
	}
	tbl := newTable(cols, 3)
	tbl.SetRows([]table.Row{{"dirty", "unmerged"}})

	rendered := tbl.View()
	wantHeaderLine := strings.Split(rendered, "\n")[0] // bold by default, even before ColorizeRows

	got := ColorizeRows(rendered, tbl.Columns(), []WordColumn{{Index: 0, Style: fixedStyle(lipgloss.NewStyle())}}, lipgloss.NewStyle())
	gotHeaderLine := strings.Split(got, "\n")[0]

	if gotHeaderLine != wantHeaderLine {
		t.Fatalf("got header line %q, want it byte-identical to the table's own %q", gotHeaderLine, wantHeaderLine)
	}
}

func TestColorizeRowsSupportsABlinkStyleSuffixMarkerViaAClosure(t *testing.T) {
	// canopy's State column strips a trailing "*" blink marker before
	// looking up the word's style, then reverses that style when the
	// marker was present — entirely expressible as a WordColumn.Style
	// closure, with no special-casing needed in ColorizeRows itself.
	withForcedColor(t)
	const blinkMarker = "*"
	stateStyles := map[string]lipgloss.Style{"done": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))}
	blinkAware := func(trimmed string) lipgloss.Style {
		word := strings.TrimSuffix(trimmed, blinkMarker)
		style := wordStyle(stateStyles)(word)
		if word != trimmed {
			style = style.Reverse(true)
		}
		return style
	}
	cols := []table.Column{{Title: "State", Width: 9}}
	tbl := newTable(cols, 2)
	tbl.SetRows([]table.Row{{"done" + blinkMarker}})

	got := ColorizeRows(tbl.View(), tbl.Columns(), []WordColumn{{Index: 0, Style: blinkAware}}, lipgloss.NewStyle())

	want := stateStyles["done"].Reverse(true).Render("done" + blinkMarker)
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want it to contain the reverse-video blink %q", got, want)
	}
}
