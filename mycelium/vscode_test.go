package mycelium

import (
	"slices"
	"testing"
)

func TestParseVSCodeWindowList(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantTitles  []string
		wantRunning bool
	}{
		{"not running", "0\x1f", nil, false},
		{"running with no windows", "1\x1f", nil, true},
		{"one window", "1\x1f~/dotfiles — main\x1e", []string{"~/dotfiles — main"}, true},
		{
			"several windows",
			"1\x1f~/dotfiles — main\x1e~/canopy — main\x1e",
			[]string{"~/dotfiles — main", "~/canopy — main"},
			true,
		},
		// runOsascript trims the whole output, not each record.
		{"trailing newline tolerated", "1\x1f~/dotfiles — main\x1e\r\n", []string{"~/dotfiles — main"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			titles, running := parseVSCodeWindowList(tc.raw)
			if running != tc.wantRunning {
				t.Fatalf("running = %v, want %v", running, tc.wantRunning)
			}
			if !slices.Equal(titles, tc.wantTitles) {
				t.Fatalf("titles = %q, want %q", titles, tc.wantTitles)
			}
		})
	}
}

func TestWindowTitlePath(t *testing.T) {
	const home = "/Users/x"
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"path and branch", "~/dotfiles — main", "/Users/x/dotfiles"},
		{"path with spaces", "~/projects/my repo — feat/x", "/Users/x/projects/my repo"},
		{"absolute path unexpanded", "/opt/repo — main", "/opt/repo"},
		// A branch component that never rendered (repo still resolving):
		// the path is still everything before the separator.
		{"trailing separator, no branch", "~/dotfiles — ", "/Users/x/dotfiles"},
		{"no separator at all", "~/dotfiles", "/Users/x/dotfiles"},
		{"home itself", "~ — main", "/Users/x"},
		// Only the first separator splits: the branch component may
		// carry dashes of its own.
		{"branch with dashes", "~/dotfiles — patch/ISA-1 — wip", "/Users/x/dotfiles"},
		// Multi-root windows render the workspace file path; they match
		// on that path only (accepted limitation).
		{"workspace file", "~/tardis-community.code-workspace — main", "/Users/x/tardis-community.code-workspace"},
		// No parseable path: never matches.
		{"no-folder window", "", ""},
		{"foreign format", "Welcome — Visual Studio Code", ""},
		{"tilde-user form is not expanded", "~root/repo — main", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowTitlePath(tc.title, home); got != tc.want {
				t.Fatalf("windowTitlePath(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestParseVSCodeWindowsDropsTitlesWithoutAPath(t *testing.T) {
	titles := []string{"~/dotfiles — main", "", "Welcome — Visual Studio Code"}
	windows := parseVSCodeWindows(titles, "/Users/x")
	if len(windows) != 1 {
		t.Fatalf("got %d windows, want 1: %+v", len(windows), windows)
	}
	if windows[0].Title != "~/dotfiles — main" || windows[0].Path != "/Users/x/dotfiles" {
		t.Fatalf("got %+v", windows[0])
	}
}

// TestMatchVSCodeWindowStages covers the three cascade stages
// (exact folder, work-tree root, nested on a path-element boundary)
// plus the boundary rule.
func TestMatchVSCodeWindowStages(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "~/dotfiles — main", Path: "/Users/x/dotfiles"},
		{Title: "~/monorepo/packages/bar — main", Path: "/Users/x/monorepo/packages/bar"},
		{Title: "~/worktrees/wt-a — feat", Path: "/Users/x/worktrees/wt-a"},
	}
	toplevel := fakeToplevel("/Users/x/dotfiles", "/Users/x/monorepo", "/Users/x/worktrees/wt-a")

	cases := []struct {
		name string
		path string
		want string // "" means no match
	}{
		{"exact folder", "/Users/x/dotfiles", "~/dotfiles — main"},
		{"work-tree root for a subdirectory", "/Users/x/dotfiles/sub", "~/dotfiles — main"},
		{"window nested inside the path", "/Users/x/monorepo", "~/monorepo/packages/bar — main"},
		{"nothing open", "/Users/x/nowhere", ""},
		// "/wt-a-b" shares a prefix with the "/wt-a" window but not a
		// path element, so it is not "inside".
		{"prefix sibling is not nested", "/Users/x/worktrees/wt-a-b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := matchVSCodeWindow(windows, tc.path, toplevel)
			if tc.want == "" {
				if found {
					t.Fatalf("matchVSCodeWindow(%q) = %q, want no match", tc.path, got)
				}
				return
			}
			if !found || got != tc.want {
				t.Fatalf("matchVSCodeWindow(%q) = %q, %v, want %q", tc.path, got, found, tc.want)
			}
		})
	}
}

// TestMatchVSCodeWindowPrefersExactOverNested: a window on path itself
// always wins over one merely scoped somewhere inside path.
func TestMatchVSCodeWindowPrefersExactOverNested(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "nested", Path: "/Users/x/monorepo/packages/bar"},
		{Title: "exact", Path: "/Users/x/monorepo"},
	}
	got, found := matchVSCodeWindow(windows, "/Users/x/monorepo", fakeToplevel("/Users/x/monorepo"))
	if !found || got != "exact" {
		t.Fatalf("got %q, %v, want the exact window", got, found)
	}
}

func TestMatchVSCodeWindowOnWorktreeIsStrict(t *testing.T) {
	windows := []vscodeWindow{
		{Title: "main checkout", Path: "/Users/x/dotfiles"},
		{Title: "subpackage", Path: "/Users/x/worktrees/understory/pkg"},
		{Title: "worktree window", Path: "/Users/x/worktrees/canopy"},
		{Title: "sibling", Path: "/Users/x/worktrees/canopy-sibling"},
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"window on the worktree root", "/Users/x/worktrees/canopy", true},
		{"window on a subpackage inside the worktree", "/Users/x/worktrees/understory", true},
		// The main checkout window does not make deleting the worktree
		// warn, whatever branch its SCM view shows: the branch component
		// is never matched.
		{"main checkout window is not the worktree", "/Users/x/worktrees/dotfiles", false},
		{"sibling prefix is not inside", "/Users/x/worktrees/canopy-sibling-x", false},
		{"unrelated path", "/Users/x/nowhere", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchVSCodeWindowOnWorktree(windows, tc.path); got != tc.want {
				t.Fatalf("matchVSCodeWindowOnWorktree(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
