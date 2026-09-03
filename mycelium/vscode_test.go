package mycelium

import (
	"strings"
	"testing"
)

func TestMatchVSCodeWindowTitleMatchesTheBareFolderName(t *testing.T) {
	title, ok := matchVSCodeWindowTitle([]string{"canopy — main", "understory"}, "/x/understory", "")
	if !ok || title != "understory" {
		t.Fatalf("got (%q, %v), want (\"understory\", true)", title, ok)
	}
}

func TestMatchVSCodeWindowTitleMatchesFolderNamePlusBranchSuffix(t *testing.T) {
	title, ok := matchVSCodeWindowTitle([]string{"canopy — main", "dotfiles — implement-workmux"}, "/x/dotfiles", "")
	if !ok || title != "dotfiles — implement-workmux" {
		t.Fatalf("got (%q, %v), want (\"dotfiles — implement-workmux\", true)", title, ok)
	}
}

func TestMatchVSCodeWindowTitleDoesNotMatchAnUnrelatedLongerName(t *testing.T) {
	// "understory-lab" must not match a search for "understory": the
	// character right after the shared prefix isn't a word boundary.
	_, ok := matchVSCodeWindowTitle([]string{"understory-lab — main"}, "/x/understory", "")
	if ok {
		t.Fatalf("want no match, got one")
	}
}

func TestMatchVSCodeWindowTitleNoMatchWhenNothingIsOpenForThatPath(t *testing.T) {
	_, ok := matchVSCodeWindowTitle([]string{"canopy — main", "dotfiles — implement-workmux"}, "/x/understory", "")
	if ok {
		t.Fatalf("want no match, got one")
	}
}

func TestMatchVSCodeWindowTitleEmptyTitleListNeverMatches(t *testing.T) {
	_, ok := matchVSCodeWindowTitle(nil, "/x/understory", "")
	if ok {
		t.Fatalf("want no match against an empty title list")
	}
}

func TestMatchVSCodeWindowTitleWithBranchPrefersTheSameBranchWindow(t *testing.T) {
	// This ecosystem's worktree layout gives every worktree of a repo the
	// repo's own leaf folder name, so two open windows can both start with
	// "tardis-community " while being different folders (the main checkout
	// vs. a branch worktree): only the branch component tells them apart.
	titles := []string{"tardis-community — main", "tardis-community — patch/ISA-18409"}
	title, ok := matchVSCodeWindowTitle(titles, "/worktrees/x/tardis-community", "patch/ISA-18409")
	if !ok || title != "tardis-community — patch/ISA-18409" {
		t.Fatalf("got (%q, %v), want the branch worktree's window", title, ok)
	}
}

func TestMatchVSCodeWindowTitleWithBranchRejectsASameNamedFolderOnAnotherBranch(t *testing.T) {
	// The only window with a matching rootName is on a *different*
	// branch, i.e. guaranteed a different folder sharing the leaf name —
	// focusing it would be wrong, so the match must fail rather than fall
	// back to it.
	_, ok := matchVSCodeWindowTitle([]string{"tardis-community — main"}, "/worktrees/x/tardis-community", "patch/ISA-18409")
	if ok {
		t.Fatalf("want no match for a same-named window on a different branch")
	}
}

func TestMatchVSCodeWindowTitleWithBranchKeepsABareTitleAsWeakFallback(t *testing.T) {
	// A window titled with just the folder name (no branch component at
	// all) can't be ruled out as a same-named other folder, so it still
	// matches — but only once no rootName+branch match exists.
	titles := []string{"understory"}
	title, ok := matchVSCodeWindowTitle(titles, "/x/understory", "main")
	if !ok || title != "understory" {
		t.Fatalf("got (%q, %v), want the bare-title fallback", title, ok)
	}
}

func TestMatchVSCodeWindowTitleWithBranchPrefersAFullMatchOverTheWeakFallback(t *testing.T) {
	titles := []string{"understory", "understory — fix-cursor"}
	title, ok := matchVSCodeWindowTitle(titles, "/x/understory", "fix-cursor")
	if !ok || title != "understory — fix-cursor" {
		t.Fatalf("got (%q, %v), want the rootName+branch match, not the bare title", title, ok)
	}
}

func TestMatchVSCodeWindowTitleWithBranchStillRejectsAnUnrelatedLongerName(t *testing.T) {
	// The word-boundary care of the legacy match must survive parsing:
	// "understory-lab"'s rootName is not "understory", branch match or
	// not.
	_, ok := matchVSCodeWindowTitle([]string{"understory-lab — fix-cursor"}, "/x/understory", "fix-cursor")
	if ok {
		t.Fatalf("want no match for an unrelated longer folder name")
	}
}

func TestMatchVSCodeWindowTitleMatchesAMonorepoSubpackageWindowByRootNameAndBranch(t *testing.T) {
	// The reported canopy bug: a window open directly on a monorepo
	// package (…/scm-analytics-engineers/dbt) titles itself with the
	// package's own folder name and the repo's branch, so an agent cwd
	// pointing at that package matches it exactly — even on a generic
	// branch, since the rootName half disambiguates (the generic-branch
	// guard applies only to the branch-*only* match).
	titles := []string{"scm-analytics-engineers", "dbt — master", "dashkit — main"}
	title, ok := matchVSCodeWindowTitle(titles, "/Users/x/tardis-community/pipelines/intl-scm-analytics/scm-analytics-engineers/dbt", "master")
	if !ok || title != "dbt — master" {
		t.Fatalf("got (%q, %v), want the subpackage window", title, ok)
	}
}

func TestMatchVSCodeWindowBranchFindsTheOneWindowCarryingTheBranch(t *testing.T) {
	// The reported scenario: the window to reuse is open on a subpackage
	// inside the worktree and has no file focused, so only its title's
	// branch component can find it.
	titles := []string{
		"isa-analytics — sync-snow-bricks",
		"understory — main — main.go",
		"scm-analytics-engineers — patch/ISA-18409-fix-staticprice-satellite-replay-dups",
	}
	title, ok := matchVSCodeWindowBranch(titles, "patch/ISA-18409-fix-staticprice-satellite-replay-dups")
	if !ok || title != "scm-analytics-engineers — patch/ISA-18409-fix-staticprice-satellite-replay-dups" {
		t.Fatalf("got (%q, %v), want the nested subpackage window", title, ok)
	}
}

func TestMatchVSCodeWindowBranchMatchesAcrossSeparatorAndFileParts(t *testing.T) {
	// The same window with a file focused (title gains a third part) must
	// still match: the branch is the middle component, not the suffix.
	titles := []string{"scm-analytics-engineers — patch/ISA-18409 — README.md"}
	title, ok := matchVSCodeWindowBranch(titles, "patch/ISA-18409")
	if !ok || title != titles[0] {
		t.Fatalf("got (%q, %v), want (%q, true)", title, ok, titles[0])
	}
}

func TestMatchVSCodeWindowBranchNeverMatchesAGenericBranchName(t *testing.T) {
	// Practically every repo has a main checked out somewhere, so a title
	// carrying "main" says nothing about which repo's window it is.
	for _, branch := range []string{"main", "master", "develop", "trunk"} {
		if _, ok := matchVSCodeWindowBranch([]string{"other-repo — " + branch}, branch); ok {
			t.Fatalf("want no match for generic branch %q", branch)
		}
	}
}

func TestMatchVSCodeWindowBranchNeverMatchesAnEmptyBranch(t *testing.T) {
	if _, ok := matchVSCodeWindowBranch([]string{"understory — main"}, ""); ok {
		t.Fatalf("want no match for an empty branch")
	}
}

func TestMatchVSCodeWindowBranchIsAmbiguousAcrossTwoDifferentWindows(t *testing.T) {
	// Two repos can share a ticket-branch name: two *different* titles
	// carrying the branch is no answer at all, and the caller must fall
	// through to opening a new window rather than focus an arbitrary one.
	titles := []string{
		"scm-analytics-engineers — patch/ISA-18409",
		"other-repo — patch/ISA-18409",
	}
	if _, ok := matchVSCodeWindowBranch(titles, "patch/ISA-18409"); ok {
		t.Fatalf("want no match when two different windows carry the branch")
	}
}

func TestMatchVSCodeWindowBranchAllowsDuplicateWindowsOnTheSameTitle(t *testing.T) {
	// Two windows with the *same* title are both the window being looked
	// for (e.g. a duplicate already open on the same folder), so that's
	// not ambiguity.
	titles := []string{
		"scm-analytics-engineers — patch/ISA-18409",
		"scm-analytics-engineers — patch/ISA-18409",
	}
	title, ok := matchVSCodeWindowBranch(titles, "patch/ISA-18409")
	if !ok || title != titles[0] {
		t.Fatalf("got (%q, %v), want (%q, true)", title, ok, titles[0])
	}
}

func TestParseVSCodeTitleSplitsAllThreeParts(t *testing.T) {
	root, branch := parseVSCodeTitle("understory — main — main.go")
	if root != "understory" || branch != "main" {
		t.Fatalf("got (%q, %q), want (\"understory\", \"main\")", root, branch)
	}
}

func TestParseVSCodeTitleSplitsRootAndBranchWithoutAnEditorPart(t *testing.T) {
	root, branch := parseVSCodeTitle("scm-analytics-engineers — patch/ISA-18409")
	if root != "scm-analytics-engineers" || branch != "patch/ISA-18409" {
		t.Fatalf("got (%q, %q)", root, branch)
	}
}

func TestParseVSCodeTitleToleratesAPlainHyphenSeparator(t *testing.T) {
	root, branch := parseVSCodeTitle("understory - main")
	if root != "understory" || branch != "main" {
		t.Fatalf("got (%q, %q)", root, branch)
	}
}

func TestParseVSCodeTitleKeepsDashesInsideTheRootName(t *testing.T) {
	// Dashes without surrounding whitespace are part of the folder name,
	// not separators.
	root, branch := parseVSCodeTitle("scm-analytics-engineers — main")
	if root != "scm-analytics-engineers" || branch != "main" {
		t.Fatalf("got (%q, %q)", root, branch)
	}
}

func TestParseVSCodeTitleReturnsABareTitleAsRootOnly(t *testing.T) {
	root, branch := parseVSCodeTitle("understory")
	if root != "understory" || branch != "" {
		t.Fatalf("got (%q, %q), want (\"understory\", \"\")", root, branch)
	}
}

// rootedAt returns a fake git-toplevel resolver for the nested-match
// tests: any directory inside one of the given work-tree roots resolves
// to that root, everything else to "" (not a work tree). The boundary
// check mirrors git's own: a root matches only the tree it actually
// heads, never a sibling sharing a string prefix.
func rootedAt(roots ...string) func(string) string {
	return func(dir string) string {
		for _, root := range roots {
			if dir == root || strings.HasPrefix(dir, root+"/") {
				return root
			}
		}
		return ""
	}
}

func TestMatchVSCodeWindowNestedPathMatchesAFileOpenSomewhereInside(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "scm-analytics-engineers — deploy-full-cost", Path: "/x/tardis-community/pipelines/intl-scm-analytics/scm-analytics-engineers/README.md"},
	}
	title, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", rootedAt("/x/tardis-community"))
	if !ok || title != "scm-analytics-engineers — deploy-full-cost" {
		t.Fatalf("got (%q, %v), want (%q, true)", title, ok, "scm-analytics-engineers — deploy-full-cost")
	}
}

func TestMatchVSCodeWindowNestedPathMatchesAFileAnywhereInTheSameWorkTree(t *testing.T) {
	// The match keys on the work tree, not on directory containment: a
	// window with a file focused anywhere in path's repo counts as open
	// inside path, even when that file sits directly at the repo root.
	windows := []vscodeWindow{{Title: "tardis-community — main", Path: "/x/tardis-community/README.md"}}
	title, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", rootedAt("/x/tardis-community"))
	if !ok || title != "tardis-community — main" {
		t.Fatalf("got (%q, %v), want a same-work-tree match", title, ok)
	}
}

func TestMatchVSCodeWindowNestedPathMatchesANestedRepoInsideTheTarget(t *testing.T) {
	// A window's file whose own work-tree root sits *underneath* path's
	// (a submodule, a standalone clone vendored inside it) is genuinely
	// scoped inside the target tree and matches.
	windows := []vscodeWindow{{Title: "vendor-lib — main", Path: "/x/tardis-community/vendor/lib/main.go"}}
	top := rootedAt("/x/tardis-community")
	withNested := func(dir string) string {
		if dir == "/x/tardis-community/vendor/lib" || strings.HasPrefix(dir, "/x/tardis-community/vendor/lib/") {
			return "/x/tardis-community/vendor/lib"
		}
		return top(dir)
	}
	title, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", withNested)
	if !ok || title != "vendor-lib — main" {
		t.Fatalf("got (%q, %v), want the nested-repo window to match", title, ok)
	}
}

func TestMatchVSCodeWindowNestedPathSkipsWindowsWithNoFileFocused(t *testing.T) {
	// A window sitting on the Explorer/Search panel, or an empty editor
	// group, has Path == "" (see vscodeWindows' doc) and must never be
	// treated as a match — that's indistinguishable from a window that
	// was never opened on this path at all.
	windows := []vscodeWindow{{Title: "scm-analytics-engineers — deploy-full-cost", Path: ""}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", rootedAt("/x/tardis-community"))
	if ok {
		t.Fatalf("want no match when the candidate window has no file focused")
	}
}

func TestMatchVSCodeWindowNestedPathDoesNotMatchASiblingWorkTree(t *testing.T) {
	// "/x/tardis-community-lab" is a different work tree from
	// "/x/tardis-community": a shared string prefix is not containment.
	windows := []vscodeWindow{{Title: "tardis-community-lab — main", Path: "/x/tardis-community-lab/README.md"}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", rootedAt("/x/tardis-community", "/x/tardis-community-lab"))
	if ok {
		t.Fatalf("want no match against a sibling work tree")
	}
}

func TestMatchVSCodeWindowNestedPathNeverMatchesAFileOutsideAnyWorkTree(t *testing.T) {
	// The misfire this git grounding exists for: a window scoped to some
	// unrelated folder but currently showing a ~/scratch file must not
	// count as "open inside" any repo target — its focused file resolves
	// to no work tree at all.
	windows := []vscodeWindow{{Title: "global-ops — ISA-18423", Path: "/Users/x/scratch/note.md"}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/x/tardis-community", rootedAt("/x/tardis-community"))
	if ok {
		t.Fatalf("want no match when the window's focused file is outside every work tree")
	}
}

func TestMatchVSCodeWindowNestedPathNeverMatchesATargetOutsideAnyWorkTree(t *testing.T) {
	// The other half of that misfire: a target that isn't inside a work
	// tree itself (~/scratch, $HOME, /tmp) has nothing to key on, so a
	// window that merely has one of its files focused must never claim
	// it — before git grounding, $HOME "matched" whichever window
	// enumerated first, since every file lives under it.
	windows := []vscodeWindow{{Title: "global-ops — ISA-18423", Path: "/Users/x/scratch/note.md"}}
	_, ok := matchVSCodeWindowNestedPath(windows, "/Users/x", rootedAt("/x/tardis-community"))
	if ok {
		t.Fatalf("want no match when the target itself is outside every work tree")
	}
}

func TestMatchVSCodeWindowNestedPathNoMatchAgainstAnEmptyWindowList(t *testing.T) {
	_, ok := matchVSCodeWindowNestedPath(nil, "/x/tardis-community", rootedAt("/x/tardis-community"))
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

func TestMatchVSCodeWindowTitleStrictMatchesRootAndBranch(t *testing.T) {
	title, ok := matchVSCodeWindowTitleStrict([]string{"dotfiles — fix-x — .zshrc"}, "/x/dotfiles", "fix-x")
	if !ok || title != "dotfiles — fix-x — .zshrc" {
		t.Fatalf("got (%q, %v), want a match", title, ok)
	}
}

func TestMatchVSCodeWindowTitleStrictDropsTheBranchlessWeakFallback(t *testing.T) {
	// The difference from matchVSCodeWindowTitle that the strict variant
	// exists for (see its doc): a bare "dotfiles" title carries no branch
	// information, and for a destructive-action warning matching it would
	// fire on every removal while any bare-titled window of that repo is
	// around (e.g. a main checkout whose SCM branch hasn't resolved).
	if _, ok := matchVSCodeWindowTitleStrict([]string{"dotfiles"}, "/x/dotfiles", "fix-x"); ok {
		t.Fatalf("want no match for a branchless title")
	}
}

func TestMatchVSCodeWindowTitleStrictRejectsADifferentBranch(t *testing.T) {
	// A same-named folder on another branch (the main checkout's window)
	// is not a window on this worktree.
	if _, ok := matchVSCodeWindowTitleStrict([]string{"dotfiles — main"}, "/x/dotfiles", "fix-x"); ok {
		t.Fatalf("want no match for a different branch")
	}
}

func TestMatchVSCodeWindowTitleStrictRejectsAnUnrelatedLongerName(t *testing.T) {
	if _, ok := matchVSCodeWindowTitleStrict([]string{"understory-lab — fix-x"}, "/x/understory", "fix-x"); ok {
		t.Fatalf("want no match for a prefix-sibling root")
	}
}

func TestMatchVSCodeWindowTitleStrictNeedsABranch(t *testing.T) {
	if _, ok := matchVSCodeWindowTitleStrict([]string{"dotfiles — fix-x"}, "/x/dotfiles", ""); ok {
		t.Fatalf("want no match without a branch to key on")
	}
}
