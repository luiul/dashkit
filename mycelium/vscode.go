package mycelium

// Window identity is the window title. The dotfiles `window.title`
// setting is "${rootPath}${separator}${activeRepositoryBranchName}":
// the opened folder's full path (~-shortened), then the branch.
// Identity is the path portion before the first separator; the branch
// portion is never matched, so the phantom-branch class of false
// positives from the old basename-plus-branch title grammar (a
// main-checkout window whose SCM view activated a worktree's repo)
// stays dead by construction.
//
// Titles beat the alternatives because identification and focus stay
// coupled: AXRaise acts on the exact window whose title matched, with
// no intermediary re-matching. The two deleted designs both broke that
// coupling: `code --reuse-window` re-runs its own matching inside the
// CLI and raises or hijacks whichever window was last active on any
// disagreement (microsoft/vscode#121926, #216602, #215749; observed
// live on 1.137), and the per-window registry identified exactly but
// still had to hand focus back to that same CLI call
// (luiul/dashkit#14). The CLI is only ever asked to open here, never
// to reuse.

import (
	"path/filepath"
	"strings"
)

// titleSeparator is what VS Code's ${separator} template variable
// renders: space, em dash (U+2014), space. windowTitlePath splits on
// the first one only; a folder name containing a spaced em dash of its
// own would parse short, and isn't a thing in this ecosystem. Written
// as an escape so the codepoint is unambiguous in source.
const titleSeparator = " \u2014 "

// vscodeWindow is one currently open VS Code window: its exact title
// (the handle AXRaise focuses by) and the folder path parsed out of it.
type vscodeWindow struct {
	Title string
	Path  string
}

// vscodeWindows lists the title of every window System Events can see
// in the Code process, plus whether VS Code is running at all. running
// lets callers tell "VS Code closed" (a definitive nothing-open) apart
// from "running but no windows listed", which can be macOS culling the
// AX tree of a backgrounded app rather than the truth
// (luiul/dashkit#9).
//
// Returns an empty slice with running false, no error, if VS Code isn't
// running at all: the ordinary "nothing to switch to" case. A non-nil
// error means the AppleScript itself failed (most likely: the
// Automation permission for scripting System Events hasn't been granted
// yet), surfaced through AutomationError / AutomationPermissionError.
func vscodeWindows() (titles []string, running bool, err error) {
	out, err := runOsascript(`
if application "Visual Studio Code" is running then
	tell application "System Events"
		tell process "Code"
			set out to ""
			set RS to (ASCII character 30)
			repeat with w in windows
				set out to out & name of w & RS
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

// parseVSCodeWindowList parses vscodeWindows' raw AppleScript output:
// the running flag, then one record per window, records separated by
// \x1e (ASCII 31 and 30, see vscodeWindows' own script). Split out from
// vscodeWindows so this part, unlike the AppleScript call itself, is
// unit-testable without shelling out to osascript. Tolerates the
// trailing \x1e every window (including the last) leaves behind, and
// any trailing \r\n runOsascript's caller-side TrimSpace didn't strip.
func parseVSCodeWindowList(raw string) (titles []string, running bool) {
	flag, rest, _ := strings.Cut(raw, "\x1f")
	running = flag == "1"
	if rest == "" {
		return nil, running
	}
	for _, rec := range strings.Split(rest, "\x1e") {
		rec = strings.TrimRight(rec, "\r\n")
		if rec != "" {
			titles = append(titles, rec)
		}
	}
	return titles, running
}

// windowTitlePath parses the folder path out of a VS Code window title:
// everything before the first separator, with a leading ~ expanded to
// home. Returns "" when what remains isn't an absolute path (a
// no-folder window's empty title, a foreign title format): such titles
// simply never match.
//
// For a multi-root window ${rootPath} renders the .code-workspace file
// path rather than a member folder, so such a window matches only on
// that path — an accepted limitation (luiul/dashkit#14); the deleted
// registry design treated multi-root focus as best-effort too.
func windowTitlePath(title, home string) string {
	path, _, _ := strings.Cut(title, titleSeparator)
	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = home + path[1:]
	}
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	return path
}

// parseVSCodeWindows turns raw window titles into vscodeWindows,
// dropping every title with no parseable path (see windowTitlePath).
func parseVSCodeWindows(titles []string, home string) []vscodeWindow {
	var windows []vscodeWindow
	for _, title := range titles {
		if path := windowTitlePath(title, home); path != "" {
			windows = append(windows, vscodeWindow{Title: title, Path: path})
		}
	}
	return windows
}

// matchVSCodeWindow finds the window that has path open, strongest
// signal first:
//
//  1. exact: the window's title path is path itself.
//  2. work-tree root: the window's title path is the root of the git
//     work tree path lives in. path can be a subdirectory of a checkout
//     (canopy hands over the agent's cwd as-is), and a window open on
//     the root is scoped to the exact tree path lives in.
//  3. nested: the window's title path sits inside path, on a path
//     element boundary ("/wt-a" must not match "/wt-a-b"). This is the
//     "window open on a subpackage of the worktree" case.
//
// Returns the exact title, the handle vscodeRaiseWindow focuses by. The
// same folder open in two windows matches the first one listed; either
// focus is correct.
func matchVSCodeWindow(windows []vscodeWindow, path string, toplevel func(string) string) (string, bool) {
	for _, w := range windows {
		if w.Path == path {
			return w.Title, true
		}
	}
	if root := toplevel(path); root != "" && root != path {
		for _, w := range windows {
			if w.Path == root {
				return w.Title, true
			}
		}
	}
	prefix := path + string(filepath.Separator)
	for _, w := range windows {
		if strings.HasPrefix(w.Path, prefix) {
			return w.Title, true
		}
	}
	return "", false
}

// matchVSCodeWindowOnWorktree is the strict match for destructive-action
// warnings (understory's and coppice's remove prompts): a window counts
// only when its title path is the worktree at path itself or sits inside
// it, so deleting the worktree would genuinely strand that window.
// Identity is the folder path and the branch component is never matched,
// so the phantom-branch class of false positives from the old
// basename-plus-branch title grammar is impossible by construction.
func matchVSCodeWindowOnWorktree(windows []vscodeWindow, path string) bool {
	prefix := path + string(filepath.Separator)
	for _, w := range windows {
		if w.Path == path || strings.HasPrefix(w.Path, prefix) {
			return true
		}
	}
	return false
}

// vscodeRaiseWindow brings the VS Code window with this exact title to
// the front, activating the app itself first (a window can't be raised
// above other apps' windows until its own app is). Returns false, no
// error, if no window with that title exists anymore (e.g. it was
// closed between vscodeWindows finding it and this call).
func vscodeRaiseWindow(title string) (bool, error) {
	script := `
tell application "Visual Studio Code" to activate
tell application "System Events"
	tell process "Code"
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

// vscodeActivateCode brings VS Code itself to the front. Besides being
// a no-op visually right before focus moves to Code anyway, this
// re-materializes the process's AX tree: macOS culls windows from
// System Events' view of a backgrounded app, and an activate makes them
// enumerable again (luiul/dashkit#9). OpenVSCode uses that to re-list
// once before believing a matchless listing.
func vscodeActivateCode() error {
	_, err := runOsascript(`tell application "Visual Studio Code" to activate`)
	return err
}
