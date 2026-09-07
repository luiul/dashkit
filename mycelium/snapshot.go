package mycelium

// This file holds the read-only half of open-or-focus: where OpenVSCode
// answers "is a window already open on this path?" for one path at the
// moment the user asks to go there, VSCodeSnapshot answers it for a
// whole table of paths on every poll, without raising, opening, or
// activating anything. canopy and understory both render a per-row "VS
// Code open?" column from this, so the listing cost and the git
// work-tree lookups are paid once per poll rather than once per row.
//
// The window source is the registry first (see registry.go): a poll is
// then a directory read plus small JSON parses, with no AppleScript
// round-trip at all. Only when the registry cannot answer (extension
// not installed, no fresh entries) does a snapshot fall back to listing
// windows over AppleScript, the pre-registry behavior. Fallback uses
// are NOT logged here, unlike OpenVSCode: a poll runs every few
// seconds, so logging them would bury the actionable signal (fallbacks
// on user actions) under thousands of "VS Code is closed" lines.

// VSCodeSnapshot is one poll cycle's view of which VS Code windows are
// open. The window source is captured once, at construction (see
// SnapshotVSCode), and IsOpen then answers per-path queries against
// that frozen view, memoizing git work-tree-root lookups across calls:
// a poll typically asks about several rows under the same root, and
// each unmemoized miss would shell out to git.
//
// IsOpen runs the exact same match OpenVSCode's already-open check
// runs (registry path match, or the findWindow cascade on fallback), so
// a column built on this says "open" precisely when OpenVSCode would
// focus an existing window rather than open a new one.
type VSCodeSnapshot struct {
	// registry holds the fresh registry entries when the registry
	// answered this poll, nil otherwise. A non-nil registry is the whole
	// window source: windows stays nil and the AppleScript matchers never
	// run.
	registry []registryEntry
	windows  []vscodeWindow
	// err is the window listing's failure, if any (most likely the
	// macOS Automation permission, see vscodeWindows' doc): the snapshot
	// can't tell open from closed, so IsOpen is false for every path
	// and callers should render their "unknown" cell rather than "not
	// open". VS Code simply not running is NOT an error, just an empty
	// listing. Always nil when the registry answered: reading a
	// directory of JSON files has no permission prompt to fail on.
	err  error
	deps deps
}

// SnapshotVSCode captures the currently open VS Code windows (from the
// window registry when it has fresh entries, else from an AppleScript
// listing, see vscodeWindows) and returns them as a queryable snapshot.
// It never returns a Go error: a listing failure is stored and reported
// by Err, so a poll's "can't tell" state is data the caller renders,
// not a control-flow branch it has to remember to take.
func SnapshotVSCode() *VSCodeSnapshot {
	return newVSCodeSnapshot(vscodeWindows, gitToplevel)
}

// newVSCodeSnapshot builds a snapshot from a window-lister and a
// work-tree-root resolver, split out from SnapshotVSCode so tests can
// feed canned windows and roots without osascript or git (see
// snapshot_test.go). listWindows' error is stored verbatim. The
// registry read goes through deps so tests can feed canned entries.
func newVSCodeSnapshot(listWindows func() ([]vscodeWindow, error), toplevel func(string) string) *VSCodeSnapshot {
	d := defaultDeps()
	return newVSCodeSnapshotWithDeps(d, listWindows, toplevel)
}

// newVSCodeSnapshotWithDeps is newVSCodeSnapshot with the deps seam
// exposed, so registry-path tests can feed canned entries (see
// snapshot_test.go).
func newVSCodeSnapshotWithDeps(d deps, listWindows func() ([]vscodeWindow, error), toplevel func(string) string) *VSCodeSnapshot {
	// Memoize toplevel across IsOpen calls: several rows commonly sit
	// under the same root, and matchVSCodeWindowNestedPath additionally
	// resolves one root per window's focused-file directory, which
	// repeats heavily across rows when windows focus files in the same
	// directories. Each lookup is otherwise a git subprocess.
	tops := map[string]string{}
	memo := func(dir string) string {
		if t, ok := tops[dir]; ok {
			return t
		}
		t := toplevel(dir)
		tops[dir] = t
		return t
	}
	d.toplevel = memo
	d.matchNestedWindow = func(windows []vscodeWindow, path string) (string, bool) {
		return matchVSCodeWindowNestedPath(windows, path, memo)
	}
	if entries, ok := d.readRegistry(); ok && len(entries) > 0 {
		return &VSCodeSnapshot{registry: entries, deps: d}
	}
	windows, err := listWindows()
	return &VSCodeSnapshot{windows: windows, err: err, deps: d}
}

// Err reports whether listing the open windows failed (see
// SnapshotVSCode). When it did, IsOpen is false for every path: the
// honest answer to "is a window open?" is "can't tell", not "no".
func (s *VSCodeSnapshot) Err() error { return s.err }

// IsOpen reports whether a VS Code window is currently open on path: on
// path itself, on its git work-tree root, or on a subpackage inside its
// tree, matched exactly the way OpenVSCode's already-open check matches
// (registry path match, or the findWindow cascade on fallback). branch
// is the branch path is expected to be on, or "" when unknown. It is
// advisory: the registry matches on folder paths alone, and only the
// title fallback uses the branch.
//
// False whenever the snapshot failed to list windows (Err non-nil) or
// path is "": in both cases there is nothing solid to match on.
func (s *VSCodeSnapshot) IsOpen(path, branch string) bool {
	if s.err != nil || path == "" {
		return false
	}
	if s.registry != nil {
		_, ok := matchRegistry(s.registry, path, s.deps.toplevel)
		return ok
	}
	_, ok := findWindow(s.deps, s.windows, path, branch)
	return ok
}

// IsOpenOnWorktree reports whether a VS Code window is currently open ON
// the worktree at path (its root, or a subpackage inside its tree),
// strictly. On the registry path this is a pure folder-path match (see
// matchRegistryOnWorktree): no branch, no title, so the phantom-branch
// class of false positives cannot occur. On the title fallback the
// title must name path's basename AND branch (see
// matchVSCodeWindowTitleStrict for why the branchless weak fallback
// IsOpen inherits is dropped here), or a window's focused file must
// live inside path's work tree (matchVSCodeWindowNestedPath — a window
// scoped to a subpackage of the worktree is stranded by its removal
// too). The branch-only fallback findWindow ends with is NOT consulted:
// it answers "is this branch visible in some window", not "would
// deleting this worktree strand a window".
//
// Use this, not IsOpen, when the answer feeds a destructive-action
// warning (understory's and coppice's remove prompts): IsOpen's
// open-or-focus semantics tolerate false opens (they merely focus a
// window), while a false open on a deletion prompt cries wolf.
//
// Same closed failure as IsOpen: false when the snapshot failed to list
// windows (Err non-nil) or path is "". On the title fallback, without a
// branch, only the nested-path match can still fire.
func (s *VSCodeSnapshot) IsOpenOnWorktree(path, branch string) bool {
	if s.err != nil || path == "" {
		return false
	}
	if s.registry != nil {
		return matchRegistryOnWorktree(s.registry, path)
	}
	titles := make([]string, len(s.windows))
	for i, w := range s.windows {
		titles[i] = w.Title
	}
	if _, ok := matchVSCodeWindowTitleStrict(titles, path, branch); ok {
		return true
	}
	_, ok := s.deps.matchNestedWindow(s.windows, path)
	return ok
}
