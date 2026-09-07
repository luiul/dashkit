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
// focusing is `code --reuse-window <folder>`. The older AppleScript
// title cascade remains as the fallback for when the registry cannot
// answer (extension not installed, no window activated yet), and every
// fallback use is logged so the fallback can be deleted once the log
// shows it unused.
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
// a redundant new one alongside it. The registry answers all three
// stages directly from folder paths. The title fallback answers them
// from titles and focused files instead: see findWindow,
// matchVSCodeWindowNestedPath, and matchVSCodeWindowBranch.
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
	vscodeWindows        func() ([]vscodeWindow, error)
	matchWindowTitle     func(titles []string, path, branch string) (string, bool)
	toplevel             func(dir string) string
	matchNestedWindow    func(windows []vscodeWindow, path string) (string, bool)
	matchWindowBranch    func(titles []string, branch string) (string, bool)
	raiseWindow          func(title string) (bool, error)
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
		vscodeWindows:    vscodeWindows,
		matchWindowTitle: matchVSCodeWindowTitle,
		toplevel:         gitToplevel,
		matchNestedWindow: func(windows []vscodeWindow, path string) (string, bool) {
			return matchVSCodeWindowNestedPath(windows, path, gitToplevel)
		},
		matchWindowBranch:    matchVSCodeWindowBranch,
		raiseWindow:          vscodeRaiseWindow,
		ghosttyFocusByCwd:    ghosttyFocusByCwd,
		ghosttyOpenNewWindow: ghosttyOpenNewWindow,
	}
}

// OpenVSCode opens, or focuses if a window is already open on path, a VS
// Code window there, using the real OS. branch is the branch path is
// expected to be on, or "" when the caller doesn't know it (canopy,
// passing a bare agent cwd). It is advisory: the registry path matches
// on folder paths alone, and only the title fallback uses the branch
// (see matchVSCodeWindowTitle).
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
// call, so nothing stacks up duplicate windows. When the registry
// cannot answer (extension not installed, no fresh entries), the older
// AppleScript title cascade runs instead, unchanged, and the use is
// logged (see logRegistryFallback).
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
func OpenVSCode(path, branch string) Result {
	return openVSCode(defaultDeps(), path, branch)
}

func openVSCode(d deps, path, branch string) Result {
	if path == "" {
		return Result{false, "No known path to open."}
	}

	// The registry is the primary source of window identity. It answers
	// only when it holds at least one fresh entry: an empty or missing
	// registry means the extension is not installed or no window has
	// activated yet, and the title cascade below covers that gap.
	entries, registryOK := d.readRegistry()
	if registryOK && len(entries) > 0 {
		return openVSCodeFromRegistry(d, entries, path)
	}

	reason := "registry-missing"
	if registryOK {
		reason = "registry-empty"
	}
	d.logFallback(reason, path)
	return openVSCodeViaTitles(d, path, branch)
}

// openVSCodeFromRegistry is the primary open-or-focus path: the registry
// says exactly which window (if any) has path open, so focusing is a
// `code --reuse-window` aimed at the matched folder (or the window's
// workspace file, for a multi-root window) and a miss means a genuinely
// new window is safe. `--reuse-window`'s dangerous behavior, hijacking
// the last-active window on a miss, cannot trigger here: the registry
// just saw the window, fresh. The one residual race is a window closing
// inside the staleness window between heartbeat and focus; that falls
// through to a new window, same as a clean miss.
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

// openVSCodeViaTitles is the fallback open-or-focus path, the
// pre-registry behavior unchanged: list windows over AppleScript, run
// the title cascade (see findWindow), raise by exact title. Used only
// when the registry cannot answer; every use is logged by the caller.
func openVSCodeViaTitles(d deps, path, branch string) Result {
	windows, windowsErr := d.vscodeWindows()
	if windowsErr == nil {
		if title, ok := findWindow(d, windows, path, branch); ok {
			if raised, raiseErr := d.raiseWindow(title); raiseErr == nil && raised {
				return Result{true, "Focused VS Code window for " + path + "."}
			}
			// Window vanished between the check and the raise (closed in
			// the meantime), or raising it failed outright: fall through
			// to opening fresh, same as if it had never matched at all.
		}
	}

	if codeBin, ok := d.lookPathCode(); ok {
		// windowsErr != nil means the already-open check itself couldn't
		// run (VS Code scripting not permitted yet, most likely): fall
		// back to the CLI's own best-effort --reuse-window rather than
		// risking a duplicate window on every press. Otherwise the check
		// ran and found nothing, so we know for certain no window is
		// already open — force a genuinely new one (-n) instead of
		// letting --reuse-window guess wrong.
		flag := "-n"
		if windowsErr != nil {
			flag = "--reuse-window"
		}
		if exitOK, _ := d.runCommand([]string{codeBin, flag, path}); exitOK {
			if flag == "-n" {
				return Result{true, "Opened a new VS Code window for " + path + "."}
			}
			return Result{true, "Focused VS Code window for " + path + "."}
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

// findWindow is the title fallback's already-open check, extracted so
// VSCodeSnapshot.IsOpen (the read-only "is a window already open on
// this path?" query backing the dashboards' VS Code columns) runs the
// exact same cascade Enter's open-or-focus does when both have to fall
// back: a column built on it says "open" precisely when OpenVSCode
// would focus rather than create. It runs only when the window
// registry cannot answer (see openVSCode); the registry path matches
// folder paths directly and never builds a title list. Returns the
// matching window's title, or ok=false when nothing currently open
// matches.
//
// The cascade, strongest signal first:
//
//  1. matchWindowTitle on path itself (rootName+branch together when
//     the caller knows the branch; see matchVSCodeWindowTitle's doc).
//  2. matchWindowTitle on path's git work-tree root. path can be a
//     subdirectory of a checkout rather than its root (canopy hands
//     over the agent's cwd as-is, e.g. a monorepo package the agent
//     runs in), and a window open on the root is scoped to the exact
//     tree path lives in, not merely somewhere inside it.
//  3. matchNestedWindow: a window open somewhere *inside* path's tree,
//     found by its currently focused file (see
//     matchVSCodeWindowNestedPath's doc).
//  4. matchWindowBranch: a nested window with no file focused, found
//     by the branch in its title (see matchVSCodeWindowBranch's doc).
func findWindow(d deps, windows []vscodeWindow, path, branch string) (string, bool) {
	titles := make([]string, len(windows))
	for i, w := range windows {
		titles[i] = w.Title
	}
	if title, ok := d.matchWindowTitle(titles, path, branch); ok {
		return title, true
	}
	if root := d.toplevel(path); root != "" && root != path {
		if title, ok := d.matchWindowTitle(titles, root, branch); ok {
			return title, true
		}
	}
	if title, ok := d.matchNestedWindow(windows, path); ok {
		return title, true
	}
	return d.matchWindowBranch(titles, branch)
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
