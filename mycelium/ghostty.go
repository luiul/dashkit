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
//
// Another gap, handled: a title too long for its window is
// head-truncated by the titlebar with a leading …
// ("…/hellofresh/review-zau/global-ops" for a window on
// ~/worktrees/hellofresh/review-zau/global-ops), and both System
// Events and Ghostty's own AppleScript report only the truncated
// label. The full path is unrecoverable, so parseGhosttyWindows keeps
// such windows and the matcher's tail stage (see matchVSCodeWindow)
// identifies them against the caller's known path by their visible
// tail.

import (
	"path/filepath"
	"strings"
)

// ghosttyTitleSeparator is what the zsh title hook renders between
// title segments: space, bullet, space.
const ghosttyTitleSeparator = " • "

// ghosttyWindows lists the title of every window System Events can see
// across every running Ghostty process, plus whether Ghostty is running
// at all — the same record encoding and AX-culling caveat as
// vscodeWindows (see that function; the running flag exists so callers
// can tell "Ghostty closed" from "running but the listing came back
// empty", which can be macOS culling the AX tree of a backgrounded app,
// luiul/dashkit#9).
//
// Every process, not `tell process "ghostty"`: System Events resolves
// that form to ONE process, and macOS runs several Ghostty instances of
// the same bundle just fine (the dotfiles piw used to spawn one per
// call via `open -na`). A tell-process listing reports only one
// instance's windows, so a jump for a window living in another instance
// looks like a miss and opens a duplicate. Enumerating every process
// named "ghostty" and merging their windows keeps this listing, and
// with it the jump and the "Ghostty open?" snapshot column, correct
// under duplicates. A culled backgrounded instance still contributes
// no windows here — the deep frontmost-then-collect pass in
// findGhosttyWindow and the frontmost-then-search pass in
// ghosttyRaiseWindow handle that.
//
// The whose-filtered process list is enumerated inline in the repeat
// header, never stored in a variable first: a stored whose-filter
// dereferences every one of its items back to the first matching
// process, so the loop would list the first instance's windows once per
// instance and the others' never — the exact bug this exists to fix.
func ghosttyWindows() (titles []string, running bool, err error) {
	out, err := runOsascript(`
tell application "System Events"
	if (count of (every process whose name is "ghostty")) is 0 then
		return "0" & (ASCII character 31) & ""
	end if
	set out to ""
	set RS to (ASCII character 30)
	repeat with p in (every process whose name is "ghostty")
		repeat with w in (every window of p)
			set out to out & (name of w) & RS
		end repeat
	end repeat
	return "1" & (ASCII character 31) & out
end tell
`)
	if err != nil {
		return nil, false, err
	}
	titles, running = parseVSCodeWindowList(out)
	return titles, running, nil
}

// ghosttyWindowsDeep is ghosttyWindows' frontmost-then-collect
// variant, for callers that are about to move focus anyway (the jump
// path): it sets each Ghostty process frontmost in turn — which
// materializes that process's AX tree, the same re-materialization an
// activate provides (luiul/dashkit#9) — and collects its window titles
// as plain strings while it is frontmost. Strings don't decay: once
// collected, later culling of the now-backgrounded process can't
// retract them, unlike an activate-then-relist, which asks for all
// windows at once and gets only whichever single instance is still
// materialized. That makes it the only way to see every window across
// several Ghostty instances. It moves focus, so the SnapshotGhostty
// poll keeps using the plain ghosttyWindows. Same inline-enumeration
// rules as ghosttyWindows: no stored whose-filters, and no windows
// kept as references across iterations.
func ghosttyWindowsDeep() (titles []string, running bool, err error) {
	out, err := runOsascript(`
tell application "System Events"
	if (count of (every process whose name is "ghostty")) is 0 then
		return "0" & (ASCII character 31) & ""
	end if
	set out to ""
	set RS to (ASCII character 30)
	repeat with p in (every process whose name is "ghostty")
		set frontmost of p to true
		repeat with w in (every window of p)
			set out to out & (name of w) & RS
		end repeat
	end repeat
	return "1" & (ASCII character 31) & out
end tell
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
// segment parses: a foreign format (pi's own "π - basename" title)
// never matches, and a head-truncated title (leading …) matches only
// through the tail stage instead (see ghosttyTruncatedTail).
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

// ghosttyTruncatedTail returns the visible path tail of a
// head-truncated Ghostty window title, "" when the title isn't
// truncated or the tail is too weak to match on. A title longer than
// its window is truncated from the left with a leading … (U+2026)
// by the titlebar, and both System Events and Ghostty's own
// AppleScript report the truncated label, so the full title is
// unrecoverable: the visible tail is the only identity left. The tail
// is the text after the … up to the first title separator — the
// surviving right segments are branch and tty, not path — and it
// must carry a separator of its own: a fragment with no element
// boundary left ("…global-ops", or a cut that consumed the whole
// path segment) never matches, erring toward opening a new window
// over focusing a guess.
func ghosttyTruncatedTail(title string) string {
	rest, ok := strings.CutPrefix(title, "…")
	if !ok {
		return ""
	}
	seg, _, _ := strings.Cut(rest, ghosttyTitleSeparator)
	if !strings.Contains(seg, "/") {
		return ""
	}
	return seg
}

// parseGhosttyWindows turns raw Ghostty window titles into windows.
// A title with a parseable root keeps that root as Path. A title
// head-truncated by the titlebar (leading …, see
// ghosttyTruncatedTail) has no parseable root but is kept anyway,
// with Path empty and the visible tail in Title: the matcher's tail
// stage can still identify it against a known path. Every other
// unparseable title (pi's own "π - basename", a foreign format) is
// dropped as before. The element type is vscodeWindow, the shared
// match machinery's, with the same Title/Path meaning.
func parseGhosttyWindows(titles []string, home string) []vscodeWindow {
	var windows []vscodeWindow
	for _, title := range titles {
		if path := ghosttyTitlePath(title, home); path != "" {
			windows = append(windows, vscodeWindow{Title: title, Path: path})
		} else if ghosttyTruncatedTail(title) != "" {
			windows = append(windows, vscodeWindow{Title: title})
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
// unrelated window. A head-truncated window has no parseable path,
// so it can never win the tty stage; it matches only through
// matchVSCodeWindow's tail stage. Without a tty, or when no tty-bearing
// window matches (an old-format title, an agent started before the
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
//
// Frontmost-then-search per process: with several Ghostty instances
// running, `tell process "ghostty"` sees only one of them, and a
// backgrounded instance's AX tree is culled entirely (luiul/dashkit#9),
// so the target window can be invisible exactly where it lives. Making
// each process frontmost right before searching it re-materializes that
// process's tree; the loop exits with the holding instance frontmost
// and its window raised, whichever instance that is. With one instance
// this is the old activate-and-raise plus one redundant frontmost.
//
// The window search deliberately avoids `every window of p whose name
// is X`: that whose-filter mints window references against the by-NAME
// `application process "ghostty"`, which System Events resolves to
// the FIRST instance — so with duplicates the AXRaise would target a
// window of the wrong instance (or error out on one that lives in the
// other). Iterating `every window of p` plainly and comparing names in
// AppleScript keeps the per-process reference context, the same inline
// form ghosttyWindows' enumeration proves correct. Nothing is stored
// across iterations: stored whose-filter items dereference back to the
// first match.
func ghosttyRaiseWindow(title string) (bool, error) {
	script := `
tell application "Ghostty" to activate
tell application "System Events"
	repeat with p in (every process whose name is "ghostty")
		set frontmost of p to true
		repeat with w in (every window of p)
			if (name of w) is "` + escapeForAppleScript(title) + `" then
				perform action "AXRaise" of w
				set frontmost of p to true
				return "true"
			end if
		end repeat
	end repeat
	return "false"
end tell
`
	out, err := runOsascript(script)
	if err != nil {
		return false, err
	}
	return out == "true", nil
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
