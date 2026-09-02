// Package mycelium opens, or focuses if one is already open, an app
// window (VS Code or a Ghostty terminal) on a given filesystem path,
// without ever risking a duplicate window for a path that's already
// open somewhere.
//
// OpenVSCode's "already open" check isn't limited to a window scoped to
// the exact path either. path can sit *inside* a checkout rather than
// at its root (canopy hands over the agent's cwd as-is, e.g. a monorepo
// package the agent runs in), so when nothing is open on path itself
// the checkout's work-tree root gets a second exact-folder title match
// before anything weaker runs: a window open on the root is scoped to
// the exact tree path lives in, not merely somewhere inside it. And if
// no window is open on either, but some other window already has a file
// focused inside that path's git work tree (e.g. a monorepo subpackage
// opened directly as its own window), that window is reused too rather
// than opening a redundant new one alongside it — see
// matchVSCodeWindowNestedPath's doc for how and why that's a
// best-effort later check, not a guarantee, and for why the "inside"
// test keys on git work trees rather than a raw path prefix. Callers
// that know which branch path is on (understory always does; see
// OpenVSCode's own doc) get two stronger matches on top: windows are
// matched on rootName + branch together, so same-named folders on
// different branches (the main checkout vs. a branch worktree) stop
// being indistinguishable, and a nested window with no file focused is
// still found by the branch in its title (see matchVSCodeWindowBranch).
//
// This is the underground layer shared by canopy (jump to whichever
// window is actually running a given agent) and understory (open or
// focus a worktree on Enter): both need the exact same "is a window
// already open on this path? raise it — otherwise open a genuinely new
// one" behavior, backed by the same AppleScript window detection, so it
// lives here once instead of being duplicated in both trees.
package mycelium

import (
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

// deps groups every external side effect OpenVSCode/OpenGhostty make, so
// tests can swap each one out (see open_test.go) without touching the
// real OS.
type deps struct {
	lookPathCode         func() (string, bool)
	runCommand           func(args []string) (exitOK bool, stderr string)
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
// passing a bare agent cwd): with a known branch, the window matching
// below keys on rootName + branch together instead of on the folder
// basename alone — this ecosystem's worktree layout gives every worktree
// of a repo the same leaf folder name as the repo itself, so the
// basename can never tell the main checkout apart from a branch
// worktree, and a window open on a subpackage *inside* path with no file
// focused is otherwise invisible to the nested-path check (see
// matchVSCodeWindowBranch's doc).
//
// `code --reuse-window <path>` alone isn't enough to get real
// switch-or-create behavior out of the `code` CLI: it only reuses the
// right window when one already has that exact folder open, and
// silently hijacks whichever window was last active otherwise, rather
// than opening a fresh one — confirmed both empirically and in upstream
// reports (microsoft/vscode#121926, #216602, #215749). OpenVSCode checks
// for an already-open window itself first, via each window's title over
// AppleScript, and only ever falls through to the CLI once that's ruled
// out, forcing a genuinely new window (`-n`) rather than handing
// `--reuse-window` a chance to guess wrong. That makes it safe to call
// repeatedly on the same never-before-seen path: the already-open check
// finds the window OpenVSCode itself just created on every subsequent
// call, so nothing stacks up duplicate windows.
//
// path can be a subdirectory of a checkout rather than its root (canopy
// passes the agent's cwd as-is, e.g. a monorepo package the agent runs
// in): when no window is open on path itself, the work-tree root gets a
// second exact-folder title match, so a window open on the checkout as
// a whole is still reused rather than a redundant new one opened next
// to it.
//
// If no window is open on path or its work-tree root, OpenVSCode also
// checks for one open somewhere *inside* path's work tree before giving
// up and opening a new window there — first by focused file
// (matchVSCodeWindowNestedPath), then by the branch in the title
// (matchVSCodeWindowBranch) — e.g. pressing Enter on a monorepo
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

	windows, windowsErr := d.vscodeWindows()
	if windowsErr == nil {
		titles := make([]string, len(windows))
		for i, w := range windows {
			titles[i] = w.Title
		}
		title, ok := d.matchWindowTitle(titles, path, branch)
		if !ok {
			// Nothing open on path itself. path can be a subdirectory of
			// a checkout (canopy hands over the agent's cwd as-is, e.g. a
			// monorepo package the agent runs in), so try the work-tree
			// root as a second exact-folder match before the weaker
			// signals below: a window open on the root is scoped to the
			// exact tree path lives in, not merely somewhere inside it.
			if root := d.toplevel(path); root != "" && root != path {
				title, ok = d.matchWindowTitle(titles, root, branch)
			}
		}
		if !ok {
			// No window scoped to path or its work-tree root either:
			// check for one already open somewhere *inside* path's tree —
			// first by focused file, then, for a nested window with no
			// file focused, by the branch in its title — before falling
			// all the way through to opening a brand-new one.
			title, ok = d.matchNestedWindow(windows, path)
			if !ok {
				title, ok = d.matchWindowBranch(titles, branch)
			}
		}
		if ok {
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

	// Fall back to just raising the app if the `code` shell command
	// isn't installed; this can't target the right *window*, only the
	// app.
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
