package mycelium

import (
	"path/filepath"
	"strings"
)

// vscodeWindowTitles lists every currently open VS Code window's title, via
// System Events. Returns an empty slice, no error, if VS Code isn't
// running at all: that's the ordinary "nothing to switch to" case, not a
// failure. A non-nil error means the AppleScript itself failed (most
// likely: the Automation permission for scripting VS Code hasn't been
// granted yet), which OpenVSCode treats as "couldn't tell, fall back to
// --reuse-window" rather than as "definitely nothing open".
//
// This exists because `code --reuse-window <path>` alone isn't enough to
// get real switch-or-create behavior out of the `code` CLI: it only
// reuses the right window when one already has that exact folder open,
// and silently hijacks whichever window was last active otherwise,
// rather than opening a fresh one. Confirmed both empirically and in
// upstream reports (microsoft/vscode#121926, #216602, #215749).
// Detecting "already open" ourselves, via each window's title, and only
// ever calling `code` at all for the "not already open" half, avoids
// that fallback entirely: the CLI never gets a chance to guess wrong
// about which window to reuse.
func vscodeWindowTitles() ([]string, error) {
	out, err := runOsascript(`
if application "Visual Studio Code" is running then
	tell application "System Events"
		tell process "Code"
			get name of every window
		end tell
	end tell
else
	return ""
end if
`)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	// osascript joins an AppleScript list with ", " when coerced to text.
	titles := strings.Split(out, ", ")
	for i := range titles {
		titles[i] = strings.TrimSpace(titles[i])
	}
	return titles, nil
}

// vscodeRaiseWindow brings the VS Code window with this exact title to the
// front, activating the app itself first (a window can't be raised above
// other apps' windows until its own app is). Returns false, no error, if
// no window with that title exists anymore (e.g. it was closed between
// vscodeWindowTitles finding it and this call).
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

// matchVSCodeWindowTitle finds the first title that's already showing
// path, going by this ecosystem's `window.title` convention (folder
// basename first, then a separator and the branch, e.g.
// "understory — main", or the plain basename on its own with nothing open
// in it yet). A title matches when it equals the basename exactly, or
// starts with the basename followed by a space: that's a real word
// boundary (so "understory-lab — main" does NOT match a search for
// "understory", since the character right after the shared prefix is
// "-", not a space), tolerant of whatever separator glyph sits between
// folder name and branch (em dash, plain hyphen, ...) without hardcoding
// one. Weak key, same class of limitation as ghosttyFocusByCwd's own cwd
// match: two different paths that happen to share a leaf folder name are
// indistinguishable by title alone.
func matchVSCodeWindowTitle(titles []string, path string) (string, bool) {
	base := filepath.Base(path)
	if base == "" {
		return "", false
	}
	for _, title := range titles {
		if title == base {
			return title, true
		}
		if strings.HasPrefix(title, base+" ") {
			return title, true
		}
	}
	return "", false
}
