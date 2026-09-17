package mycelium

// This file holds the read-only half of open-or-focus: where OpenVSCode
// answers "is a window already open on this path?" for one path at the
// moment the user asks to go there, VSCodeSnapshot answers it for a
// whole table of paths on every poll, without raising, opening, or
// activating anything. canopy and understory both render a per-row "VS
// Code open?" column from this, so the window listing and the git
// work-tree lookups are paid once per poll rather than once per row.
//
// The window source is one System Events listing of window titles (see
// vscode.go): a poll is a single osascript round-trip plus in-memory
// matches. A poll never activates Code to defeat AX culling the way
// OpenVSCode does (focus isn't moving, so stealing it would be a bug);
// an empty listing while Code runs is reported as "can't tell" instead.

import (
	"errors"
	"os"
)

// errVSCodeListingEmpty is reported by VSCodeSnapshot.Err when VS Code
// is running but System Events listed no windows at all. macOS culls
// the AX tree of a backgrounded app (luiul/dashkit#9), so an empty
// listing can mean "couldn't see", not "nothing open" — and a poll
// can't activate Code to find out (that's OpenVSCode's move, because
// focus is moving there anyway). The snapshot can't tell open from
// closed, so IsOpen is false for every path and callers should render
// their "unknown" cell rather than "not open". A partial cull (one
// window missing from a non-empty listing) is undetectable on the poll
// path; the Enter path self-heals via OpenVSCode's activate-and-relist.
var errVSCodeListingEmpty = errors.New("vscode is running but its window listing is empty (accessibility cull?)")

// VSCodeSnapshot is one poll cycle's view of which VS Code windows are
// open. The window titles are captured once, at construction (see
// SnapshotVSCode), and IsOpen then answers per-path queries against
// that frozen view, memoizing git work-tree-root lookups across calls:
// a poll typically asks about several rows under the same root, and
// each unmemoized miss would shell out to git.
//
// IsOpen runs the exact same match OpenVSCode's already-open check
// runs, so a column built on this says "open" precisely when OpenVSCode
// would focus an existing window rather than open a new one.
type VSCodeSnapshot struct {
	windows  []vscodeWindow
	err      error
	toplevel func(string) string // memoized, see newVSCodeSnapshot
}

// SnapshotVSCode captures the currently open VS Code windows from one
// System Events listing and returns them as a queryable snapshot. It
// never returns a Go error: an unreadable listing is stored and
// reported by Err, so a poll's "can't tell" state is data the caller
// renders, not a control-flow branch it has to remember to take.
func SnapshotVSCode() *VSCodeSnapshot {
	home, _ := os.UserHomeDir()
	return newVSCodeSnapshot(vscodeWindows, home, gitToplevel)
}

// newVSCodeSnapshot builds a snapshot from a window lister and a
// work-tree-root resolver, split out from SnapshotVSCode so tests can
// feed canned titles and roots without touching osascript or git (see
// snapshot_test.go).
func newVSCodeSnapshot(listWindows func() (titles []string, running bool, err error), home string, toplevel func(string) string) *VSCodeSnapshot {
	// Memoize toplevel across IsOpen calls: several rows commonly sit
	// under the same root, and each lookup is otherwise a git
	// subprocess.
	tops := map[string]string{}
	memo := func(dir string) string {
		if t, ok := tops[dir]; ok {
			return t
		}
		t := toplevel(dir)
		tops[dir] = t
		return t
	}
	titles, running, err := listWindows()
	s := &VSCodeSnapshot{toplevel: memo}
	switch {
	case err != nil:
		s.err = err
	case running && len(titles) == 0:
		s.err = errVSCodeListingEmpty
	default:
		// VS Code not running is a definitive nothing-open, not an
		// error: no window exists, culled or otherwise.
		s.windows = parseVSCodeWindows(titles, home)
	}
	return s
}

// Err reports whether reading the window listing failed (see
// SnapshotVSCode). When it did, IsOpen is false for every path: the
// honest answer to "is a window open?" is "can't tell", not "no".
func (s *VSCodeSnapshot) Err() error { return s.err }

// IsOpen reports whether a VS Code window is currently open on path: on
// path itself, on its git work-tree root, or on a subpackage inside its
// tree, matched exactly the way OpenVSCode's already-open check matches
// (see matchVSCodeWindow).
//
// False whenever the snapshot's listing is untrustworthy (Err non-nil)
// or path is "": in both cases there is nothing solid to match on.
func (s *VSCodeSnapshot) IsOpen(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	_, ok := matchVSCodeWindow(s.windows, path, s.toplevel)
	return ok
}

// IsOpenOnWorktree reports whether a VS Code window is currently open ON
// the worktree at path (its root, or a subpackage inside its tree),
// strictly: a pure folder-path match (see matchVSCodeWindowOnWorktree),
// so the phantom-branch class of false positives from the old
// basename-plus-branch title grammar cannot occur.
//
// Use this, not IsOpen, when the answer feeds a destructive-action
// warning (understory's and coppice's remove prompts): IsOpen's
// open-or-focus semantics tolerate false opens (they merely focus a
// window), while a false open on a deletion prompt cries wolf.
//
// Same closed failure as IsOpen: false when the snapshot's listing is
// untrustworthy (Err non-nil) or path is "".
func (s *VSCodeSnapshot) IsOpenOnWorktree(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	return matchVSCodeWindowOnWorktree(s.windows, path)
}
