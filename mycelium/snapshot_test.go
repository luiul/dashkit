package mycelium

import (
	"errors"
	"strings"
	"testing"
)

// fakeToplevel maps directories to git work-tree roots the way
// gitToplevel would, without shelling out to git: anything under a
// listed root resolves to that root, everything else (outside any work
// tree) resolves to "".
func fakeToplevel(roots ...string) func(string) string {
	return func(dir string) string {
		best := ""
		for _, root := range roots {
			if dir == root || strings.HasPrefix(dir, root+"/") {
				if len(root) > len(best) {
					best = root
				}
			}
		}
		return best
	}
}

const testHome = "/Users/x"

// snapshotWithTitles builds a VS Code snapshot over a canned window
// listing of a running VS Code.
func snapshotWithTitles(titles []string, toplevel func(string) string) *WindowSnapshot {
	return newWindowSnapshot(func() ([]string, bool, error) { return titles, true, nil }, parseVSCodeWindows, "vscode", testHome, toplevel)
}

// ghosttySnapshotWithTitles builds a Ghostty snapshot over a canned
// window listing of a running Ghostty.
func ghosttySnapshotWithTitles(titles []string, toplevel func(string) string) *WindowSnapshot {
	return newWindowSnapshot(func() ([]string, bool, error) { return titles, true, nil }, parseGhosttyWindows, "ghostty", testHome, toplevel)
}

func TestSnapshotErrMeansCantTellNotClosed(t *testing.T) {
	// A failed listing (Automation permission pending, most likely) is
	// "can't tell": every IsOpen must answer false, the caller's "?"
	// cell, and a wrong "open" is worse than none.
	s := newWindowSnapshot(func() ([]string, bool, error) {
		return nil, false, errors.New("grant Automation permission")
	}, parseVSCodeWindows, "vscode", testHome, fakeToplevel())

	if s.Err() == nil {
		t.Fatal("want Err non-nil for a failed listing")
	}
	if s.IsOpen("/Users/x/dotfiles") {
		t.Fatal("IsOpen true despite a failed listing")
	}
	if s.IsOpenOnWorktree("/Users/x/dotfiles") {
		t.Fatal("IsOpenOnWorktree true despite a failed listing")
	}
}

func TestSnapshotEmptyListingWhileRunningIsCantTell(t *testing.T) {
	// Zero windows listed while Code runs is the AX-cull signature
	// (luiul/dashkit#9), and a poll can't activate Code to re-check:
	// Err, not "definitely nothing open".
	s := snapshotWithTitles(nil, fakeToplevel())
	if s.Err() == nil {
		t.Fatal("want Err non-nil for an empty listing while Code runs")
	}
	if s.IsOpen("/Users/x/dotfiles") {
		t.Fatal("IsOpen true on an untrustworthy listing")
	}
}

func TestSnapshotNotRunningIsNotAnError(t *testing.T) {
	// VS Code simply closed: nothing is open, definitively, so every
	// IsOpen is legitimately false and Err stays nil.
	s := newWindowSnapshot(func() ([]string, bool, error) { return nil, false, nil }, parseVSCodeWindows, "vscode", testHome, fakeToplevel())
	if s.Err() != nil {
		t.Fatalf("Err() = %v, want nil", s.Err())
	}
	if s.IsOpen("/Users/x/dotfiles") {
		t.Fatal("IsOpen true with VS Code not running")
	}
}

func TestSnapshotIsOpen(t *testing.T) {
	titles := []string{
		"~/dotfiles — main",
		"~/worktrees/dotfiles — feat/x",
		"~/monorepo/packages/bar — main",
		// Multi-root windows title on the workspace file, so they match
		// only on that path (accepted limitation).
		"~/tardis-community.code-workspace — main",
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/worktrees/dotfiles", "/Users/x/monorepo")

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"exact folder", "/Users/x/dotfiles", true},
		// Identity is the folder path: a same-named worktree elsewhere
		// is a different path and does not match.
		{"same-named worktree is a different path", "/Users/x/worktrees/tardis-community", false},
		{"worktree window matched on its own path", "/Users/x/worktrees/dotfiles", true},
		{"work-tree root for a subdirectory", "/Users/x/dotfiles/sub", true},
		{"window nested inside the path", "/Users/x/monorepo", true},
		{"multi-root window matches its workspace file path", "/Users/x/tardis-community.code-workspace", true},
		{"multi-root member folder does not match", "/Users/x/tardis-community/scm-analytics-engineers", false},
		{"nothing open", "/Users/x/nowhere", false},
		{"empty path never matches", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotWithTitles(titles, toplevel).IsOpen(tc.path); got != tc.want {
				t.Fatalf("IsOpen(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSnapshotIsOpenOnWorktreeIsPhantomImmune(t *testing.T) {
	// The destructive-prompt match: strict folder identity. A window
	// open on the main checkout whose SCM view happens to have the
	// worktree as its active repository renders the worktree's branch
	// in its title — and still is not a window on the worktree's path,
	// because the branch component is never matched.
	titles := []string{
		"~/dotfiles — feat/worktree-branch",   // main checkout, phantom branch
		"~/worktrees/understory/pkg — feat/x", // subpackage of a worktree
		"~/worktrees/canopy — feat/y",         // a genuine worktree window
		"~/worktrees/canopy-sibling — feat/z", // prefix sibling, not nested
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"window on the worktree root", "/Users/x/worktrees/canopy", true},
		{"window on a subpackage inside the worktree", "/Users/x/worktrees/understory", true},
		// The phantom's branch component does not make deleting the
		// worktree warn.
		{"main checkout window is not the worktree", "/Users/x/worktrees/dotfiles", false},
		{"sibling prefix is not inside", "/Users/x/worktrees/canopy-sibling-x", false},
		{"unrelated path", "/Users/x/nowhere", false},
		{"empty path never matches", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotWithTitles(titles, fakeToplevel()).IsOpenOnWorktree(tc.path); got != tc.want {
				t.Fatalf("IsOpenOnWorktree(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestSnapshotAgreesWithOpenVSCode is the invariant the dashboards'
// columns are built on: IsOpen says true exactly when OpenVSCode, given
// the same window listing, would focus an existing window rather than
// open a new one. Both sides run the real matcher; only the OS seams
// (window listing, toplevel, raise, the code CLI) are faked, and faked
// identically.
func TestSnapshotAgreesWithOpenVSCode(t *testing.T) {
	titles := []string{
		"~/dotfiles — main",
		"~/monorepo/packages/bar — main",
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/monorepo")

	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) { return titles, true, nil }
	d.toplevel = toplevel
	openedNew := false
	d.runCommand = func(args []string) (bool, string) {
		for _, a := range args {
			if a == "-n" {
				openedNew = true
			}
		}
		return true, ""
	}

	snapshot := snapshotWithTitles(titles, toplevel)

	cases := []string{
		"/Users/x/dotfiles",
		"/Users/x/dotfiles/sub",
		"/Users/x/monorepo",
		"/Users/x/nowhere",
	}
	for _, path := range cases {
		openedNew = false
		result := openVSCode(d, path)
		if !result.OK {
			t.Fatalf("openVSCode(%q) failed: %+v", path, result)
		}
		wouldFocus := !openedNew
		if got := snapshot.IsOpen(path); got != wouldFocus {
			t.Fatalf("IsOpen(%q) = %v, but OpenVSCode wouldFocus = %v", path, got, wouldFocus)
		}
	}
}

func TestSnapshotMemoizesToplevelLookups(t *testing.T) {
	calls := map[string]int{}
	counting := func(dir string) string {
		calls[dir]++
		return fakeToplevel("/Users/x/repo")(dir)
	}
	s := newWindowSnapshot(func() ([]string, bool, error) {
		return []string{"~/other — main"}, true, nil
	}, parseVSCodeWindows, "vscode", testHome, counting)

	// Two rows under the same root, each missing the exact match and
	// falling through to the work-tree-root stage: every directory's
	// work-tree root must be resolved at most once across both calls,
	// or a poll of N rows pays N git subprocesses for the same answer.
	s.IsOpen("/Users/x/repo")
	s.IsOpen("/Users/x/repo/sub")
	for dir, n := range calls {
		if n > 1 {
			t.Fatalf("toplevel(%q) called %d times, want 1", dir, n)
		}
	}
}

// TestSnapshotAgreesWithOpenGhostty is the column's invariant: IsOpen
// says true exactly when OpenGhostty, given the same window listing,
// would focus an existing window rather than open a new one. Only the
// OS seams are faked, identically on both sides.
func TestSnapshotAgreesWithOpenGhostty(t *testing.T) {
	titles := []string{
		"~/dotfiles • main",
		"π • ~/worktrees/canopy • feat/y • ttys004",
	}
	toplevel := fakeToplevel("/Users/x/worktrees/canopy")

	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) { return titles, true, nil }
	d.toplevel = toplevel
	openedNew := false
	d.ghosttyOpenNewWindow = func(string) error { openedNew = true; return nil }

	s := newWindowSnapshot(
		func() ([]string, bool, error) { return titles, true, nil },
		parseGhosttyWindows, "ghostty", testHome, toplevel,
	)

	cases := []string{
		"/Users/x/dotfiles",
		"/Users/x/dotfiles/sub",
		"/Users/x/worktrees/canopy",
		"/Users/x/nowhere",
	}
	for _, path := range cases {
		openedNew = false
		result := openGhostty(d, "", path)
		if !result.OK {
			t.Fatalf("openGhostty(%q) failed: %+v", path, result)
		}
		wouldFocus := !openedNew
		if got := s.IsOpen(path); got != wouldFocus {
			t.Fatalf("IsOpen(%q) = %v, but OpenGhostty wouldFocus = %v", path, got, wouldFocus)
		}
	}
}

// TestGhosttySnapshotMatchesGhosttyTitles pins the Ghostty title grammar
// end to end: a pi-session title ("π • root • branch") and a
// running-command title ("cmd • root • branch") both resolve the root,
// so a poll's open column sees agent windows at a plain prompt and
// mid-run alike.
func TestGhosttySnapshotMatchesGhosttyTitles(t *testing.T) {
	titles := []string{
		"~/dotfiles • main",               // zsh hook, idle prompt
		"go test • ~/dashkit • fix/x",     // zsh hook, command running
		"π • ~/worktrees/canopy • feat/y", // pi session (session-title extension)
	}
	s := ghosttySnapshotWithTitles(titles, fakeToplevel("/Users/x/worktrees/canopy"))
	for _, path := range []string{"/Users/x/dotfiles", "/Users/x/dashkit", "/Users/x/worktrees/canopy"} {
		if !s.IsOpen(path) {
			t.Fatalf("IsOpen(%q) false, want true", path)
		}
	}
	if s.IsOpen("/Users/x/nowhere") {
		t.Fatal("IsOpen true for an unopened path")
	}
}

// TestGhosttySnapshotMatchesTruncatedTitles pins the narrow-window
// case end to end: a title head-truncated by the titlebar (leading …)
// keeps its window in the snapshot and IsOpen answers true for the
// worktree whose path ends with the visible tail — the column's
// "open" must agree with OpenGhostty's focus on the same listing.
// A too-weak tail and a non-matching sibling stay false.
func TestGhosttySnapshotMatchesTruncatedTitles(t *testing.T) {
	titles := []string{
		"…/hellofresh/review-zau/global-ops", // narrow window, head-truncated
	}
	s := ghosttySnapshotWithTitles(titles, fakeToplevel())

	want := "/Users/x/worktrees/hellofresh/review-zau/global-ops"
	if !s.IsOpen(want) {
		t.Fatalf("IsOpen(%q) false, want true", want)
	}
	if s.IsOpen(want + "-2") {
		t.Fatal("IsOpen true for a sibling path that doesn't share the tail")
	}
	if s.IsOpenOnWorktree(want) {
		t.Fatal("IsOpenOnWorktree true on a truncated window: the strict match must stay conservative")
	}
}

// TestGhosttySnapshotDropsTooWeakTruncatedTitles pins the guard: a
// truncation whose visible path part carries no separator is not
// kept as a matchable window at all — erring toward "not open"
// rather than a guess.
func TestGhosttySnapshotDropsTooWeakTruncatedTitles(t *testing.T) {
	s := ghosttySnapshotWithTitles([]string{"…global-ops", "… • main • ttys004"}, fakeToplevel())

	want := "/Users/x/worktrees/hellofresh/review-zau/global-ops"
	if s.IsOpen(want) {
		t.Fatal("IsOpen true on a tail with no element boundary")
	}
}
