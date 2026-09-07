# mycelium

A tiny Go library that opens, or focuses if one is already open, an app
window (VS Code or a Ghostty terminal) on a given filesystem path —
without ever risking a duplicate window for a path that's already open
somewhere.

This is the underground layer shared by [canopy](https://github.com/luiul/canopy)
(jump to whichever window is actually running a given agent) and
[understory](https://github.com/luiul/understory) (open or focus a
worktree on Enter): both need the exact same "is a window already open
on this path? raise it — otherwise open a genuinely new one" behavior,
backed by the same window detection, so it lives here once instead of
being duplicated in both trees.

## Why this needs to exist at all

`code --reuse-window <path>` alone isn't enough to get real
switch-or-create behavior out of the `code` CLI: it only reuses the
right window when one already has that exact folder open, and silently
hijacks whichever window was last active otherwise, rather than opening
a fresh one. Confirmed both empirically and in upstream reports
([microsoft/vscode#121926](https://github.com/microsoft/vscode/issues/121926),
[#216602](https://github.com/microsoft/vscode/issues/216602),
[#215749](https://github.com/microsoft/vscode/issues/215749)).

`mycelium.OpenVSCode` checks for an already-open window itself first
and only ever falls through to the CLI once that's ruled out — forcing
a genuinely new window (`-n`) instead of handing `--reuse-window` a
chance to guess wrong. That makes it safe to call repeatedly on the
same never-before-seen path: the already-open check finds the window
`OpenVSCode` itself just created on every subsequent call, so nothing
stacks up duplicate windows.

## How windows are identified: the registry

The primary source of window identity is the window registry: every VS
Code window self-registers into `~/.local/state/vscode-windows/` via
the small [vscode-window-registry](../vscode-window-registry/README.md)
extension (one JSON file per window: session id, workspace folders,
workspace file, heartbeat every 5s). Matching is then exact folder
paths, strongest signal first:

1. **Exact**: a window has path itself open as a folder.
2. **Work-tree root**: a window has the root of path's git work tree
   open. The path handed over can sit *inside* a checkout (canopy
   passes the agent's cwd as-is, e.g. a monorepo package), and a window
   open on the root is scoped to the exact tree path lives in.
3. **Nested**: a window has a folder open inside path, on a path
   element boundary (`/wt-a` never matches `/wt-a-b`). This is the
   "window open on a subpackage of the worktree" case.

Focus is then `code --reuse-window <folder>` (or the window's
`.code-workspace` file for a multi-root window, which the CLI keys on
the workspace rather than on individual folders). The call is safe
precisely because the registry removes the miss-case uncertainty
described above.

What this fixes over the old title parsing: identity is a path, never
SCM state or a basename, so same-named worktrees and phantom branches
stop existing as problems. Multi-root windows record every folder (a
title carries one root). Nothing depends on the `window.title` setting.
And polls read small JSON files instead of paying an osascript
round-trip.

## The fallback: window titles over AppleScript

When the registry cannot answer (the extension is not installed, or no
window has activated it yet, so there are no fresh entries), mycelium
runs the previous mechanism unchanged: list windows over AppleScript
(System Events), match titles against the ecosystem's `window.title`
grammar (folder basename, then branch, then the active file), with a
focused-file nested match via each window's `AXDocument` accessibility
attribute. "Inside the same worktree" on that path is decided by
comparing git work-tree roots (`git rev-parse --show-toplevel`), not by
raw path prefix.

Every fallback use appends one line to
`~/.local/state/vscode-windows/fallback.log` (reason plus path). That
log is the data source for deleting the fallback entirely: once daily
use produces zero fallback lines for weeks, the title cascade is dead
code in practice. Fallback uses from `SnapshotVSCode` polls are not
logged: a poll runs every few seconds, so logging them would bury the
actionable signal under "VS Code is closed" noise.

On the fallback path, callers that know which branch the path is on
(understory always does) get two stronger title matches:

- **Windows are matched on folder name + branch together.** This
  ecosystem's worktree layout gives every worktree of a repo the same
  leaf folder name as the repo itself, so the basename alone can never
  tell "tardis-community — main" (the main checkout) apart from
  "tardis-community — patch/ISA-…" (a branch worktree). With a known
  branch, a same-named window on a *different* branch is rejected rather
  than focused.
- **A nested window with no file focused is still found.** `AXDocument`
  tracks the focused *file*, not the workspace folder, so a window
  sitting on the Explorer or an empty editor group reports no path at
  all. Its title still carries the branch, though
  (`scm-analytics-engineers — patch/ISA-…`), and a branch is checked out
  in at most one worktree of a repo at a time — so as a last resort
  before opening a new window, a window is matched by the branch
  component of its title alone. This fails safe on two guards: generic
  branch names (`main`, `master`, `develop`, `trunk`) never match, and
  the branch must be carried by exactly one distinct window title
  (ambiguity falls through to opening a new window rather than focusing
  an arbitrary one).

`mycelium.OpenGhostty` does the equivalent for a bare Ghostty tab,
matching by working directory (Ghostty's `tty`/`pid` AppleScript
properties don't reliably work as of Ghostty 1.3.1; working directory
does).

`mycelium.SnapshotVSCode` is the read-only half of all this: one
queryable snapshot of which VS Code windows are open, for dashboards
that want to *show* per-row window state (canopy's and understory's "VS
Code open?" columns) rather than act on one selected row. The window
source is captured once per snapshot (registry first, AppleScript
listing on fallback), git work-tree-root lookups are memoized across
queries, and each `IsOpen(path, branch)` runs the exact same match
`OpenVSCode`'s already-open check runs — so a column built on it says
"open" precisely when Enter would focus an existing window instead of
opening a new one. A listing failure (most likely the macOS Automation
permission not granted yet) is reported by `Err()` and makes every
`IsOpen` false, so callers render "can't tell" rather than "definitely
not open". `IsOpenOnWorktree(path, branch)` is the strict variant for
destructive-action prompts: on the registry path it is a pure
folder-path match (no branch, no title, phantom-immune), on the
fallback it drops the branchless weak title match.

## Usage

```go
import "github.com/luiul/dashkit/mycelium"

// Second argument is the branch the path is on, or "" when unknown.
// Advisory: only the title fallback uses it; the registry matches on
// folder paths alone.
result := mycelium.OpenVSCode("/Users/you/code/some-repo", "main")
// result.OK, result.Message

result = mycelium.OpenGhostty("/Users/you/code/some-repo")

// Read-only per-row queries, one listing per poll:
snapshot := mycelium.SnapshotVSCode()
if snapshot.Err() != nil {
	// can't tell; render "?", not "not open"
}
open := snapshot.IsOpen("/Users/you/code/some-repo", "main")
```

Both currently macOS-only: the registry itself is portable (the
extension and the reader are plain files), but the title fallback and
Ghostty's window detection shell out to `osascript`, and there is no
equivalent implementation for other platforms yet.

## Errors

A failed AppleScript call surfaces as `*mycelium.AutomationError`, or
more specifically `*mycelium.AutomationPermissionError` when it looks
like macOS's Automation permission for scripting the target app hasn't
been granted yet (System Settings → Privacy & Security → Automation).
`Result.Message` is already a human-readable rendering of either, meant
to be shown straight to a user (e.g. as a TUI notification) — callers
don't need to inspect the error types themselves unless they want to
branch on them.

## License

MIT
