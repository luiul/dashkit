package mycelium

// Ghostty window identity is layered. The primary source is Ghostty's
// own AppleScript surface listing (see ghosttySurfaces): every window,
// tab, and split of the instance macOS resolves "Ghostty" to, each
// with its tracked working directory reported as data, not prose. The
// tracked directory is the surface's creation seed updated by every
// OSC 7 report, and the dotfiles title hook reports OSC 7 at every
// prompt from .zshrc (nested shells too, not only the surface's own
// login shell), so the value follows the foreground shell. Matching
// runs the shared stage order over those directories (see
// matchGhosttySurfaces), and the winner is focused by its window id
// and tab/terminal coordinates (ghosttyFocusSurface), so
// identification and focus stay bound to the same surface.
//
// The System Events window-title layer (ghosttyWindows,
// ghosttyWindowsDeep, matchGhosttyWindow, ghosttyRaiseWindow) is the
// second source, kept for two things the bridge cannot do: it sees
// EVERY running Ghostty instance (the bridge addresses exactly one),
// and it works when the Ghostty automation permission is missing while
// System Events automation is granted. Its identity is the window
// title, the same design the VS Code path uses (see vscode.go): the
// dotfiles zsh title hook
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
//
// The old reason `working directory` was rejected as a key — stale
// values frozen at a surface's creation directory — died with that
// per-prompt reporting: every surface a jump would want now carries a
// current directory. What stays weak is only the ambiguity: two
// surfaces cd'ed to the same directory are indistinguishable, so the
// caller that needs the exact one passes a tty (canopy; see
// matchGhosttySurfaces), and understory's worktree rows have no tty
// and accept any window on the path.
//
// Ghostty builds expose the surface tty through AppleScript only
// since upstream ghostty#11922, merged after the shipped 1.3.1
// stable; on 1.3.1 the tty read fails at runtime and the listing
// script's try-guard leaves "", so matching degrades to the path
// stages exactly as before. When the running build gains the
// property, tty matching starts working with no change here.
//
// One known gap of the title layer, accepted: System Events reports
// one title per window (its focused surface), so a surface running in
// a background tab of another window isn't listed by its own title. A
// miss opens a new window, never a hijack. The surface layer has no
// such gap, which is why it is primary.
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
	"strconv"
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

// ghosttySurface is one terminal surface as Ghostty's own AppleScript
// reports it: the surface's tracked working directory (seed at
// creation, updated by every OSC 7 report), plus the coordinates that
// focus it again — its window's id and its tab/terminal positions.
// tty carries the surface's short tty name ("ttys016", the form ps
// reports) on Ghostty builds that expose the tty property (upstream
// ghostty#11922, merged after 1.3.1); on builds without it the listing
// script's try-guard leaves "" and matching degrades to the path
// stages.
type ghosttySurface struct {
	windowID string
	tabIdx   int
	termIdx  int
	dir      string
	tty      string
}

// ghosttySurfaceKeySep is the byte the surface key and the listing
// record share (unit separator, 0x1f).
const ghosttySurfaceKeySep = "\x1f"

// key encodes a surface's focus coordinates as the Title of a
// match-machinery vscodeWindow, so matchVSCodeWindow runs unchanged
// over surface-derived windows and returns the key the focus step
// resolves back into coordinates. The directory rides in the
// vscodeWindow's Path, the actual match key.
func (s ghosttySurface) key() string {
	return s.windowID + ghosttySurfaceKeySep + strconv.Itoa(s.tabIdx) + ghosttySurfaceKeySep + strconv.Itoa(s.termIdx)
}

// ghosttyProcessCount reports how many Ghostty processes are running.
// It is the surface stage's not-running check, run through System
// Events before any `tell application "Ghostty"` block so the stage
// never LAUNCHES a Ghostty that isn't there: osascript resolves the
// tell block's dictionary at compile time, and compiling against a
// non-running app starts it. It also tells the caller whether the
// title stage is even needed (see openGhostty: one instance means the
// surface listing already saw everything the title stage could).
func ghosttyProcessCount() (int, error) {
	out, err := runOsascript(`tell application "System Events" to count of (every process whose name is "ghostty")`)
	if err != nil {
		return 0, err
	}
	n, perr := strconv.Atoi(out)
	if perr != nil {
		return 0, &AutomationError{Msg: "unparseable Ghostty process count: " + out}
	}
	return n, nil
}

// ghosttySurfaces lists every terminal surface of every Ghostty
// window through Ghostty's own AppleScript bridge, plus the number of
// running Ghostty processes (0 means Ghostty isn't running: no
// surfaces, no error). Every tab and split of every window of the ONE
// instance macOS resolves "Ghostty" to, no AX culling (the bridge
// reads the app's own state, not the accessibility tree), and each
// surface's tracked working directory as data. The bridge cannot see
// other Ghostty instances; the title stage covers those (see
// openGhostty and ghosttyWindows).
//
// The tty property is read inside a try-guard because builds without
// it (1.3.1 stable; the property arrived upstream in ghostty#11922)
// evaluate `tty of term` to a runtime error the try swallows, leaving
// "". On builds with it the device path is stripped of its /dev/
// prefix into the short form ps reports.
func ghosttySurfaces() ([]ghosttySurface, int, error) {
	instances, err := ghosttyProcessCount()
	if err != nil {
		return nil, 0, err
	}
	if instances == 0 {
		return nil, 0, nil
	}
	out, err := runOsascript(`
tell application "Ghostty"
	set out to ""
	set RS to (ASCII character 30)
	set US to (ASCII character 31)
	repeat with w in windows
		set tIdx to 0
		repeat with t in tabs of w
			set tIdx to tIdx + 1
			set eIdx to 0
			repeat with term in terminals of t
			set eIdx to eIdx + 1
			set devTty to ""
			try
				set ttyPath to tty of term
				if ttyPath starts with "/dev/" then
					set devTty to text 6 thru -1 of ttyPath
				end if
			end try
			set out to out & (id of w as text) & US & (tIdx as text) & US & (eIdx as text) & US & (working directory of term) & US & devTty & RS
			end repeat
		end repeat
	end repeat
	return out
end tell
`)
	if err != nil {
		return nil, instances, err
	}
	return parseGhosttySurfaces(out), instances, nil
}

// parseGhosttySurfaces decodes ghosttySurfaces' listing: RS-separated
// records (0x1e), each US-separated (0x1f) into window id, tab index,
// terminal index, working directory, and short tty name. Malformed
// records are skipped, not fatal: a listing the script built field by
// field can only lose whole records to a race (a window closed mid-
// enumeration), and one lost record must not blind the whole stage.
func parseGhosttySurfaces(raw string) []ghosttySurface {
	var surfaces []ghosttySurface
	for _, record := range strings.Split(raw, "\x1e") {
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) != 5 || fields[0] == "" {
			continue
		}
		tabIdx, errTab := strconv.Atoi(fields[1])
		termIdx, errTerm := strconv.Atoi(fields[2])
		if errTab != nil || errTerm != nil {
			continue
		}
		surfaces = append(surfaces, ghosttySurface{
			windowID: fields[0],
			tabIdx:   tabIdx,
			termIdx:  termIdx,
			dir:      fields[3],
			tty:      fields[4],
		})
	}
	return surfaces
}

// matchGhosttySurfaces finds the surface to focus on path. When tty is
// given and the running Ghostty exposes the property (see
// ghosttySurface), the one surface carrying that tty wins, with its
// directory still checked against path so a recycled tty name can't
// focus an unrelated window. Otherwise the surface directories run
// through matchVSCodeWindow's stages unchanged (exact, work-tree root,
// nested); the truncated-tail stage never fires because a surface's
// path is data, never head-truncated prose. Surfaces without a
// directory (never reported one) match nothing. The winner comes back
// as decoded focus coordinates; the directory and tty were match
// inputs only.
func matchGhosttySurfaces(surfaces []ghosttySurface, path string, toplevel func(string) string, tty string) (ghosttySurface, bool) {
	if tty != "" {
		for _, s := range surfaces {
			if s.tty == tty && windowPathMatches(s.dir, path, toplevel) {
				return s, true
			}
		}
	}
	windows := ghosttySurfaceWindows(surfaces)
	if key, ok := matchVSCodeWindow(windows, path, toplevel); ok {
		if s, ok := parseGhosttySurfaceKey(key); ok {
			return s, true
		}
	}
	return ghosttySurface{}, false
}

// parseGhosttySurfaceKey decodes key()'s encoding back into focus
// coordinates. The key crosses the match machinery as a vscodeWindow's
// Title — a string handle — so the winner comes back as a key, not a
// surface; the directory and tty rode along only for matching and stay
// empty here. Focusing needs the coordinates alone.
func parseGhosttySurfaceKey(key string) (ghosttySurface, bool) {
	parts := strings.Split(key, ghosttySurfaceKeySep)
	if len(parts) != 3 || parts[0] == "" {
		return ghosttySurface{}, false
	}
	tabIdx, errTab := strconv.Atoi(parts[1])
	termIdx, errTerm := strconv.Atoi(parts[2])
	if errTab != nil || errTerm != nil {
		return ghosttySurface{}, false
	}
	return ghosttySurface{windowID: parts[0], tabIdx: tabIdx, termIdx: termIdx}, true
}

// ghosttySurfaceWindows turns a surface listing into the match
// machinery's window records (see SnapshotGhostty): one per surface
// with a directory, its focus-coordinates key in Title and the
// directory in Path, so IsOpen and IsOpenOnWorktree run their stages
// unchanged. A surface without a directory (never reported one) is
// skipped: nothing solid to match on.
func ghosttySurfaceWindows(surfaces []ghosttySurface) []vscodeWindow {
	windows := make([]vscodeWindow, 0, len(surfaces))
	for _, s := range surfaces {
		if s.dir == "" {
			continue
		}
		windows = append(windows, vscodeWindow{Title: s.key(), Path: s.dir})
	}
	return windows
}

// ghosttyFocusSurface focuses the surface at these coordinates:
// Ghostty's `focus` command selects the surface's tab and raises its
// window, so the focus binds to the exact surface that matched — no
// re-matching by title in between, the same guarantee the title path's
// AXRaise-by-exact-title gives. A window that vanished between the
// listing and the focus reports errAENoSuchObject (-1728) and counts
// as a miss (the caller opens a fresh window); other errors surface,
// because the window is known to be open and opening a new one would
// stack the duplicate this library exists to prevent.
func ghosttyFocusSurface(s ghosttySurface) (bool, error) {
	script := `
tell application "Ghostty"
	activate
	focus (terminal ` + strconv.Itoa(s.termIdx) + ` of tab ` + strconv.Itoa(s.tabIdx) + ` of window id "` + escapeForAppleScript(s.windowID) + `")
end tell
return "true"
`
	out, err := runOsascript(script)
	if err != nil {
		if isNoSuchObjectError(err) {
			return false, nil
		}
		return false, err
	}
	return out == "true", nil
}

// isNoSuchObjectError reports whether err is errAENoSuchObject, the
// error a vanished window/tab/terminal specifier produces (-1728,
// "Can't get ..." or its typographic-apostrophe twin).
func isNoSuchObjectError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "-1728") ||
		strings.Contains(msg, "Can't get") ||
		strings.Contains(msg, "Can\u2019t get")
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
