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

`mycelium.OpenGhostty` does the equivalent for a bare Ghostty tab,
matching by working directory (Ghostty's `tty`/`pid` AppleScript
properties don't reliably work as of Ghostty 1.3.1; working directory
does).

## Usage

```go
import "github.com/luiul/mycelium"

result := mycelium.OpenVSCode("/Users/you/code/some-repo")
// result.OK, result.Message

result = mycelium.OpenGhostty("/Users/you/code/some-repo")
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
