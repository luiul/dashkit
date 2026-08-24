package mycelium

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const osascriptTimeout = 6 * time.Second

// runOsascript runs script via `osascript -e` and returns its trimmed
// stdout. A timed-out or permission-denied run is reported as an
// AutomationPermissionError rather than a bare error, since both cases
// are "answer the macOS Automation prompt" from the caller's point of
// view, not a bug in the script itself.
func runOsascript(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), osascriptTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return "", newPermissionError(
			"The scripted app didn't respond to AppleScript in time. macOS may be waiting on an " +
				"Automation permission prompt you haven't seen/answered yet: System Settings -> " +
				"Privacy & Security -> Automation, allow your terminal to control it.",
		)
	}

	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		lowered := strings.ToLower(stderrStr)
		if strings.Contains(stderrStr, "-1743") || strings.Contains(lowered, "not allowed") || strings.Contains(lowered, "not authorized") {
			return "", newPermissionError(
				"macOS hasn't granted Automation permission for scripting yet. Go to System " +
					"Settings -> Privacy & Security -> Automation and allow your terminal to control " +
					"it, then try again.",
			)
		}
		if strings.Contains(stderrStr, "-1712") || strings.Contains(lowered, "timed out") {
			return "", newPermissionError(
				"The scripted app didn't respond to AppleScript in time (same permission prompt as " +
					"above, or it isn't running).",
			)
		}
		msg := stderrStr
		if msg == "" {
			msg = "osascript failed: " + err.Error()
		}
		return "", &AutomationError{Msg: msg}
	}
	return strings.TrimSpace(stdout.String()), nil
}

func escapeForAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
