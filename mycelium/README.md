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
backed by the same AppleScript window detection, so it lives here once
instead of being duplicated in both trees.

## Why this needs to exist at all

`code --reuse-window <path>` alone isn't enough to get real
switch-or-create behavior out of the `code` CLI: it only reuses the
right window when one already has that exact folder open, and silently
hijacks whichever window was last active otherwise, rather than opening
a fresh one. Confirmed both empirically and in upstream reports
([microsoft/vscode#121926](https://github.com/microsoft/vscode/issues/121926),
[#216602](https://github.com/microsoft/vscode/issues/216602),
[#215749](https://github.com/microsoft/vscode/issues/215749)).

`mycelium.OpenVSCode` checks for an already-open window itself first,
via each window's title over AppleScript (System Events), and only ever
falls through to the CLI once that's ruled out — forcing a genuinely new
window (`-n`) instead of handing `--reuse-window` a chance to guess
wrong. That makes it safe to call repeatedly on the same
never-before-seen path: the already-open check finds the window
`OpenVSCode` itself just created on every subsequent call, so nothing
stacks up duplicate windows.

The path handed over can also sit *inside* a checkout rather than at
its root (canopy passes the agent's cwd as-is, e.g. a monorepo package
the agent runs in). When nothing is open on the exact path, the
checkout's work-tree root gets a second exact-folder title match, so a
window open on the checkout as a whole is still reused rather than a
redundant new one opened next to it.

If nothing is open on the exact path or its work-tree root, but some
other window already has a file focused inside its git work tree — e.g.
a monorepo subpackage opened directly as its own window — that window
is reused too, via each window's `AXDocument` accessibility attribute
(the same one behind VS Code's title-bar proxy icon/breadcrumb), rather
than opening a second, redundant window on the same tree. "Inside" is decided by comparing git
work-tree roots (`git rev-parse --show-toplevel`), not by raw path
prefix: a window scoped to an unrelated folder that merely has a file
inside the target focused right now (a `~/scratch` note, say) must not
claim the target, and neither side ever matches when it isn't inside a
work tree at all.

Callers that know which branch the path is on (understory always does)
can pass it along, and matching gets two upgrades on top of that:

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
listing is captured once per snapshot, git work-tree-root lookups are
memoized across queries, and each `IsOpen(path, branch)` runs the exact
same match cascade `OpenVSCode`'s already-open check runs — so a column
built on it says "open" precisely when Enter would focus an existing
window instead of opening a new one. A listing failure (most likely the
macOS Automation permission not granted yet) is reported by `Err()` and
makes every `IsOpen` false, so callers render "can't tell" rather than
"definitely not open".

## Usage

```go
import "github.com/luiul/dashkit/mycelium"

// Second argument is the branch the path is on, or "" when unknown.
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

Both currently macOS-only: window detection shells out to `osascript`.
Ghostty's own scripting dictionary and VS Code's `System Events` window
titles are both macOS-specific; there's no equivalent implementation for
other platforms yet.

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
