package mycelium

// AutomationError means the scripted app didn't do what was asked (focus a
// terminal, raise a window, list window titles, ...).
type AutomationError struct {
	Msg string
}

func (e *AutomationError) Error() string { return e.Msg }

// AutomationPermissionError means the macOS Automation permission for
// scripting the target app (VS Code, Ghostty, System Events, ...) hasn't
// been granted yet. The first attempt normally pops a system permission
// dialog; if nothing is there to click it (e.g. this is being run
// non-interactively), osascript times out or errors instead of prompting,
// which is surfaced as this instead of a generic error so callers (canopy's
// jump package, understory's open-on-Enter) can print something
// actionable rather than a bare AppleScript failure.
type AutomationPermissionError struct {
	AutomationError
}

func newPermissionError(msg string) error {
	return &AutomationPermissionError{AutomationError{Msg: msg}}
}
