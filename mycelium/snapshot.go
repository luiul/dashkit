package mycelium

// This file holds the read-only half of open-or-focus: where OpenVSCode
// and OpenGhostty answer "is a window already open on this path?" for
// one path at the moment the user asks to go there, the snapshots
// answer it for a whole table of paths on every poll, without raising,
// opening, or activating anything. understory renders a per-row
// "Ghostty open?" column from this (SnapshotGhostty), so the window
// listing and the git work-tree lookups are paid once per poll rather
// than once per row.
//
// The window source is one System Events listing of window titles per
// app (see vscode.go and ghostty.go): a poll is one osascript round-trip
// per editor plus in-memory matches. A poll never activates an app to
// defeat AX culling the way the Open* functions do (focus isn't moving,
// so stealing it would be a bug); an empty listing while the app runs
// is reported as "can't tell" instead.

import (
	"errors"
	"os"
)

// errListingEmpty is reported by a snapshot's Err when an app is
// running but System Events listed no windows at all for it. macOS
// culls the AX tree of a backgrounded app (luiul/dashkit#9), so an
// empty listing can mean "couldn't see", not "nothing open" — and a
// poll can't activate the app to find out (that's the Open* functions'
// move, because focus is moving there anyway). The snapshot can't tell
// open from closed, so IsOpen is false for every path and callers
// should render their "unknown" cell rather than "not open". A partial
// cull (one window missing from a non-empty listing) is undetectable
// on the poll path; the Enter path self-heals via the Open* functions'
// activate-and-relist.
func errListingEmpty(app string) error {
	return errors.New(app + " is running but its window listing is empty (accessibility cull?)")
}

// WindowSnapshot is one poll cycle's view of one app's open windows: a
// VS Code listing or a Ghostty listing, whichever lister and title
// parser it was built with (see SnapshotVSCode, SnapshotEditor). The
// window titles are captured once, at construction, and IsOpen then
// answers per-path queries against that frozen view, memoizing git
// work-tree-root lookups across calls: a poll typically asks about
// several rows under the same root, and each unmemoized miss would
// shell out to git.
//
// IsOpen runs the exact same match OpenVSCode's and OpenGhostty's
// already-open checks run, so a column built on this says "open"
// precisely when the matching Open* function would focus an existing
// window rather than open a new one.
type WindowSnapshot struct {
	windows  []vscodeWindow
	err      error
	toplevel func(string) string // memoized, see newWindowSnapshot
}

// SnapshotVSCode captures the currently open VS Code windows from one
// System Events listing and returns them as a queryable snapshot. It
// never returns a Go error: an unreadable listing is stored and
// reported by Err, so a poll's "can't tell" state is data the caller
// renders, not a control-flow branch it has to remember to take.
func SnapshotVSCode() *WindowSnapshot {
	home, _ := os.UserHomeDir()
	return newWindowSnapshot(vscodeWindows, parseVSCodeWindows, "vscode", home, gitToplevel)
}

// SnapshotGhostty captures the currently open Ghostty windows the same
// way, parsed with the Ghostty title grammar (see ghostty.go). Same
// error contract as SnapshotVSCode.
func SnapshotGhostty() *WindowSnapshot {
	home, _ := os.UserHomeDir()
	return newWindowSnapshot(ghosttyWindows, parseGhosttyWindows, "ghostty", home, gitToplevel)
}

// newWindowSnapshot builds a snapshot from a window lister, a title
// parser, and a work-tree-root resolver, split out from SnapshotVSCode
// so tests can feed canned titles and roots without touching osascript
// or git (see snapshot_test.go).
func newWindowSnapshot(listWindows func() (titles []string, running bool, err error), parse func([]string, string) []vscodeWindow, app, home string, toplevel func(string) string) *WindowSnapshot {
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
	s := &WindowSnapshot{toplevel: memo}
	switch {
	case err != nil:
		s.err = err
	case running && len(titles) == 0:
		s.err = errListingEmpty(app)
	default:
		// The app not running is a definitive nothing-open, not an
		// error: no window exists, culled or otherwise.
		s.windows = parse(titles, home)
	}
	return s
}

// Err reports whether reading the window listing failed (see
// SnapshotVSCode). When it did, IsOpen is false for every path: the
// honest answer to "is a window open?" is "can't tell", not "no".
func (s *WindowSnapshot) Err() error { return s.err }

// IsOpen reports whether a window of this snapshot's app is currently
// open on path: on path itself, on its git work-tree root, or on a
// subpackage inside its tree, matched exactly the way the matching
// Open* function's already-open check matches (see matchVSCodeWindow).
//
// False whenever the snapshot's listing is untrustworthy (Err non-nil)
// or path is "": in both cases there is nothing solid to match on.
func (s *WindowSnapshot) IsOpen(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	_, ok := matchVSCodeWindow(s.windows, path, s.toplevel)
	return ok
}

// IsOpenOnWorktree reports whether a window of this snapshot's app is
// currently open ON
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
func (s *WindowSnapshot) IsOpenOnWorktree(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	return matchVSCodeWindowOnWorktree(s.windows, path)
}
