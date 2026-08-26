package mycelium

// ghosttyFocusByCwd focuses a Ghostty terminal surface whose working
// directory is cwd, bringing its window to the front. Returns false (not
// an error) if no open terminal currently matches, e.g. it already
// closed, or the process has since `cd`-ed elsewhere since the caller
// last resolved it.
//
// Ghostty's own scripting dictionary
// (https://github.com/ghostty-org/ghostty/blob/main/macos/Ghostty.sdef)
// declares a `tty` property per terminal surface, which would be the
// exact match callers like canopy actually want (it already resolves
// every agent's tty via `ps`). In practice, on the shipped 1.3.1 build,
// `tty` (and `pid`) reliably fails: "Can't make tty of terminal id ...
// into type specifier", while string properties like `working
// directory` and `name` work fine. So this matches by working directory
// instead. That's a weaker key: two tabs `cd`-ed to the same directory
// are indistinguishable, this just focuses whichever one Ghostty's
// script bridge returns first. If tty ever starts working reliably,
// this should switch back to matching by tty.
func ghosttyFocusByCwd(cwd string) (bool, error) {
	escaped := escapeForAppleScript(cwd)
	script := `
    tell application "Ghostty"
        repeat with t in terminals
            if (working directory of t) is "` + escaped + `" then
                focus t
                return "true"
            end if
        end repeat
    end tell
    return "false"
    `
	out, err := runOsascript(script)
	if err != nil {
		return false, err
	}
	return out == "true", nil
}

// ghosttyOpenNewWindow opens a brand-new Ghostty window with its initial
// working directory set to cwd. This is the create-a-new-instance half of
// switch-or-create behavior: when ghosttyFocusByCwd finds nothing to
// focus (e.g. the agent's tab has since been closed), this opens a fresh
// one at the same location instead of just reporting failure.
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
