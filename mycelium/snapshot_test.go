package mycelium

import (
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

// snapshotWithEntries builds a snapshot over canned registry entries.
func snapshotWithEntries(entries []registryEntry, toplevel func(string) string) *VSCodeSnapshot {
	return newVSCodeSnapshot(func() ([]registryEntry, bool) { return entries, true }, toplevel)
}

func TestSnapshotErrMeansCantTellNotClosed(t *testing.T) {
	// An unreadable registry (extension not installed) is "can't tell":
	// every IsOpen must answer false, the caller's "?" cell, and a wrong
	// "open" is worse than none.
	s := newVSCodeSnapshot(func() ([]registryEntry, bool) { return nil, false }, fakeToplevel())

	if s.Err() == nil {
		t.Fatal("want Err non-nil for an unreadable registry")
	}
	if s.IsOpen("/Users/x/dotfiles") {
		t.Fatal("IsOpen true despite an unreadable registry")
	}
	if s.IsOpenOnWorktree("/Users/x/dotfiles") {
		t.Fatal("IsOpenOnWorktree true despite an unreadable registry")
	}
}

func TestSnapshotEmptyRegistryIsNotAnError(t *testing.T) {
	// VS Code simply not running (or no window with a folder open): the
	// registry answered empty, so every IsOpen is legitimately false
	// and Err stays nil.
	s := snapshotWithEntries(nil, fakeToplevel())
	if s.Err() != nil {
		t.Fatalf("Err() = %v, want nil", s.Err())
	}
	if s.IsOpen("/Users/x/dotfiles") {
		t.Fatal("IsOpen true with no windows open")
	}
}

func TestSnapshotIsOpenViaTheRegistry(t *testing.T) {
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}},
		{SessionID: "2", Folders: []string{"/Users/x/tardis-community", "/Users/x/tardis-community/scm-analytics-engineers"}},
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/tardis-community", "/Users/x/worktrees/dotfiles")

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"exact folder", "/Users/x/dotfiles", true},
		// Identity is the folder path: a same-named worktree elsewhere
		// is a different path and does not match.
		{"same-named worktree is a different path", "/Users/x/worktrees/dotfiles", false},
		{"work-tree root for a subdirectory", "/Users/x/dotfiles/sub", true},
		{"second folder of a multi-root window", "/Users/x/tardis-community/scm-analytics-engineers", true},
		{"window nested inside the path", "/Users/x/tardis-community", true},
		{"nothing open", "/Users/x/nowhere", false},
		{"empty path never matches", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotWithEntries(entries, toplevel).IsOpen(tc.path); got != tc.want {
				t.Fatalf("IsOpen(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSnapshotIsOpenOnWorktreeViaTheRegistryIsPhantomImmune(t *testing.T) {
	// The destructive-prompt match: strict folder identity. A window
	// open on the main checkout whose SCM view happens to have the
	// worktree as its active repository (the phantom that title
	// matching could never rule out) simply is not a window on the
	// worktree's path.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}},                 // main checkout window
		{SessionID: "2", Folders: []string{"/Users/x/worktrees/understory/pkg"}}, // subpackage of a worktree
		{SessionID: "3", Folders: []string{"/Users/x/worktrees/canopy"}},         // a genuine worktree window
		{SessionID: "4", Folders: []string{"/Users/x/worktrees/canopy-sibling"}}, // prefix sibling, not nested
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"window on the worktree root", "/Users/x/worktrees/canopy", true},
		{"window on a subpackage inside the worktree", "/Users/x/worktrees/understory", true},
		// The main checkout window does not make deleting the worktree
		// warn.
		{"main checkout window is not the worktree", "/Users/x/worktrees/dotfiles", false},
		{"sibling prefix is not inside", "/Users/x/worktrees/canopy-sibling-x", false},
		{"unrelated path", "/Users/x/nowhere", false},
		{"empty path never matches", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotWithEntries(entries, fakeToplevel()).IsOpenOnWorktree(tc.path); got != tc.want {
				t.Fatalf("IsOpenOnWorktree(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestSnapshotAgreesWithOpenVSCode is the invariant the dashboards'
// columns are built on: IsOpen says true exactly when OpenVSCode, given
// the same registry entries, would focus an existing window rather than
// open a new one. Both sides run the real matcher; only the OS seams
// (registry read, toplevel, the code CLI) are faked, and faked
// identically.
func TestSnapshotAgreesWithOpenVSCode(t *testing.T) {
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}},
		{SessionID: "2", Folders: []string{"/Users/x/monorepo/packages/bar"}},
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/monorepo")

	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) { return entries, true }
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

	snapshot := snapshotWithEntries(entries, toplevel)

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
	entries := []registryEntry{{SessionID: "1", Folders: []string{"/Users/x/other"}}}
	s := newVSCodeSnapshot(func() ([]registryEntry, bool) { return entries, true }, counting)

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
