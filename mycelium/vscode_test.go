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

func TestMatchVSCodeWindowNestedPathMatchesAFileOpenSomewhereInside(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "scm-analytics-engineers — deploy-full-cost", Path: "/x/tardis-community/pipelines/intl-scm-analytics/scm-analytics-engineers/README.md"},
	}
	title, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community")
	if !ok || title != "scm-analytics-engineers — deploy-full-cost" {
		t.Fatalf("got (%q, %v), want (%q, true)", title, ok, "scm-analytics-engineers — deploy-full-cost")
	}
}

func TestMatchVSCodeWindowNestedPathSkipsWindowsWithNoFileFocused(t *testing.T) {
	// A window sitting on the Explorer/Search panel, or an empty editor
	// group, has Path == "" (see vscodeWindows' doc) and must never be
	// treated as a match — that's indistinguishable from a window that
	// was never opened on this path at all.
	windows := []vscodeWindow{{Title: "scm-analytics-engineers — deploy-full-cost", Path: ""}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community")
	if ok {
		t.Fatalf("want no match when the candidate window has no file focused")
	}
}

func TestMatchVSCodeWindowNestedPathDoesNotMatchASiblingWithASharedPrefix(t *testing.T) {
	// "/x/tardis-community-lab" is not inside "/x/tardis-community": the
	// match must require a real path-separator boundary, not just a
	// shared string prefix, mirroring matchVSCodeWindowTitle's own
	// word-boundary care for titles.
	windows := []vscodeWindow{{Title: "tardis-community-lab — main", Path: "/x/tardis-community-lab/README.md"}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community")
	if ok {
		t.Fatalf("want no match against an unrelated sibling directory")
	}
}

func TestMatchVSCodeWindowNestedPathDoesNotMatchThePathItselfAsNested(t *testing.T) {
	// A file literally at path (not inside a subdirectory of it) doesn't
	// count as "nested": that's the exact-match case matchVSCodeWindowTitle
	// already owns.
	windows := []vscodeWindow{{Title: "tardis-community — main", Path: "/x/tardis-community"}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community")
	if ok {
		t.Fatalf("want no match when the window's path equals path itself, not a subdirectory of it")
	}
}

func TestMatchVSCodeWindowNestedPathNoMatchAgainstAnEmptyWindowList(t *testing.T) {
	_, ok := matchVSCodeWindowNestedPath(nil, "/x/tardis-community")
	if ok {
		t.Fatalf("want no match against an empty window list")
	}
}

func TestFileURLToPathDecodesAPercentEncodedFileURL(t *testing.T) {
	got := fileURLToPath("file:///Users/x/My%20Repo/README.md")
	want := "/Users/x/My Repo/README.md"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFileURLToPathRejectsANonFileScheme(t *testing.T) {
	if got := fileURLToPath("https://example.com/x"); got != "" {
		t.Fatalf("got %q, want empty for a non-file URL", got)
	}
}

func TestFileURLToPathIsEmptyForAnEmptyInput(t *testing.T) {
	if got := fileURLToPath(""); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
