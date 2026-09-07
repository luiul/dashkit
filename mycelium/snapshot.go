package mycelium

// This file holds the read-only half of open-or-focus: where OpenVSCode
// answers "is a window already open on this path?" for one path at the
// moment the user asks to go there, VSCodeSnapshot answers it for a
// whole table of paths on every poll, without raising, opening, or
// activating anything. canopy and understory both render a per-row "VS
// Code open?" column from this, so the registry read and the git
// work-tree lookups are paid once per poll rather than once per row.
//
// The window source is the registry (see registry.go): a poll is a
// directory read plus small JSON parses, with no AppleScript round-trip
// at all.

import (
	"errors"
	"time"
)

// errRegistryUnavailable is reported by VSCodeSnapshot.Err when the
// registry directory cannot be read (the extension is not installed,
// most likely): the snapshot can't tell open from closed, so IsOpen is
// false for every path and callers should render their "unknown" cell
// rather than "not open".
var errRegistryUnavailable = errors.New("vscode window registry unavailable")

// VSCodeSnapshot is one poll cycle's view of which VS Code windows are
// open. The registry entries are captured once, at construction (see
// SnapshotVSCode), and IsOpen then answers per-path queries against
// that frozen view, memoizing git work-tree-root lookups across calls:
// a poll typically asks about several rows under the same root, and
// each unmemoized miss would shell out to git.
//
// IsOpen runs the exact same match OpenVSCode's already-open check
// runs, so a column built on this says "open" precisely when OpenVSCode
// would focus an existing window rather than open a new one.
type VSCodeSnapshot struct {
	registry []registryEntry
	err      error
	toplevel func(string) string // memoized, see SnapshotVSCode
}

// SnapshotVSCode captures the currently open VS Code windows from the
// window registry and returns them as a queryable snapshot. It never
// returns a Go error: an unreadable registry is stored and reported by
// Err, so a poll's "can't tell" state is data the caller renders, not a
// control-flow branch it has to remember to take.
func SnapshotVSCode() *VSCodeSnapshot {
	return newVSCodeSnapshot(func() ([]registryEntry, bool) {
		return readRegistry(registryDir(), time.Now())
	}, gitToplevel)
}

// newVSCodeSnapshot builds a snapshot from a registry reader and a
// work-tree-root resolver, split out from SnapshotVSCode so tests can
// feed canned entries and roots without touching the filesystem or git
// (see snapshot_test.go).
func newVSCodeSnapshot(readReg func() ([]registryEntry, bool), toplevel func(string) string) *VSCodeSnapshot {
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
	entries, ok := readReg()
	s := &VSCodeSnapshot{toplevel: memo}
	if !ok {
		s.err = errRegistryUnavailable
		return s
	}
	s.registry = entries
	return s
}

// Err reports whether reading the window registry failed (see
// SnapshotVSCode). When it did, IsOpen is false for every path: the
// honest answer to "is a window open?" is "can't tell", not "no".
func (s *VSCodeSnapshot) Err() error { return s.err }

// IsOpen reports whether a VS Code window is currently open on path: on
// path itself, on its git work-tree root, or on a subpackage inside its
// tree, matched exactly the way OpenVSCode's already-open check matches
// (see matchRegistry).
//
// False whenever the snapshot failed to read the registry (Err non-nil)
// or path is "": in both cases there is nothing solid to match on.
func (s *VSCodeSnapshot) IsOpen(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	_, ok := matchRegistry(s.registry, path, s.toplevel)
	return ok
}

// IsOpenOnWorktree reports whether a VS Code window is currently open ON
// the worktree at path (its root, or a subpackage inside its tree),
// strictly: a pure folder-path match (see matchRegistryOnWorktree), so
// the phantom-branch class of false positives from the title-matching
// era cannot occur.
//
// Use this, not IsOpen, when the answer feeds a destructive-action
// warning (understory's and coppice's remove prompts): IsOpen's
// open-or-focus semantics tolerate false opens (they merely focus a
// window), while a false open on a deletion prompt cries wolf.
//
// Same closed failure as IsOpen: false when the snapshot failed to read
// the registry (Err non-nil) or path is "".
func (s *VSCodeSnapshot) IsOpenOnWorktree(path string) bool {
	if s.err != nil || path == "" {
		return false
	}
	return matchRegistryOnWorktree(s.registry, path)
}
