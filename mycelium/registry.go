package mycelium

// This file is the read side of the VS Code window registry: every
// window self-registers into ~/.local/state/vscode-windows/ via the
// vscode-window-registry extension (see dashkit's top-level
// vscode-window-registry/ directory for the writer and the contract),
// and OpenVSCode/VSCodeSnapshot match against those records by exact
// folder path instead of parsing window titles over AppleScript. The
// title cascade stays as the fallback for when the registry cannot
// answer (extension not installed, no window activated yet); every
// fallback use is logged to fallback.log in the same directory, which
// is the data source for deciding when the fallback can be deleted.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// registryStaleness is how long an entry stays trusted without a
// heartbeat. The extension rewrites its file every 5s, so 30s tolerates
// several missed beats (a busy extension host, a throttled timer) while
// still forgetting a closed window quickly. Window close cannot be
// relied on to delete the entry (deactivate is best effort), so this
// pruning is the cleanup, the same pattern canopy-status uses.
const registryStaleness = 30 * time.Second

// registryEntry is one window's self-registration record. Folders holds
// every workspace folder (multi-root windows record all of them; a
// window with no folder open records none and simply never matches).
// WorkspaceFile is the .code-workspace path for a multi-root window,
// "" otherwise: `code --reuse-window` keys on the workspace rather than
// on individual folders, so a multi-root window is focused through its
// workspace file.
type registryEntry struct {
	SessionID     string   `json:"sessionId"`
	Folders       []string `json:"folders"`
	WorkspaceFile string   `json:"workspaceFile"`
	UpdatedAt     string   `json:"updatedAt"`
}

// focusTarget is what `code --reuse-window` should be handed to focus
// this entry's window: the workspace file when the window has one (see
// registryEntry), else the matched folder.
func (e registryEntry) focusTarget(folder string) string {
	if e.WorkspaceFile != "" {
		return e.WorkspaceFile
	}
	return folder
}

// registryDir is the directory the extension writes into. "" when the
// home directory is unknown, which readRegistry reports as unavailable.
func registryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "vscode-windows")
}

// readRegistry lists the fresh registry entries under dir. ok is false
// when the directory cannot be read at all (extension not installed,
// most likely): the caller falls back to the title cascade. Individual
// files that are stale (mtime older than registryStaleness), unreadable,
// or unparseable are skipped, never fatal: a torn write or a foreign
// file must not take window detection down. ok stays true when every
// entry is stale or the directory is empty; "the registry answered, and
// nothing is fresh" is a different situation from "no registry", and
// callers log them under different reasons.
func readRegistry(dir string, now time.Time) (entries []registryEntry, ok bool) {
	if dir == "" {
		return nil, false
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > registryStaleness {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		var e registryEntry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, true
}

// matchRegistry finds a window that has path open, over fresh registry
// entries, strongest signal first:
//
//  1. exact: one of the window's folders is path itself.
//  2. work-tree root: one of the window's folders is the root of the
//     git work tree path lives in. path can be a subdirectory of a
//     checkout (canopy hands over the agent's cwd as-is), and a window
//     open on the root is scoped to the exact tree path lives in.
//  3. nested: one of the window's folders sits inside path, on a path
//     element boundary ("/wt-a" must not match "/wt-a-b"). This is the
//     "window open on a subpackage of the worktree" case.
//
// Returns the focus target for `code --reuse-window` (see focusTarget).
// The same folder open in two windows matches the first one registered;
// either focus is correct.
func matchRegistry(entries []registryEntry, path string, toplevel func(string) string) (string, bool) {
	for _, e := range entries {
		for _, f := range e.Folders {
			if f == path {
				return e.focusTarget(f), true
			}
		}
	}
	if root := toplevel(path); root != "" && root != path {
		for _, e := range entries {
			for _, f := range e.Folders {
				if f == root {
					return e.focusTarget(f), true
				}
			}
		}
	}
	prefix := path + string(filepath.Separator)
	for _, e := range entries {
		for _, f := range e.Folders {
			if strings.HasPrefix(f, prefix) {
				return e.focusTarget(f), true
			}
		}
	}
	return "", false
}

// matchRegistryOnWorktree is the strict registry match for
// destructive-action warnings (understory's and coppice's remove
// prompts): a window counts only when one of its folders is the
// worktree at path itself or sits inside it, so deleting the worktree
// would genuinely strand that window. No branch, no title: identity is
// the folder path, which makes the phantom-branch class of false
// positives (a main-checkout window whose SCM view has the worktree as
// its active repository) impossible by construction.
func matchRegistryOnWorktree(entries []registryEntry, path string) bool {
	prefix := path + string(filepath.Separator)
	for _, e := range entries {
		for _, f := range e.Folders {
			if f == path || strings.HasPrefix(f, prefix) {
				return true
			}
		}
	}
	return false
}

// logRegistryFallback appends one line to fallback.log in the registry
// directory every time a caller had to use the AppleScript title
// cascade because the registry could not answer. The log is the data
// source for the decision to delete the title cascade: once daily use
// produces zero fallback lines for weeks, the fallback is dead code in
// practice. Best effort by design: observability must never break or
// slow the action it observes, so every failure is swallowed. The
// directory is created if missing so the "extension not installed at
// all" case is recorded too.
func logRegistryFallback(dir, reason, path string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "fallback.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), reason, path)
}
