package sieve

import "testing"

func TestMatchEmptyQuery(t *testing.T) {
	if !Match("", "anything") {
		t.Fatal("empty query should match everything")
	}
	if !Match("") {
		t.Fatal("empty query should match even a row with no cells")
	}
}

func TestMatchSubsequence(t *testing.T) {
	// Subsequence, not substring: the query's characters only need to
	// appear in order, with anything at all in between.
	if !Match("cnp", "canopy") {
		t.Fatal(`"cnp" should fuzzy-match "canopy"`)
	}
	if Match("cpn", "canopy") {
		t.Fatal(`"cpn" should not match "canopy": order matters`)
	}
	if Match("xyz", "canopy") {
		t.Fatal(`"xyz" should not match "canopy"`)
	}
}

func TestMatchCaseInsensitive(t *testing.T) {
	if !Match("vscode", "VS Code") {
		t.Fatal("lowercase query should match mixed-case cell")
	}
	if !Match("PID", "pid 4271") {
		t.Fatal("uppercase query should match lowercase cell")
	}
}

func TestMatchAcrossCells(t *testing.T) {
	// The query spans the space-joined cells: cell boundaries (and the
	// join's own spaces) are just more skippable characters.
	if !Match("pi4271", "pi", "4271") {
		t.Fatal("query should match across cell boundaries")
	}
	if !Match("~ dashkit", "~/projects", "dashkit") {
		t.Fatal("query with a space should match across the join")
	}
}

func TestMatchExactCellSubset(t *testing.T) {
	// A verbatim substring of one cell is a subsequence too, so plain
	// "type what you see" filtering works unchanged.
	if !Match("working", "done", "working", "idle") {
		t.Fatal("verbatim cell text should match")
	}
}
