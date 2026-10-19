# mycelium

A tiny Go library that opens, or focuses if one is already open, an app
window (VS Code or a Ghostty terminal) on a given filesystem path,
without ever risking a duplicate window for a path that's already open
somewhere.

This is the underground layer shared by [canopy](https://github.com/luiul/canopy)
(jump to whichever window is actually running a given agent) and
[understory](https://github.com/luiul/understory) (open or focus a
worktree on Enter): both need the exact same "is a window already open
on this path? raise it, otherwise open a genuinely new one" behavior,
backed by the same window detection, so it lives here once instead of
being duplicated in both trees.

## The open-or-focus flow at a glance

![Flowchart of mycelium's title-based open-or-focus, shared by VS Code and Ghostty: canopy and understory hand over a path (and a tty for Ghostty-surfaced agents); the library lists window titles per app via System Events, parses the work-tree root out of each title, prefers a tty-bearing window when the caller knows one and then matches exact, work-tree root, and nested; focuses a match with AXRaise, retries the listing once against macOS accessibility culling, and otherwise opens a genuinely new window](diagrams/open-or-focus.drawio.svg)

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
and focuses the matched window directly, so the CLI is only ever asked
to *open*: once "no window is open on path" is established, `code -n
path` forces a genuinely new window, and `--reuse-window` (the call
that hijacks) is never made. That makes it safe to call repeatedly on
the same never-before-seen path: the already-open check finds the
window `OpenVSCode` itself just created on every subsequent call, so
nothing stacks up duplicate windows.

## How windows are identified: titles, matched as paths

Window identity is the window title. The dotfiles
[`window.title`](https://github.com/luiul/dotfiles/blob/main/vscode/README.md) setting is
`${rootPath}${separator}${activeRepositoryBranchName}`: the opened
folder's full path, `~`-shortened, then the branch. One osascript call
(System Events, process `Code`, `name of every window`) lists the
titles; each is split on the first ` — ` and a leading `~` expanded.
Identity is that path portion; the branch portion is never matched, so
the phantom-branch failure class of the old basename-plus-branch title
grammar (a main-checkout window whose SCM view activated a worktree's
repo) is impossible by construction. The setting is a load-bearing
dependency, not cosmetic.

Matching runs three stages, strongest signal first:

1. **Exact**: a window's title path is path itself.
2. **Work-tree root**: a window's title path is the root of path's git
   work tree. The path handed over can sit *inside* a checkout (canopy
   passes the agent's cwd as-is, e.g. a monorepo package), and a window
   open on the root is scoped to the exact tree path lives in.
3. **Nested**: a window's title path sits inside path, on a path
   element boundary (`/wt-a` never matches `/wt-a-b`). This is the
   "window open on a subpackage of the worktree" case.

Focus is then an `AXRaise` of the exact window whose title matched
(activating the app first, since a window can't be raised above other
apps' windows until its own app is). Identification and focus bind to
the same window, with no intermediary re-matching: the property the
CLI-based designs lacked
([luiul/dashkit#14](https://github.com/luiul/dashkit/issues/14)).

One listing can lie: macOS culls the accessibility tree of a
backgrounded app, so System Events can report fewer windows than exist
([luiul/dashkit#9](https://github.com/luiul/dashkit/issues/9)). On the
`OpenVSCode` path, when Code is running but the listing is empty or
matchless, Code is activated (which re-materializes the tree, and focus
is moving to Code anyway) and the listing re-run once before a new
window is opened.

Accepted limitations: multi-root windows title on their
`.code-workspace` file path, not member folders, so they match only on
that path. And a window that just changed folders keeps its old title
briefly; Enter during that window can open one duplicate, self
correcting on the next title update.

`mycelium.OpenGhostty` does the equivalent for a Ghostty window,
matching by window title: the dotfiles zsh title hook
(`~/.zsh_config/title.zsh`) renders every surface's title as the git
work-tree root plus the branch, the same grammar VS Code's window.title
renders, and the session-title pi extension re-asserts that grammar
while a pi agent runs (pi's own "π - basename" title carries no path),
appending the surface's tty. Titles are the strongest working identity:
Ghostty's `tty`/`pid` AppleScript properties don't work as of Ghostty
1.3.1, and the previous `working directory` match was weak (two
surfaces in the same directory are indistinguishable, and a stale
value focuses the wrong window or opens a duplicate). The tty segment
closes the remaining ambiguity: two agents side by side in the same
repo on the same branch have identical path components, and
`OpenGhostty(tty, path)` raises the window whose title carries the
caller's tty (canopy knows each agent's tty from ps). Without a tty,
or when no tty-bearing window matches, matching falls back to the path
alone. Accepted gap: System Events reports one title per window (its
focused surface), so a surface in a background tab of another window
isn't listed by its own title; a miss opens a new window, never a
hijack.

`mycelium.SnapshotVSCode` and `mycelium.SnapshotGhostty` are the
read-only half of all this: one queryable snapshot of one app's open
windows, for dashboards that want to *show* per-row window state
(understory's "Ghostty open?" column) rather than act on one selected
row. The titles are listed once per snapshot, git work-tree-root
lookups are memoized across queries, and each `IsOpen(path)` runs the
exact same match the matching Open* function's already-open check
runs, so a column built on it says "open" precisely when Enter would
focus an existing window instead of opening a new one. A poll never
activates an app (focus isn't moving, so stealing it would be a bug):
a failed listing, or an empty listing while the app runs, is reported
by `Err()` and makes every `IsOpen` false, so callers render "can't
tell" rather than "definitely not open". `IsOpenOnWorktree(path)` is
the strict variant for destructive-action prompts: a pure folder-path
match, phantom-immune by construction.

## Usage

```go
import "github.com/luiul/dashkit/mycelium"

result := mycelium.OpenVSCode("/Users/you/code/some-repo")
// result.OK, result.Message

result = mycelium.OpenGhostty("ttys034", "/Users/you/code/some-repo")
// pass "" as the tty for path-only matching

// Read-only per-row queries, one window listing per poll:
snapshot := mycelium.SnapshotVSCode()
if snapshot.Err() != nil {
	// can't tell; render "?", not "not open"
}
open := snapshot.IsOpen("/Users/you/code/some-repo")
```

Both currently macOS-only: window detection shells out to `osascript`
(System Events for both apps), and there is no equivalent
implementation for other platforms yet. Scripting System Events requires macOS Automation
permission for the calling terminal (System Settings → Privacy &
Security → Automation), so the apps embedding this need a stable
codesign identity to keep the grant across rebuilds.

## Errors

A failed AppleScript call surfaces as `*mycelium.AutomationError`, or
more specifically `*mycelium.AutomationPermissionError` when it looks
like macOS's Automation permission for scripting the target app hasn't
been granted yet (System Settings → Privacy & Security → Automation).
`Result.Message` is already a human-readable rendering of either, meant
to be shown straight to a user (e.g. as a TUI notification): callers
don't need to inspect the error types themselves unless they want to
branch on them.

## License

MIT
