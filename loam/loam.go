// Package loam is the shared rendering substrate canopy and understory
// both grow their bubbles/table dashboards from: per-word column
// coloring and whole-row selection highlighting, applied by
// post-processing an already-rendered table view rather than putting
// ANSI-styled strings into table.Row values directly.
//
// That indirection exists because bubbles/table v1's cell truncation
// (runewidth.Truncate) is not ANSI-aware: escape codes get counted as
// extra visible width and sliced mid-sequence, corrupting the row
// (verified empirically against bubbles/table v1.0.0 — a styled
// "unmerged" in a 9-wide column gets truncated with a dangling escape
// code). Post-processing the table's already-rendered plain-text view
// instead sidesteps that entirely: the widths/padding/truncation the
// table computes are always over plain text, and only the final display
// string gets colored.
//
// This started as two independently-written, near-identical
// colorize.go files (one in each tree, one literally commenting that it
// was "the same technique... as [the other]'s own package") before being
// pulled out here, the same way mycelium already holds the
// open-or-focus-a-window logic both dashboards' Enter key needs.
package loam

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Sentinel tags whichever row a caller wants ColorizeRows to highlight,
// without needing a dedicated leading marker column/glyph that takes up
// space in every row. Prepend it (see Tag) to any one cell's text on the
// row you want highlighted — Since/Updated-style columns are a good
// choice: always populated, never blanked for grouping, and never long
// enough to risk truncation.
//
// It's a zero-width Unicode space (U+200B), not a visible character:
// zero width means it never changes any column's padding/truncation
// math (bubbles/table and go-runewidth both measure display width, not
// byte length, so this is invisible to both — verified empirically
// against runewidth.Truncate and lipgloss.Style.Width), and it travels
// with the row's own data through bubbles/table's internal scrolling
// exactly like any other cell value would. That matters because
// ColorizeRows operates on the table's already-rendered, already-
// scrolled text: bubbles/table v1 doesn't expose its internal scroll
// offset, so there'd be no other way to know which rendered line
// corresponds to a given data row's cursor index once the view has
// scrolled. ColorizeRows strips Sentinel back out of the final output
// before returning it, so it never leaks into, say, a copy-pasted
// terminal selection.
const Sentinel = "\u200b"

// Tag prepends Sentinel to text when selected is true, leaving text
// unchanged otherwise. Prepended rather than appended: bubbles/table
// truncates a too-long cell from the tail (runewidth.Truncate keeps the
// head plus an ellipsis), so a leading zero-width tag survives
// regardless of how long the cell's real content is, where a trailing
// one could get truncated away along with the tail.
func Tag(text string, selected bool) string {
	if selected {
		return Sentinel + text
	}
	return text
}

// WordColumn recolors one column of an already-rendered table view: the
// cell at Index gets Style(word) applied to it, where word is that
// cell's own trimmed text. An empty word (a blank filler row below the
// real data, or a placeholder row) is left alone.
//
// Style controls the entire per-word lookup, so it covers both callers'
// needs: a map-backed lookup that varies by word (e.g. "dirty" vs
// "clean"), or a closure that ignores word and always returns the same
// fixed style (e.g. a Since/Updated column that's always styled the
// same regardless of its own content) — and anything in between, like a
// closure that strips its own suffix marker before deciding the style
// (e.g. a blinking "done*" indicator), same as any other function value.
type WordColumn struct {
	Index int
	Style func(word string) lipgloss.Style
}

// ColorizeRows recolors each of wordCols on an already-rendered
// bubbles/table view, then highlights the whole line of whichever row
// carries Sentinel (see Tag) in highlight, and finally strips Sentinel
// out of the result. cols must be the exact columns the view was
// rendered with.
//
// The header line and any line that already contains an escape sequence
// coming in (from some outer style applied before ColorizeRows ever
// ran — e.g. bubbles/table's own default Selected style, if a caller
// hasn't overridden it to empty) is left untouched entirely: recoloring
// a sub-span of a line that already carries its own color would inject
// a reset code that cuts the outer style short for the rest of that
// line. That guard does not apply between the steps below for the
// *same* line, though: each WordColumn only inserts bytes into its own
// disjoint span (processed rightmost first, since inserting bytes into
// a column would otherwise shift the start offset of any column to its
// right), and the row highlight applied after them is specifically
// built (see HighlightRow) to survive wrapping a line that already
// contains their escape codes.
func ColorizeRows(view string, cols []table.Column, wordCols []WordColumn, highlight lipgloss.Style) string {
	offsets := ColumnOffsets(cols)
	ordered := rightmostFirst(wordCols)
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if i == 0 || strings.Contains(line, "\x1b") {
			continue
		}
		isSelected := strings.Contains(line, Sentinel)
		for _, wc := range ordered {
			if wc.Index < 0 || wc.Index >= len(cols) {
				continue
			}
			line = RecolorWord(line, offsets[wc.Index], wc.Style)
		}
		if isSelected {
			line = HighlightRow(line, highlight)
		}
		lines[i] = strings.ReplaceAll(line, Sentinel, "")
	}
	return strings.Join(lines, "\n")
}

// rightmostFirst returns wordCols sorted by Index descending, without
// mutating the caller's slice.
func rightmostFirst(wordCols []WordColumn) []WordColumn {
	ordered := make([]WordColumn, len(wordCols))
	copy(ordered, wordCols)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Index > ordered[j].Index })
	return ordered
}

// BorderGlyph is the vertical divider DrawHeaderBorders renders at each
// internal column border. A thin box-drawing line (U+2502) rather than a
// plain "|" (U+007C): most terminal fonts draw "|" with visible gaps
// above and below at typical line heights, where "│" — the same glyph
// most terminals already use to draw pane/box borders — reads as one
// continuous line instead.
const BorderGlyph = "│"

// DrawHeaderBorders marks each internal column border — there are
// len(cols)-1 of them — in the header line (line 0) of an already-
// rendered bubbles/table view with BorderGlyph, rendered in style. cols
// must be the exact columns the view was rendered with, in the same
// order. Lines other than the header are returned unchanged.
//
// Header only, for the same reason ColorizeRows leaves line 0 alone
// entirely (see its own doc): bubbles/table's default Header style is
// Bold(true) (table.DefaultStyles()), so the header line normally
// already carries ANSI of its own — per cell, wrapping that cell's own
// 1-space padding along with its title — even when a caller never colors
// anything else. That's exactly why this uses github.com/charmbracelet/
// x/ansi's Cut rather than a naive byte-offset walk (an earlier version
// of this function did exactly that, assuming the header was always
// plain text, and silently spliced the border glyph into the middle of
// the Bold escape sequence between two header cells the moment color
// was enabled — corrupting the row instead of marking it, e.g. a
// literal "[1m" appearing as text between two header titles). ansi.Cut
// treats escape sequences as zero-width when measuring display columns,
// so it finds the right character to replace regardless of how many
// styled spans surround it. Every data row can *also* carry ANSI by the
// time View() gets here (RecolorWord's per-word coloring, HighlightRow's
// selected-row background) — marking those too would work the same way,
// but the header is the only row a drag can ever start from (see
// trellis.Model.Handle's own doc), so this stays limited to the one row
// that actually matters for grabbing a border.
func DrawHeaderBorders(view string, cols []table.Column, style lipgloss.Style) string {
	if len(cols) < 2 {
		return view
	}
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		return view
	}
	offsets := ColumnOffsets(cols)
	header := lines[0]
	width := ansi.StringWidth(header)
	glyph := style.Render(BorderGlyph)
	// Unlike RecolorWord's own rightmost-first ordering (needed because
	// its byte-offset walk gets confused by earlier insertions), order
	// doesn't matter here: ansi.Cut re-measures display columns from
	// scratch each time and already skips escape sequences, so an earlier
	// insertion's own ANSI never shifts where a later border lands.
	for c := 0; c < len(cols)-1; c++ {
		border := offsets[c].Start + offsets[c].Width
		if border < 0 || border >= width {
			continue
		}
		header = ansi.Cut(header, 0, border) + glyph + ansi.Cut(header, border+1, width)
	}
	lines[0] = header
	return strings.Join(lines, "\n")
}

// ColOffset is a column's start position and width within a rendered
// row line, accounting for bubbles/table's fixed 1-space padding on
// both sides of every cell (table.DefaultStyles()'s Cell/Header
// Padding(0, 1)).
type ColOffset struct {
	Start, Width int
}

// ColumnOffsets computes each column's start/width within a rendered
// line, given cols in the same order the table was built with. Only
// correct for bubbles/table's default (no border) layout.
func ColumnOffsets(cols []table.Column) []ColOffset {
	offsets := make([]ColOffset, len(cols))
	pos := 1 // leading pad of the first cell
	for i, c := range cols {
		offsets[i] = ColOffset{Start: pos, Width: c.Width}
		pos += c.Width + 2 // this cell's trailing pad + the next cell's leading pad
	}
	return offsets
}

// RecolorWord wraps the display-column span of line at off in whichever
// style lookup(word) returns for that span's trimmed text, preserving
// line's total length. An empty word (blank filler row below the real
// data, or the placeholder row) is left alone.
//
// off's start/width are display columns, not byte offsets: naively
// slicing line as line[off.Start:off.Start+off.Width] silently corrupts
// this the moment any earlier column contains a multi-byte rune whose
// byte length doesn't match its display width — the truncation ellipsis
// "…" bubbles/table's own runewidth.Truncate appends to an over-long
// cell, or a genuinely unicode name. DisplayColumnToByteOffset walks the
// line rune-by-rune to find the real byte offsets first.
func RecolorWord(line string, off ColOffset, lookup func(string) lipgloss.Style) string {
	start := DisplayColumnToByteOffset(line, off.Start)
	end := DisplayColumnToByteOffset(line, off.Start+off.Width)
	if start >= len(line) || end > len(line) || start > end {
		return line
	}
	slice := line[start:end]
	word := strings.TrimRight(slice, " ")
	if word == "" {
		return line
	}
	pad := strings.Repeat(" ", len(slice)-len(word))
	return line[:start] + lookup(word).Render(word) + pad + line[end:]
}

// DisplayColumnToByteOffset returns the byte index in line at which
// display column col begins. Returns len(line) once col reaches or
// passes the line's own display width.
func DisplayColumnToByteOffset(line string, col int) int {
	width := 0
	for i, r := range line {
		if width >= col {
			return i
		}
		width += runewidth.RuneWidth(r)
	}
	return len(line)
}

// HighlightRow wraps the whole of line in style, even when line already
// contains other ANSI escape codes (e.g. from RecolorWord, applied
// first by ColorizeRows). A naive open+line+close wrap would break the
// moment line contains its own reset code: every lipgloss render ends
// with a full SGR reset ("\x1b[0m", verified against lipgloss/termenv
// directly — true regardless of which attributes were opened), so an
// inner reset would end style's effect for the remainder of line, well
// before the intended closing tag. Reapplying style's own opening
// sequence immediately after every such inner reset keeps it in effect
// right up to the final, real close.
func HighlightRow(line string, style lipgloss.Style) string {
	open, closeSeq := StyleSequences(style)
	if open == "" {
		return line // no color support (e.g. NoColor profile): nothing to wrap
	}
	body := line
	if closeSeq != "" {
		body = strings.ReplaceAll(line, closeSeq, closeSeq+open)
	}
	return open + body + closeSeq
}

// StyleSequences extracts style's opening and closing escape sequences
// by rendering it around a NUL byte and splitting on that byte, rather
// than assuming any particular SGR codes: NUL can't otherwise appear in
// a rendered table view, and this stays correct regardless of which
// attributes style combines or whether color is even enabled (in which
// case lipgloss renders content unchanged and both return values are
// "").
func StyleSequences(style lipgloss.Style) (open, closeSeq string) {
	rendered := style.Render("\x00")
	idx := strings.IndexByte(rendered, 0)
	if idx < 0 {
		return "", ""
	}
	return rendered[:idx], rendered[idx+1:]
}
