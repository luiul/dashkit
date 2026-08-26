package mycelium

import "testing"

func TestMatchVSCodeWindowTitleMatchesTheBareFolderName(t *testing.T) {
	title, ok := matchVSCodeWindowTitle([]string{"canopy — main", "understory"}, "/x/understory")
	if !ok || title != "understory" {
		t.Fatalf("got (%q, %v), want (\"understory\", true)", title, ok)
	}
}

func TestMatchVSCodeWindowTitleMatchesFolderNamePlusBranchSuffix(t *testing.T) {
	title, ok := matchVSCodeWindowTitle([]string{"canopy — main", "dotfiles — implement-workmux"}, "/x/dotfiles")
	if !ok || title != "dotfiles — implement-workmux" {
		t.Fatalf("got (%q, %v), want (\"dotfiles — implement-workmux\", true)", title, ok)
	}
}

func TestMatchVSCodeWindowTitleDoesNotMatchAnUnrelatedLongerName(t *testing.T) {
	// "understory-lab" must not match a search for "understory": the
	// character right after the shared prefix isn't a word boundary.
	_, ok := matchVSCodeWindowTitle([]string{"understory-lab — main"}, "/x/understory")
	if ok {
		t.Fatalf("want no match, got one")
	}
}

func TestMatchVSCodeWindowTitleNoMatchWhenNothingIsOpenForThatPath(t *testing.T) {
	_, ok := matchVSCodeWindowTitle([]string{"canopy — main", "dotfiles — implement-workmux"}, "/x/understory")
	if ok {
		t.Fatalf("want no match, got one")
	}
}

func TestMatchVSCodeWindowTitleEmptyTitleListNeverMatches(t *testing.T) {
	_, ok := matchVSCodeWindowTitle(nil, "/x/understory")
	if ok {
		t.Fatalf("want no match against an empty title list")
	}
}
