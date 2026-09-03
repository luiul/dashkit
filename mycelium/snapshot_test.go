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

func snapshotWith(windows []vscodeWindow, toplevel func(string) string) *VSCodeSnapshot {
	return newVSCodeSnapshot(func() ([]vscodeWindow, error) { return windows, nil }, toplevel)
}

func TestSnapshotErrMeansCantTellNotClosed(t *testing.T) {
	listingErr := errors.New("osascript: not authorized")
	s := newVSCodeSnapshot(func() ([]vscodeWindow, error) { return nil, listingErr }, fakeToplevel())

	if !errors.Is(s.Err(), listingErr) {
		t.Fatalf("Err() = %v, want %v", s.Err(), listingErr)
	}
	// Even with a window that would obviously match, a failed listing
	// must answer false: "can't tell" is the caller's "?" cell, and a
	// wrong "open" is worse than none.
	if s.IsOpen("/Users/x/dotfiles", "main") {
		t.Fatal("IsOpen true despite a failed listing")
	}
}

func TestSnapshotEmptyListingIsNotAnError(t *testing.T) {
	// VS Code simply not running: vscodeWindows returns an empty slice
	// and no error (see its doc), so every IsOpen is legitimately false
	// and Err stays nil.
	s := snapshotWith(nil, fakeToplevel())
	if s.Err() != nil {
		t.Fatalf("Err() = %v, want nil", s.Err())
	}
	if s.IsOpen("/Users/x/dotfiles", "main") {
		t.Fatal("IsOpen true with no windows open")
	}
}

func TestSnapshotIsOpenMatchesTheWholeCascade(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "dotfiles — main", Path: "/Users/x/dotfiles/.zshrc"},
		{Title: "understory — fix-writeback"},
		{Title: "bar — main", Path: "/Users/x/monorepo/packages/bar/main.go"},
		{Title: "scratchpad"},
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/monorepo", "/Users/x/scratchpad", "/Users/x/worktrees/dotfiles")

	cases := []struct {
		name         string
		path, branch string
		want         bool
	}{
		{"exact title and branch", "/Users/x/dotfiles", "main", true},
		// The nested-path check is branch-agnostic by design (see
		// matchVSCodeWindowNestedPath): a window with a file focused
		// inside this tree counts as open on it, whatever its title's
		// branch component says.
		{"window with a file focused inside counts as open", "/Users/x/dotfiles", "other-branch", true},
		// ...but a *same-named* folder elsewhere (a worktree) on another
		// branch must not claim the main checkout's window: the strict
		// title match rejects the different branch, and the focused file
		// lives in the other tree, so nothing matches.
		{"same-named worktree on a different branch is not open", "/Users/x/worktrees/dotfiles", "fix-x", false},
		{"root fallback for a subdirectory", "/Users/x/dotfiles/sub", "main", true},
		{"nested window by focused file", "/Users/x/monorepo", "", true},
		{"branch-only match, no file focused", "/Users/x/other/understory", "fix-writeback", true},
		{"bare basename, no branch known", "/Users/x/scratchpad", "", true},
		{"nothing open, outside any work tree", "/Users/x/nowhere", "", false},
		{"empty path never matches", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotWith(windows, toplevel).IsOpen(tc.path, tc.branch); got != tc.want {
				t.Fatalf("IsOpen(%q, %q) = %v, want %v", tc.path, tc.branch, got, tc.want)
			}
		})
	}
}

func TestSnapshotMemoizesToplevelLookups(t *testing.T) {
	calls := map[string]int{}
	counting := func(dir string) string {
		calls[dir]++
		return fakeToplevel("/Users/x/repo")(dir)
	}
	windows := []vscodeWindow{{Title: "unrelated", Path: "/Users/x/repo/a/f.go"}}
	s := newVSCodeSnapshot(func() ([]vscodeWindow, error) { return windows, nil }, counting)

	// Two rows under the same root, each missing the title match and
	// falling through to the nested-path check: every directory's
	// work-tree root must be resolved at most once across both calls,
	// or a poll of N rows pays N git subprocesses for the same answer.
	s.IsOpen("/Users/x/repo", "x")
	s.IsOpen("/Users/x/repo/sub", "y")
	for dir, n := range calls {
		if n > 1 {
			t.Fatalf("toplevel(%q) called %d times, want 1", dir, n)
		}
	}
}

// TestSnapshotAgreesWithOpenVSCode is the invariant the dashboards'
// columns are built on: IsOpen says true exactly when OpenVSCode, given
// the same windows and roots, would focus an existing window rather
// than open a new one. Both sides run the real matchers; only the OS
// seams (window listing, toplevel, raise, the code CLI) are faked, and
// faked identically.
func TestSnapshotAgreesWithOpenVSCode(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "dotfiles — main", Path: "/Users/x/dotfiles/.zshrc"},
		{Title: "understory — fix-writeback"},
		{Title: "bar — main", Path: "/Users/x/monorepo/packages/bar/main.go"},
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/monorepo", "/Users/x/worktrees/dotfiles")

	d := defaultDeps()
	d.vscodeWindows = func() ([]vscodeWindow, error) { return windows, nil }
	d.toplevel = toplevel
	d.matchNestedWindow = func(w []vscodeWindow, path string) (string, bool) {
		return matchVSCodeWindowNestedPath(w, path, toplevel)
	}
	d.lookPathCode = func() (string, bool) { return "/usr/local/bin/code", true }
	openedNew := false
	d.runCommand = func(args []string) (bool, string) {
		for _, a := range args {
			if a == "-n" {
				openedNew = true
			}
		}
		return true, ""
	}
	d.raiseWindow = func(title string) (bool, error) { return true, nil }

	snapshot := snapshotWith(windows, toplevel)

	cases := []struct{ path, branch string }{
		{"/Users/x/dotfiles", "main"},
		{"/Users/x/dotfiles", "other-branch"},
		{"/Users/x/dotfiles/sub", "main"},
		{"/Users/x/monorepo", ""},
		{"/Users/x/other/understory", "fix-writeback"},
		{"/Users/x/worktrees/dotfiles", "fix-x"},
		{"/Users/x/nowhere", ""},
	}
	for _, tc := range cases {
		openedNew = false
		result := openVSCode(d, tc.path, tc.branch)
		if !result.OK {
			t.Fatalf("openVSCode(%q, %q) failed: %+v", tc.path, tc.branch, result)
		}
		wouldFocus := !openedNew
		if got := snapshot.IsOpen(tc.path, tc.branch); got != wouldFocus {
			t.Fatalf("IsOpen(%q, %q) = %v, but OpenVSCode wouldFocus = %v", tc.path, tc.branch, got, wouldFocus)
		}
	}
}
