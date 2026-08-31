package mycelium

import (
	"os/exec"
	"strings"
)

// gitToplevel returns the root of the git work tree containing dir, or ""
// when dir isn't inside one (not a repo, or git not installed — either way
// the nested-window match simply has nothing solid to work with, and fails
// closed rather than guessing from a raw path prefix).
func gitToplevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
