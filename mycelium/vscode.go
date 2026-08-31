package mycelium

import (
	"net/url"
	"path/filepath"
	"regexp"
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
	return parseVSCodeWindowList(out), nil
}

// parseVSCodeWindowList parses vscodeWindows' raw AppleScript output: one
// record per window, each "title\x1fdocumentURL", records themselves
// separated by \x1e (see vscodeWindows' own script — ASCII characters 31
// and 30). Split out from vscodeWindows so this part, unlike the
// AppleScript call itself, is unit-testable without shelling out to
// osascript. Tolerates the trailing \x1e every window (including the
// last) leaves behind, and any trailing \r\n runOsascript's caller-side
// TrimSpace didn't already strip from an individual record.
func parseVSCodeWindowList(raw string) []vscodeWindow {
	if raw == "" {
		return nil
	}
	records := strings.Split(raw, "\x1e")
	windows := make([]vscodeWindow, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimRight(rec, "\r\n")
		if rec == "" {
			continue
		}
		title, doc, _ := strings.Cut(rec, "\x1f")
		windows = append(windows, vscodeWindow{Title: title, Path: fileURLToPath(doc)})
	}
	return windows
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

// titleSeparator is what VS Code's `${separator}` template variable
// renders between the non-empty parts of `window.title` — whitespace, a
// dash-ish glyph (plain hyphen, en or em dash), whitespace — matched
// tolerantly rather than pinned to the exact " — " the current setting
// produces, the same tolerance the title matchers have always applied. A
// rootName with dashes of its own ("scm-analytics-engineers") never trips
// this: those dashes have no surrounding whitespace.
var titleSeparator = regexp.MustCompile(`\s+[-–—]\s+`)

// parseVSCodeTitle splits a VS Code window title back into the parts this
// ecosystem's `window.title` template
// ("${rootName}${separator}${activeRepositoryBranchName}${separator}${activeEditorShort}",
// see matchVSCodeWindowTitle's doc) put into it: the workspace folder's
// basename, and the branch that folder is on ("" when the title has no
// branch component at all — a bare rootName, with neither a branch nor an
// editor to show). Any third part (the active editor's filename) is
// irrelevant to every matcher here and is dropped. A branch name
// containing the separator itself would parse wrong, but branch names
// with spaces aren't a thing in this ecosystem, so the split is safe in
// practice.
func parseVSCodeTitle(title string) (root, branch string) {
	parts := titleSeparator.Split(title, 3)
	root = parts[0]
	if len(parts) > 1 {
		branch = parts[1]
	}
	return root, branch
}

// matchVSCodeWindowTitle finds the title of the window already showing
// path, going by this ecosystem's `window.title` convention (folder
// basename first, then the branch, then the active file, e.g.
// "understory — main — main.go", or the plain basename on its own with
// nothing open in it yet).
//
// branch is the branch path is expected to be on ("" when the caller
// doesn't know it, e.g. canopy passing a bare agent cwd), and it matters:
// this ecosystem's worktree layout gives every worktree of a repo the
// same leaf folder name as the repo itself, so the basename alone can
// never tell "tardis-community — main" (the main checkout) apart from
// "tardis-community — patch/ISA-…" (a branch worktree) — two different
// folders whose titles both start with the same word. With a known branch
// the match is therefore strict: a title whose rootName matches but whose
// branch component is a *different*, parseable branch is guaranteed to be
// one of those same-named other folders (a worktree directory has exactly
// one branch checked out) and is rejected, never focused. A title with no
// branch component at all (a bare folder name) can't be ruled out the
// same way and stays as a weak fallback, ranked below any full
// rootName+branch match. The strictness deliberately couples matching to
// this ecosystem's documented `window.title` grammar: a title whose
// middle component isn't really the branch (a detached HEAD rendering,
// a changed template) fails closed toward opening a new window.
//
// With branch == "" the behavior is the legacy basename match: a title
// matches when it equals the basename exactly, or starts with the
// basename followed by a space — a real word boundary (so
// "understory-lab — main" does NOT match a search for "understory", since
// the character right after the shared prefix is "-", not a space).
// Weak key either way, same class of limitation as ghosttyFocusByCwd's
// own cwd match: without a branch, two different paths that happen to
// share a leaf folder name are indistinguishable by title alone.
func matchVSCodeWindowTitle(titles []string, path, branch string) (string, bool) {
	base := filepath.Base(path)
	if base == "" {
		return "", false
	}
	if branch != "" {
		weak := ""
		for _, title := range titles {
			root, titleBranch := parseVSCodeTitle(title)
			if root != base {
				continue
			}
			if titleBranch == branch {
				return title, true
			}
			// A different parseable branch means a different same-named
			// folder (see this func's doc): skip it. Only a title with no
			// branch component at all is kept, as the weak fallback.
			if titleBranch == "" && weak == "" {
				weak = title
			}
		}
		if weak != "" {
			return weak, true
		}
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
// (vscodeWindow.Path, from AXDocument) lives inside path's git work tree
// — e.g. path is a monorepo worktree's root and some other window already
// has one of its subpackages open directly, with a file focused there.
// This is meant to run only once matchVSCodeWindowTitle has already ruled
// out a window open on path itself: a window scoped to the exact folder
// is always preferred over reusing one merely scoped somewhere inside it.
//
// "Inside" is decided by git, not by raw path prefix: a window matches
// when its focused file's work-tree root (toplevel("<dir of Path>"))
// equals path's own root or sits underneath it. A bare prefix check
// can't tell "window open on a subpackage of path" apart from "window
// open on some unrelated folder that merely has a file inside path
// focused right now" — a real misfire, observed live: windows scoped to
// a worktree but currently showing a ~/scratch file made ~/scratch (and
// $HOME, and every other ancestor of that file) "match" whatever window
// enumerated first. Git grounding kills that class outright: a focused
// file outside any work tree (scratch notes, /tmp, ...) resolves to ""
// and never matches, and a path that isn't inside a work tree itself
// has nothing to key on and never matches either.
//
// Otherwise this still fails closed, never wrongly claims a match: a
// window sitting on the Explorer/Search panel or an empty editor group,
// with no file currently focused, has Path == "" and looks
// indistinguishable from one that was never opened on that path at all —
// see vscodeWindows' own doc for why. That case is exactly what
// matchVSCodeWindowBranch exists for (it runs next, keying on the
// title's branch component instead).
//
// toplevel is injected so tests stay hermetic (production wires in
// gitToplevel). Results are memoized per call: several windows frequently
// have files focused in the same directory, and each lookup is a git
// subprocess.
func matchVSCodeWindowNestedPath(windows []vscodeWindow, path string, toplevel func(string) string) (string, bool) {
	targetTop := toplevel(path)
	if targetTop == "" {
		return "", false
	}
	resolved := map[string]string{}
	top := func(dir string) string {
		if t, ok := resolved[dir]; ok {
			return t
		}
		t := toplevel(dir)
		resolved[dir] = t
		return t
	}
	for _, w := range windows {
		if w.Path == "" {
			continue
		}
		// AXDocument tracks a file, and git -C needs a directory.
		wt := top(filepath.Dir(w.Path))
		if wt == "" {
			continue
		}
		if wt == targetTop || strings.HasPrefix(wt, targetTop+string(filepath.Separator)) {
			return w.Title, true
		}
	}
	return "", false
}

// genericBranches are branch names too common across repos to key a
// window match on: practically every repo has one checked out somewhere,
// so a title carrying one says nothing about *which* repo's window it is.
var genericBranches = map[string]bool{
	"main":    true,
	"master":  true,
	"develop": true,
	"trunk":   true,
}

// matchVSCodeWindowBranch finds a window by the branch component of its
// title (see parseVSCodeTitle), regardless of which folder that window is
// open on. This is the last-resort nested signal: a window open on a
// subpackage *inside* path but with no file focused leaves
// matchVSCodeWindowNestedPath nothing to work with (AXDocument tracks the
// focused file, not the workspace folder — see vscodeWindows' doc), but
// its title still carries the branch, and a branch is checked out in at
// most one worktree of a repo at a time. So
// "scm-analytics-engineers — patch/ISA-18409-…" identifies the window to
// reuse even with no file open in it.
//
// Two guards keep this failing safe rather than wrong. Generic branch
// names never match (see genericBranches). And the branch must be carried
// by exactly one *distinct* title: two repos sharing a ticket-branch name
// is a real possibility, and an ambiguous answer is worse than none — the
// caller falls through to opening a new window rather than focusing an
// arbitrary one. (Two windows with the *same* title are both a window
// being looked for — e.g. a duplicate already open on the same folder —
// so those do match.)
func matchVSCodeWindowBranch(titles []string, branch string) (string, bool) {
	if branch == "" || genericBranches[branch] {
		return "", false
	}
	found := ""
	for _, title := range titles {
		if _, titleBranch := parseVSCodeTitle(title); titleBranch == branch {
			if found != "" && found != title {
				return "", false // ambiguous: two different windows claim this branch
			}
			found = title
		}
	}
	return found, found != ""
}
