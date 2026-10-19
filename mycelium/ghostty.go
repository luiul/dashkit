package mycelium

// Ghostty window identity is the window title, the same design the VS
// Code path uses (see vscode.go): the dotfiles zsh title hook
// (~/.zsh_config/title.zsh) renders every surface's title as the git
// work-tree root plus the branch, the same grammar VS Code's
// window.title renders, and the session-title pi extension re-asserts
// that grammar while a pi agent runs (pi's own "π - <basename>" title
// carries no path and cannot identify a window), appending the
// surface's tty so two agents side by side in the same repo on the
// same branch — same root, same branch, identical path component —
// stay distinguishable. Matching reuses the VS Code machinery: parse
// the root out of the title, run the same match stages (exact,
// work-tree root, nested — see matchVSCodeWindow), prefer a
// tty-bearing window when the caller knows the target's tty (see
// matchGhosttyWindow), then AXRaise the exact window whose title
// matched, so identification and focus stay bound to the same window.
//
// The previous working-directory AppleScript match is gone on
// purpose. `working directory` is a weak key: two surfaces cd'ed to
// the same directory are indistinguishable, and a stale or divergent
// value focuses the wrong window or opens a duplicate. Ghostty's
// `tty` property, the exact key, still fails on the shipped 1.3.1
// build ("Can't make tty of terminal id ... into type specifier"), so
// titles are the strongest working identity. If `tty` ever starts
// working reliably, switch to matching by tty.
//
// One known gap, accepted: System Events reports one title per window
// (its focused surface), so a surface running in a background tab of
// another window isn't listed by its own title. A miss opens a new
// window, never a hijack.

import (
	"path/filepath"
	"strings"
)

// ghosttyTitleSeparator is what the zsh title hook renders between
// title segments: space, bullet, space.
const ghosttyTitleSeparator = " • "

// ghosttyWindows lists the title of every window System Events can see
// in the ghostty process, plus whether Ghostty is running at all — the
// same shape, record encoding, and AX-culling caveat as vscodeWindows
// (see that function; the running flag exists so callers can tell
// "Ghostty closed" from "running but the listing came back empty",
// which can be macOS culling the AX tree of a backgrounded app,
// luiul/dashkit#9).
func ghosttyWindows() (titles []string, running bool, err error) {
	out, err := runOsascript(`
if application "Ghostty" is running then
	tell application "System Events"
		tell process "ghostty"
			set out to ""
			set RS to (ASCII character 30)
			repeat with w in windows
				set out to out & (name of w) & RS
			end repeat
			return "1" & (ASCII character 31) & out
		end tell
	end tell
else
	return "0" & (ASCII character 31) & ""
end if
`)
	if err != nil {
		return nil, false, err
	}
	titles, running = parseVSCodeWindowList(out)
	return titles, running, nil
}

// ghosttyTitlePath parses the work-tree root out of a Ghostty window
// title. The title grammar is " • "-separated with the root as the
// rightmost segment that is an absolute or ~-shortened path:
// "root • branch" at a shell prompt, "cmd • root • branch" or
// "π • root • branch" while something is running, and a bare "path"
// outside a repo. The session-title pi extension may append the
// surface's tty after the branch ("π • root • branch • ttys034") —
// not a path segment, so the scan skips past it. Returns "" when no
// segment parses (pi's own "π - basename" title, a foreign format):
// such titles never match.
func ghosttyTitlePath(title, home string) string {
	segments := strings.Split(title, ghosttyTitleSeparator)
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		switch {
		case seg == "~":
			seg = home
		case strings.HasPrefix(seg, "~/"):
			seg = home + seg[1:]
		}
		if strings.HasPrefix(seg, "/") {
			return seg
		}
	}
	return ""
}

// parseGhosttyWindows turns raw Ghostty window titles into windows,
// dropping every title with no parseable root (see ghosttyTitlePath).
// The element type is vscodeWindow, the shared match machinery's, with
// the same Title/Path meaning.
func parseGhosttyWindows(titles []string, home string) []vscodeWindow {
	var windows []vscodeWindow
	for _, title := range titles {
		if path := ghosttyTitlePath(title, home); path != "" {
			windows = append(windows, vscodeWindow{Title: title, Path: path})
		}
	}
	return windows
}

// matchGhosttyWindow finds the Ghostty window to focus on path. When
// tty is given (canopy knows each agent's tty from ps, and the
// session-title pi extension puts it in the title), the window whose
// title carries that tty wins — two agents side by side in the same
// repo on the same branch have identical path components and are
// distinguishable only by tty — but its parsed path must still match
// path under the normal stages, so a recycled tty name can't focus an
// unrelated window. Without a tty, or when no tty-bearing window
// matches (an old-format title, an agent started before the
// extension), it falls back to the plain path match
// (matchVSCodeWindow): understory rows have no tty, and a same-path
// window is better than a new duplicate.
func matchGhosttyWindow(windows []vscodeWindow, path string, toplevel func(string) string, tty string) (string, bool) {
	if tty != "" {
		for _, w := range windows {
			if ghosttyTitleTty(w.Title) != tty {
				continue
			}
			if windowPathMatches(w.Path, path, toplevel) {
				return w.Title, true
			}
		}
	}
	return matchVSCodeWindow(windows, path, toplevel)
}

// ghosttyTitleTty returns the short tty name a Ghostty window title
// carries ("ttys034", the form `ps -o tty=` reports), when one of its
// segments is a tty name. "" otherwise.
func ghosttyTitleTty(title string) string {
	for _, seg := range strings.Split(title, ghosttyTitleSeparator) {
		if isTtyName(seg) {
			return seg
		}
	}
	return ""
}

// isTtyName reports whether s is a short macOS tty device name, the
// form `ps -o tty=` reports ("ttys034"). Deliberately narrow: a folder
// or branch named "ttys034" is not a thing in this ecosystem.
func isTtyName(s string) bool {
	if !strings.HasPrefix(s, "ttys") || len(s) == len("ttys") {
		return false
	}
	for _, r := range s[len("ttys"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// windowPathMatches checks one window path against matchVSCodeWindow's
// match stages, in the same order: exact, work-tree root, then the
// window nested inside path. Used by the tty-preferred stage of
// matchGhosttyWindow, where at most one window can carry the tty, so
// per-window stage order is all it needs. (matchVSCodeWindow itself
// keeps its cross-window stage priority and stays the fallback.)
func windowPathMatches(windowPath, path string, toplevel func(string) string) bool {
	if windowPath == path {
		return true
	}
	if root := toplevel(path); root != "" && root != path && windowPath == root {
		return true
	}
	return strings.HasPrefix(windowPath, path+string(filepath.Separator))
}

// ghosttyRaiseWindow brings the Ghostty window with this exact title to
// the front, activating Ghostty itself first (a window can't be raised
// above other apps' windows until its own app is). Returns false, no
// error, if no window with that title exists anymore (e.g. it was
// closed between ghosttyWindows finding it and this call).
func ghosttyRaiseWindow(title string) (bool, error) {
	script := `
tell application "Ghostty" to activate
tell application "System Events"
	tell process "ghostty"
		set matches to (every window whose name is "` + escapeForAppleScript(title) + `")
		if (count of matches) is 0 then
			return "false"
		end if
		perform action "AXRaise" of (item 1 of matches)
		return "true"
	end tell
end tell
`
	out, err := runOsascript(script)
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

// ghosttyActivate brings Ghostty itself to the front. Besides moving
// focus, this re-materializes the process's AX tree the same way
// vscodeActivateCode does: macOS culls windows from System Events' view
// of a backgrounded app, and an activate makes them enumerable again
// (luiul/dashkit#9).
func ghosttyActivate() error {
	_, err := runOsascript(`tell application "Ghostty" to activate`)
	return err
}

// ghosttyOpenNewWindow opens a brand-new Ghostty window with its
// initial working directory set to cwd: the create-a-new-instance half
// of open-or-focus, used when no open window's title matches the path.
func ghosttyOpenNewWindow(cwd string) error {
	escaped := escapeForAppleScript(cwd)
	script := `
    tell application "Ghostty"
        new window with configuration {initial working directory:"` + escaped + `"}
    end tell
    `
	_, err := runOsascript(script)
	return err
}
