package mycelium

import (
	"net/url"
	"path/filepath"
	"strings"
)

// vscodeWindow is one currently open VS Code window: its title, plus the
// POSIX path of whichever file is currently active in it, if any (see
// vscodeWindows' doc for where Path comes from and why it's often "").
type vscodeWindow struct {
	Title string
	Path  string
}

// vscodeWindows lists every currently open VS Code window, via System
// Events. Returns an empty slice, no error, if VS Code isn't running at
// all: that's the ordinary "nothing to switch to" case, not a failure. A
// non-nil error means the AppleScript itself failed (most likely: the
// Automation permission for scripting VS Code hasn't been granted yet),
// which OpenVSCode treats as "couldn't tell, fall back to
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
//
// Each window's Path comes from the "AXDocument" accessibility
// attribute, the same one backing VS Code's macOS title bar proxy
// icon/breadcrumb (Electron's BrowserWindow.setRepresentedFilename,
// kept in sync with the active editor). Confirmed empirically: it
// tracks whichever *file* is currently focused in that window, not the
// workspace folder itself, and is "" whenever nothing is (an empty
// editor group, the Explorer/Search panel focused, ...) — there's no
// reliable way to read a window's workspace-root folder path directly,
// only whatever file (if any) happens to be open in it right now.
// That's still useful: matchVSCodeWindowNestedPath uses it to tell
// whether some other already-open window's active file lives inside a
// given folder, which the title alone (just a basename) can never
// answer.
func vscodeWindows() ([]vscodeWindow, error) {
	out, err := runOsascript(`
if application "Visual Studio Code" is running then
	tell application "System Events"
		tell process "Code"
			set out to ""
			set US to (ASCII character 31)
			set RS to (ASCII character 30)
			repeat with w in windows
				set t to name of w
				try
					set d to value of attribute "AXDocument" of w
					if d is missing value then set d to ""
				on error
					set d to ""
				end try
				set out to out & t & US & d & RS
			end repeat
			return out
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
	records := strings.Split(out, "\x1e")
	windows := make([]vscodeWindow, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimRight(rec, "\r\n")
		if rec == "" {
			continue
		}
		title, doc, _ := strings.Cut(rec, "\x1f")
		windows = append(windows, vscodeWindow{Title: title, Path: fileURLToPath(doc)})
	}
	return windows, nil
}

// fileURLToPath converts a "file://..." URL (percent-encoded, as
// AXDocument reports it) to a plain POSIX path, or "" if raw isn't a
// file URL at all (empty, or some other scheme entirely).
func fileURLToPath(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

// vscodeRaiseWindow brings the VS Code window with this exact title to the
// front, activating the app itself first (a window can't be raised above
// other apps' windows until its own app is). Returns false, no error, if
// no window with that title exists anymore (e.g. it was closed between
// vscodeWindows finding it and this call).
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

// matchVSCodeWindowNestedPath finds a window whose currently active file
// (vscodeWindow.Path, from AXDocument) lives inside path — e.g. path is
// a monorepo worktree's root and some other window already has one of
// its subpackages open directly, with a file focused there. This is
// meant to run only once matchVSCodeWindowTitle has already ruled out a
// window open on path itself: a window scoped to the exact folder is
// always preferred over reusing one merely scoped somewhere inside it.
//
// This fails closed, never wrongly claims a match: a window sitting on
// the Explorer/Search panel or an empty editor group, with no file
// currently focused, has Path == "" and looks indistinguishable from one
// that was never opened on that path at all — see vscodeWindows' own doc
// for why. It also only ever matches strictly *inside* path (Path must
// have path + "/" as a prefix, not just share a string prefix), so e.g.
// a window open on "/x/understory-lab" never matches a search for
// "/x/understory", the same word-boundary care matchVSCodeWindowTitle
// already takes for titles.
func matchVSCodeWindowNestedPath(windows []vscodeWindow, path string) (string, bool) {
	prefix := filepath.Clean(path) + string(filepath.Separator)
	for _, w := range windows {
		if w.Path == "" {
			continue
		}
		if strings.HasPrefix(filepath.Clean(w.Path), prefix) {
			return w.Title, true
		}
	}
	return "", false
}
