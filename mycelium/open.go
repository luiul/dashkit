// Package mycelium opens, or focuses if one is already open, an app
// window (VS Code or a Ghostty terminal) on a given filesystem path,
// without ever risking a duplicate window for a path that's already
// open somewhere.
//
// For VS Code, window identity is the window title: the dotfiles
// `window.title` setting renders each title as the opened folder's full
// path plus the branch (which is never matched), so "is a window
// already open on this path?" is a folder-path match over one System
// Events listing, and focusing is an AXRaise of the exact window whose
// title matched (see vscode.go). Identification and focus stay bound to
// the same window, so nothing re-runs its own matching in between —
// `code --reuse-window` does exactly that inside the CLI and hijacks
// the last-active window on any disagreement, so it is never called:
// the CLI is only ever asked to open a genuinely new window.
//
// Ghostty gets the same title-based identity (see ghostty.go): the
// dotfiles zsh title hook and the session-title pi extension render
// each surface's title as the git work-tree root plus the branch, the
// same grammar as VS Code's window.title, so the same listing, match
// stages, and AXRaise apply. The branch component is never matched
// there either.
//
// The "already open" check isn't limited to a window scoped to the
// exact path either. path can sit *inside* a checkout rather than at
// its root (canopy hands over the agent's cwd as-is, e.g. a monorepo
// package the agent runs in), so when nothing is open on path itself
// the checkout's work-tree root gets a second exact-path match before
// anything weaker runs: a window open on the root is scoped to the
// exact tree path lives in, not merely somewhere inside it. And a
// window open on a folder nested inside path (a monorepo subpackage
// opened directly as its own window) is reused too rather than opening
// a redundant new one alongside it. See matchVSCodeWindow for the three
// stages.
//
// This is the underground layer shared by canopy (jump to whichever
// window is actually running a given agent) and understory (open or
// focus a worktree on Enter): both need the exact same "is a window
// already open on this path? raise it — otherwise open a genuinely new
// one" behavior, backed by the same window detection, so it lives here
// once instead of being duplicated in both trees.
package mycelium

import (
	"os"
	"os/exec"
	"strings"
)

// Result reports whether opening/focusing a window succeeded, and a
// human-readable message about what happened, meant to be shown
// straight to a user (e.g. as a TUI notification).
type Result struct {
	OK      bool
	Message string
}

// deps groups every external side effect OpenVSCode/OpenGhostty/
// OpenEditor make, so tests can swap each one out (see open_test.go)
// without touching the real OS.
type deps struct {
	lookPathCode         func() (string, bool)
	runCommand           func(args []string) (exitOK bool, stderr string)
	vscodeWindows        func() (titles []string, running bool, err error)
	raiseWindow          func(title string) (bool, error)
	activateCode         func() error
	ghosttyWindows       func() (titles []string, running bool, err error)
	raiseGhosttyWindow   func(title string) (bool, error)
	activateGhostty      func() error
	home                 func() string
	toplevel             func(dir string) string
	ghosttyOpenNewWindow func(cwd string) error
}

func defaultDeps() deps {
	return deps{
		lookPathCode: func() (string, bool) {
			p, err := exec.LookPath("code")
			return p, err == nil
		},
		runCommand: func(args []string) (bool, string) {
			cmd := exec.Command(args[0], args[1:]...)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			err := cmd.Run()
			return err == nil, strings.TrimSpace(stderr.String())
		},
		vscodeWindows: vscodeWindows,
		raiseWindow:   vscodeRaiseWindow,
		activateCode:  vscodeActivateCode,
		home: func() string {
			h, _ := os.UserHomeDir()
			return h
		},
		toplevel:             gitToplevel,
		ghosttyWindows:       ghosttyWindows,
		raiseGhosttyWindow:   ghosttyRaiseWindow,
		activateGhostty:      ghosttyActivate,
		ghosttyOpenNewWindow: ghosttyOpenNewWindow,
	}
}

// OpenVSCode opens, or focuses if a window is already open on path, a VS
// Code window there, using the real OS.
//
// `code --reuse-window <path>` alone isn't enough to get real
// switch-or-create behavior out of the `code` CLI: it only reuses the
// right window when one already has that exact folder open, and
// silently hijacks whichever window was last active otherwise, rather
// than opening a fresh one — confirmed both empirically and in upstream
// reports (microsoft/vscode#121926, #216602, #215749). OpenVSCode
// checks for an already-open window itself first, by matching folder
// paths parsed out of window titles (see vscode.go), and focuses the
// matched window directly with AXRaise: the focus action binds to the
// identified window, with no CLI re-matching in between, so the hijack
// class is impossible. The CLI is only ever asked to open, never to
// reuse: once "no window is open on path" is established, `code -n
// path` forces a genuinely new window. That makes it safe to call
// repeatedly on the same never-before-seen path: the already-open check
// finds the window OpenVSCode itself just created on every subsequent
// call, so nothing stacks up duplicate windows.
//
// path can be a subdirectory of a checkout rather than its root (canopy
// passes the agent's cwd as-is, e.g. a monorepo package the agent runs
// in): when no window is open on path itself, the work-tree root gets a
// second exact-path match, so a window open on the checkout as a whole
// is still reused rather than a redundant new one opened next to it.
//
// If no window is open on path or its work-tree root, OpenVSCode also
// checks for one open somewhere *inside* path before giving up and
// opening a new window there — e.g. pressing Enter on a monorepo
// worktree's root reuses a window already open on one of its
// subpackages, rather than opening a second, redundant window on the
// same tree.
//
// One listing can lie: macOS culls the AX tree of a backgrounded app,
// so System Events can report fewer windows than exist
// (luiul/dashkit#9). When VS Code is running but the listing comes back
// empty or matchless, OpenVSCode activates Code (which re-materializes
// the AX tree, and focus is moving to Code anyway) and re-lists once
// before concluding a new window is needed.
func OpenVSCode(path string) Result {
	return openVSCode(defaultDeps(), path)
}

// findVSCodeWindow returns the exact title of an open VS Code window
// matched on path, strongest signal first (see matchVSCodeWindow for
// the stages). A matchless listing while VS Code runs isn't believed:
// macOS culls the AX tree of a backgrounded app, so Code is activated
// (which re-materializes the tree; focus is moving toward Code anyway
// when a window is about to be raised) and the listing re-run once
// before concluding nothing matches.
func findVSCodeWindow(d deps, path string) (string, bool, error) {
	match := func(titles []string) (string, bool) {
		return matchVSCodeWindow(parseVSCodeWindows(titles, d.home()), path, d.toplevel)
	}

	titles, running, err := d.vscodeWindows()
	if err != nil {
		return "", false, err
	}
	if title, found := match(titles); found {
		return title, true, nil
	}

	if running {
		_ = d.activateCode()
		titles, _, err = d.vscodeWindows()
		if err != nil {
			return "", false, err
		}
		if title, found := match(titles); found {
			return title, true, nil
		}
	}
	return "", false, nil
}

func openVSCode(d deps, path string) Result {
	if path == "" {
		return Result{false, "No known path to open."}
	}

	title, found, err := findVSCodeWindow(d, path)
	if err != nil {
		// Can't tell what's open (the Automation permission for
		// scripting System Events hasn't been granted, most likely):
		// opening blind could stack the duplicate window this library
		// exists to prevent, so fail with the actionable message
		// instead.
		return Result{false, err.Error()}
	}
	if found {
		return focusVSCodeWindow(d, title, path)
	}

	return openNewVSCodeWindow(d, path)
}

// focusVSCodeWindow raises the matched window by its exact title (see
// vscodeRaiseWindow). A window that vanishes between the listing and
// the raise falls through to a genuinely new window, same as a clean
// miss. A raise *error* (the AX call itself failed) is surfaced rather
// than falling through: the window is known to be open, so opening a
// new one would stack the duplicate this library exists to prevent.
func focusVSCodeWindow(d deps, title, path string) Result {
	raised, err := d.raiseWindow(title)
	if err != nil {
		return Result{false, err.Error()}
	}
	if !raised {
		return openNewVSCodeWindow(d, path)
	}
	return Result{true, "Focused VS Code window for " + path + "."}
}

// openNewVSCodeWindow opens a genuinely new window on path (`-n`),
// never `--reuse-window`: the already-open check above just ruled out
// every existing window, and `--reuse-window` on a miss hijacks the
// last-active window instead of opening a fresh one (see OpenVSCode's
// doc). That call is the bug this package exists to avoid, so it is
// never made.
func openNewVSCodeWindow(d deps, path string) Result {
	if codeBin, ok := d.lookPathCode(); ok {
		if exitOK, _ := d.runCommand([]string{codeBin, "-n", path}); exitOK {
			return Result{true, "Opened a new VS Code window for " + path + "."}
		}
	}
	return openVSCodeAppFallback(d, path)
}

// openVSCodeAppFallback is the last resort when the `code` shell command
// isn't installed or failed: raise the app with the path. This can't
// target the right *window*, only the app.
func openVSCodeAppFallback(d deps, path string) Result {
	exitOK, stderr := d.runCommand([]string{"open", "-a", "Visual Studio Code", path})
	if exitOK {
		return Result{true, "Opened " + path + " in VS Code (install the 'code' CLI for exact-window focus)."}
	}
	if stderr == "" {
		stderr = "Couldn't open VS Code."
	}
	return Result{false, stderr}
}

// OpenGhostty focuses a Ghostty window whose title names path (the
// path itself, its git work-tree root, or a folder inside its tree —
// the same match stages OpenVSCode runs, see matchVSCodeWindow), or
// opens a brand-new window there when nothing open matches (e.g. that
// window has since been closed).
//
// tty is the exact surface identity when the caller knows it (canopy
// does: it resolves every agent's tty via ps): a window whose title
// carries that tty wins over same-path windows, so two agents side by
// side in the same location each jump to their own window. Pass "" for
// path-only matching (understory's rows are worktrees, not agent
// processes, so they have no tty); a tty that matches no window
// falls back to the path match too.
func OpenGhostty(tty, path string) Result {
	return openGhostty(defaultDeps(), tty, path)
}

func openGhostty(d deps, tty, path string) Result {
	if path == "" {
		return Result{false, "No known path to focus."}
	}

	title, found, err := findGhosttyWindow(d, tty, path)
	if err != nil {
		return Result{false, err.Error()}
	}
	if found {
		return focusGhosttyWindow(d, title, path)
	}
	return openNewGhosttyWindow(d, path)
}

// findGhosttyWindow is Ghostty's findVSCodeWindow: the exact title of
// an open Ghostty window matched on path, with the same
// activate-and-relist once against an AX-culled matchless listing
// while Ghostty runs (luiul/dashkit#9).
func findGhosttyWindow(d deps, tty, path string) (string, bool, error) {
	match := func(titles []string) (string, bool) {
		return matchGhosttyWindow(parseGhosttyWindows(titles, d.home()), path, d.toplevel, tty)
	}

	titles, running, err := d.ghosttyWindows()
	if err != nil {
		return "", false, err
	}
	if title, found := match(titles); found {
		return title, true, nil
	}

	if running {
		_ = d.activateGhostty()
		titles, _, err = d.ghosttyWindows()
		if err != nil {
			return "", false, err
		}
		if title, found := match(titles); found {
			return title, true, nil
		}
	}
	return "", false, nil
}

// focusGhosttyWindow raises the matched window by its exact title. A
// window that vanishes between the listing and the raise falls through
// to a genuinely new window, same as a clean miss. A raise *error*
// (the AX call itself failed) is surfaced rather than falling through:
// the window is known to be open, so opening a new one would stack the
// duplicate this library exists to prevent.
func focusGhosttyWindow(d deps, title, path string) Result {
	raised, err := d.raiseGhosttyWindow(title)
	if err != nil {
		return Result{false, err.Error()}
	}
	if !raised {
		return openNewGhosttyWindow(d, path)
	}
	return Result{true, "Focused Ghostty window for " + path + "."}
}

// openNewGhosttyWindow opens a genuinely new window at path. The
// already-open check above just ruled out every existing window, so
// this can't stack a duplicate.
func openNewGhosttyWindow(d deps, path string) Result {
	if err := d.ghosttyOpenNewWindow(path); err != nil {
		return Result{false, err.Error()}
	}
	return Result{true, "Opened a new Ghostty window for " + path + "."}
}
