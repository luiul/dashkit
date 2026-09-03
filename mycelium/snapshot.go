package mycelium

// This file holds the read-only half of open-or-focus: where OpenVSCode
// answers "is a window already open on this path?" for one path at the
// moment the user asks to go there, VSCodeSnapshot answers it for a
// whole table of paths on every poll, without raising, opening, or
// activating anything. canopy and understory both render a per-row "VS
// Code open?" column from this, so the listing cost (one AppleScript
// round-trip) and the git work-tree lookups are paid once per poll
// rather than once per row.

// VSCodeSnapshot is one poll cycle's view of which VS Code windows are
// open. The window listing is captured once, at construction (see
// SnapshotVSCode), and IsOpen then answers per-path queries against
// that frozen view, memoizing git work-tree-root lookups across calls:
// a poll typically asks about several rows under the same root, and
// each unmemoized miss would shell out to git.
//
// IsOpen runs the exact same match cascade OpenVSCode's already-open
// check runs (see findWindow), so a column built on this says "open"
// precisely when OpenVSCode would focus an existing window rather than
// open a new one.
type VSCodeSnapshot struct {
	windows []vscodeWindow
	// err is the window listing's failure, if any (most likely the
	// macOS Automation permission, see vscodeWindows' doc): the snapshot
	// can't tell open from closed, so IsOpen is false for every path
	// and callers should render their "unknown" cell rather than "not
	// open". VS Code simply not running is NOT an error, just an empty
	// listing.
	err  error
	deps deps
}

// SnapshotVSCode lists the currently open VS Code windows (see
// vscodeWindows) and returns them as a queryable snapshot. It never
// returns a Go error: a listing failure is stored and reported by Err,
// so a poll's "can't tell" state is data the caller renders, not a
// control-flow branch it has to remember to take.
func SnapshotVSCode() *VSCodeSnapshot {
	return newVSCodeSnapshot(vscodeWindows, gitToplevel)
}

// newVSCodeSnapshot builds a snapshot from a window-lister and a
// work-tree-root resolver, split out from SnapshotVSCode so tests can
// feed canned windows and roots without osascript or git (see
// snapshot_test.go). listWindows' error is stored verbatim.
func newVSCodeSnapshot(listWindows func() ([]vscodeWindow, error), toplevel func(string) string) *VSCodeSnapshot {
	windows, err := listWindows()
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
	d := defaultDeps()
	d.toplevel = memo
	d.matchNestedWindow = func(windows []vscodeWindow, path string) (string, bool) {
		return matchVSCodeWindowNestedPath(windows, path, memo)
	}
	return &VSCodeSnapshot{windows: windows, err: err, deps: d}
}

// Err reports whether listing the open windows failed (see
// SnapshotVSCode). When it did, IsOpen is false for every path: the
// honest answer to "is a window open?" is "can't tell", not "no".
func (s *VSCodeSnapshot) Err() error { return s.err }

// IsOpen reports whether a VS Code window is currently open on path: on
// path itself, on its git work-tree root, or on a subpackage inside its
// tree, matched exactly the way OpenVSCode's already-open check matches
// (see findWindow). branch is the branch path is expected to be on, or
// "" when unknown, with the same meaning and the same same-named-
// worktree disambiguation as OpenVSCode's branch parameter.
//
// False whenever the snapshot failed to list windows (Err non-nil) or
// path is "": in both cases there is nothing solid to match on.
func (s *VSCodeSnapshot) IsOpen(path, branch string) bool {
	if s.err != nil || path == "" {
		return false
	}
	_, ok := findWindow(s.deps, s.windows, path, branch)
	return ok
}
