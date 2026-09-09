// Package mycelium opens, or focuses if one is already open, an app
// window (VS Code or a Ghostty terminal) on a given filesystem path,
// without ever risking a duplicate window for a path that's already
// open somewhere.
//
// For VS Code, window identity comes from the window registry: every
// window self-registers into ~/.local/state/vscode-windows/ via the
// vscode-window-registry extension (see registry.go and dashkit's
// top-level vscode-window-registry/ directory), so "is a window already
// open on this path?" is answered by matching exact folder paths, and
// focusing is `code --reuse-window <folder>`.
//
// The "already open" check isn't limited to a window scoped to the
// exact path either. path can sit *inside* a checkout rather than at
// its root (canopy hands over the agent's cwd as-is, e.g. a monorepo
// package the agent runs in), so when nothing is open on path itself
// the checkout's work-tree root gets a second exact-folder match before
// anything weaker runs: a window open on the root is scoped to the
// exact tree path lives in, not merely somewhere inside it. And a
// window open on a folder nested inside path (a monorepo subpackage
// opened directly as its own window) is reused too rather than opening
// a redundant new one alongside it. See matchRegistry for the three
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
	"os/exec"
	"strings"
	"time"
)

// Result reports whether opening/focusing a window succeeded, and a
// human-readable message about what happened, meant to be shown
// straight to a user (e.g. as a TUI notification).
type Result struct {
	OK      bool
	Message string
}

// deps groups every external side effect OpenVSCode/OpenGhostty make, so
// tests can swap each one out (see open_test.go) without touching the
// real OS.
type deps struct {
	lookPathCode         func() (string, bool)
	runCommand           func(args []string) (exitOK bool, stderr string)
	readRegistry         func() ([]registryEntry, bool)
	logFallback          func(reason, path string)
	toplevel             func(dir string) string
	ghosttyFocusByCwd    func(cwd string) (bool, error)
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
		readRegistry: func() ([]registryEntry, bool) {
			return readRegistry(registryDir(), time.Now())
		},
		logFallback: func(reason, path string) {
			logRegistryFallback(registryDir(), reason, path)
		},
		toplevel:             gitToplevel,
		ghosttyFocusByCwd:    ghosttyFocusByCwd,
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
// reports (microsoft/vscode#121926, #216602, #215749). OpenVSCode checks
// for an already-open window itself first, via the window registry (see
// registry.go), and only ever falls through to the CLI once that's
// ruled out, forcing a genuinely new window (`-n`) rather than handing
// `--reuse-window` a chance to guess wrong. That makes it safe to call
// repeatedly on the same never-before-seen path: the already-open check
// finds the window OpenVSCode itself just created on every subsequent
// call, so nothing stacks up duplicate windows.
//
// path can be a subdirectory of a checkout rather than its root (canopy
// passes the agent's cwd as-is, e.g. a monorepo package the agent runs
// in): when no window is open on path itself, the work-tree root gets a
// second exact-folder match, so a window open on the checkout as a
// whole is still reused rather than a redundant new one opened next
// to it.
//
// If no window is open on path or its work-tree root, OpenVSCode also
// checks for one open somewhere *inside* path before giving up and
// opening a new window there — e.g. pressing Enter on a monorepo
// worktree's root reuses a window already open on one of its
// subpackages, rather than opening a second, redundant window on the
// same tree.
//
// When the registry cannot answer at all (the extension is not
// installed or its directory is unreadable), there is no way to tell
// what is open: OpenVSCode degrades to the CLI's own best-effort
// `--reuse-window path` and logs the miss (see logRegistryFallback), so
// a broken extension is visible rather than silently stacking
// duplicates.
func OpenVSCode(path string) Result {
	return openVSCode(defaultDeps(), path)
}

func openVSCode(d deps, path string) Result {
	if path == "" {
		return Result{false, "No known path to open."}
	}

	entries, registryOK := d.readRegistry()
	if !registryOK {
		d.logFallback("registry-missing", path)
		if codeBin, ok := d.lookPathCode(); ok {
			if exitOK, _ := d.runCommand([]string{codeBin, "--reuse-window", path}); exitOK {
				return Result{true, "Opened or focused VS Code window for " + path + "."}
			}
		}
		return openVSCodeAppFallback(d, path)
	}
	return openVSCodeFromRegistry(d, entries, path)
}

// openVSCodeFromRegistry is the open-or-focus path: the registry says
// exactly which window (if any) has path open, so focusing is a
// `code --reuse-window` aimed at the matched folder (or the window's
// workspace file, for a multi-root window) and a miss means a genuinely
// new window is safe. `--reuse-window`'s dangerous behavior, hijacking
// the last-active window on a miss, cannot trigger here: the registry
// just saw the window, fresh.
//
// The one residual race is a window vanishing inside the staleness
// window between heartbeat and focus, and only a crash or kill can cause
// it: a gracefully closed window runs the extension's deactivate and
// deletes its own entry (verified live), so the common case never
// races. When it does happen, the stale match hands `--reuse-window` a
// folder no window has open and the CLI hijacks the most-recent window
// into path (observed on VS Code 1.136) instead of opening a new one.
// The hijacked window then re-registers with path, so the next
// OpenVSCode focuses it correctly: a one-time disruption, not a stuck
// state.
//
// Multi-root windows are the one focus weakness: `--reuse-window`
// aimed at an already-open workspace file is a no-op (verified on VS
// Code 1.136): it neither raises the window nor opens a duplicate. The
// match still prevents a redundant window, but the existing one may not
// come to front. No CLI spelling raises it (`open -a` on the workspace
// file doesn't either); raising it would take an AppleScript AXRaise by
// window title, which the registry design deliberately dropped.
func openVSCodeFromRegistry(d deps, entries []registryEntry, path string) Result {
	codeBin, haveCode := d.lookPathCode()
	if target, found := matchRegistry(entries, path, d.toplevel); found && haveCode {
		if exitOK, _ := d.runCommand([]string{codeBin, "--reuse-window", target}); exitOK {
			return Result{true, "Focused VS Code window for " + path + "."}
		}
	}
	if haveCode {
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

// OpenGhostty focuses a Ghostty terminal whose working directory is
// path, bringing its window to the front, or opens a brand-new window
// there if nothing currently open matches (e.g. that tab has since been
// closed).
func OpenGhostty(path string) Result {
	return openGhostty(defaultDeps(), path)
}

func openGhostty(d deps, path string) Result {
	if path == "" {
		return Result{false, "No known path to focus."}
	}

	found, err := d.ghosttyFocusByCwd(path)
	if err != nil {
		return Result{false, err.Error()}
	}
	if found {
		return Result{true, "Focused in Ghostty."}
	}

	// Nothing open matches this path anymore (e.g. the tab closed since
	// the caller last resolved it). Rather than dead-ending, create a
	// fresh instance at the same path, mirroring OpenVSCode's
	// reuse-or-create behavior via `-n`.
	if err := d.ghosttyOpenNewWindow(path); err != nil {
		return Result{false, err.Error()}
	}
	return Result{true, "Opened a new Ghostty window for " + path + "."}
}
