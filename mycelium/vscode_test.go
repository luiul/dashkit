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

func TestParseVSCodeWindowListParsesMultipleRecords(t *testing.T) {
	raw := "understory — untruncate-branch\x1f\x1e" +
		"scm-analytics-engineers — deploy-full-cost — README.md\x1ffile:///x/tardis-community/scm-analytics-engineers/README.md\x1e"
	windows := parseVSCodeWindowList(raw)
	if len(windows) != 2 {
		t.Fatalf("got %d windows, want 2: %+v", len(windows), windows)
	}
	if windows[0] != (vscodeWindow{Title: "understory — untruncate-branch", Path: ""}) {
		t.Fatalf("got %+v, want title-only window with no path", windows[0])
	}
	want := vscodeWindow{
		Title: "scm-analytics-engineers — deploy-full-cost — README.md",
		Path:  "/x/tardis-community/scm-analytics-engineers/README.md",
	}
	if windows[1] != want {
		t.Fatalf("got %+v, want %+v", windows[1], want)
	}
}

func TestParseVSCodeWindowListIgnoresTheTrailingRecordSeparatorEveryWindowLeavesBehind(t *testing.T) {
	// The AppleScript always appends RS after every record, including the
	// last one, so splitting on RS always leaves one trailing empty
	// element that isn't a real window and must not become a bogus
	// zero-value entry in the result.
	windows := parseVSCodeWindowList("dotfiles — main\x1f\x1e")
	if len(windows) != 1 {
		t.Fatalf("got %d windows, want 1 (the trailing empty record must be dropped): %+v", len(windows), windows)
	}
}

func TestParseVSCodeWindowListTrimsATrailingNewlineFromARecord(t *testing.T) {
	// osascript can carry a trailing newline through on some runs;
	// runOsascript's own TrimSpace only strips it from the very end of
	// the whole output, not from an individual record if it ends up
	// elsewhere, so parseVSCodeWindowList must handle it itself too.
	windows := parseVSCodeWindowList("dotfiles — main\x1f\r\n\x1e")
	if len(windows) != 1 || windows[0].Title != "dotfiles — main" {
		t.Fatalf("got %+v, want a single window titled %q", windows, "dotfiles — main")
	}
}

func TestParseVSCodeWindowListHandlesAWindowWithNoFieldSeparatorAtAll(t *testing.T) {
	// strings.Cut's own "not found" case: a record with no \x1f in it at
	// all (shouldn't happen given the AppleScript always emits one, but
	// the parser shouldn't panic or misbehave if it ever does) is treated
	// as a title with no path, not dropped.
	windows := parseVSCodeWindowList("dotfiles — main\x1e")
	if len(windows) != 1 || windows[0] != (vscodeWindow{Title: "dotfiles — main", Path: ""}) {
		t.Fatalf("got %+v", windows)
	}
}

func TestParseVSCodeWindowListIsEmptyForAnEmptyInput(t *testing.T) {
	// The "" case vscodeWindows' own AppleScript returns when VS Code
	// isn't running at all — must come back as no windows, not one bogus
	// empty-titled entry.
	if windows := parseVSCodeWindowList(""); windows != nil {
		t.Fatalf("got %+v, want nil for an empty input", windows)
	}
}
