# vscode-window-registry

A tiny VS Code extension, plain JavaScript with no build step. Every
window writes one small JSON file into `~/.local/state/vscode-windows/`
and rewrites it every 5 seconds. External tools read that directory to
learn which window has which folder open.

## Why this needs to exist

Tools like [canopy](https://github.com/luiul/canopy),
[understory](https://github.com/luiul/understory), and coppice need to
know which VS Code window has a given worktree open. Before this
extension they parsed window titles (`folder — branch — editor`) over
AppleScript. That is weak: every worktree of a repo shares the repo's
leaf folder name, the branch component can be missing or phantom, a
window with no file focused reports no path at all, and the whole thing
depends on a user-level `window.title` setting staying in effect.
Identity was never ground truth.

The registry makes identity an exact folder path instead. Readers match
paths, then focus with `code --reuse-window <folder>`. That CLI call is
only safe because the registry removes the miss case: on a miss,
`--reuse-window` hijacks whichever window was last active
(microsoft/vscode#121926).

The decisive design rule: **the writer must see every window.** A
registry written by the opening tools would only know about tool-opened
windows. A window opened by hand (`code .`, File > Open, the recents
list) would be invisible, the "already open?" check would say no, and a
duplicate window would open. That is the exact bug this feature exists
to prevent. So the writer is an extension, and an extension runs in
every window no matter how it was opened.

## The contract

One file per window: `~/.local/state/vscode-windows/<sessionId>.json`.
`vscode.env.sessionId` is unique per window instance.

```json
{
  "sessionId": "…",
  "folders": ["/abs/path/one", "/abs/path/two"],
  "workspaceFile": "/abs/path/workspace.code-workspace",
  "updatedAt": "RFC3339"
}
```

- `folders`: every workspace folder (`workspaceFolders[*].uri.fsPath`).
  Multi-root windows record all of them. A window with no folder open
  writes `[]` and simply never matches.
- `workspaceFile`: the `.code-workspace` path for multi-root windows,
  else `null`. `code --reuse-window` keys on the workspace, not on
  individual folders, so multi-root windows are focused through this
  file.
- Freshness: the file is rewritten every 5s and on every workspace
  folder change. Readers drop entries whose mtime is older than 30s.
  Window close cannot be relied on to delete the file (`deactivate` is
  best effort), so staleness pruning is the cleanup.
- Writes are atomic: temp file, then rename. Readers never see torn
  JSON.

The readers live in [../mycelium](../mycelium/README.md) (Go) and in
coppice's `vscode.py` (Python). Both treat the registry as the primary
source and keep the old title matching as the fallback for when the
registry is missing or has no fresh entries.

## Install

Sideload this directory into VS Code's extensions folder, then restart
Code:

```bash
ln -s "$PWD" ~/.vscode/extensions/luiul-window-registry
```

(A copy works too; a symlink tracks this repo automatically.) Verify
after restart:

```bash
ls ~/.local/state/vscode-windows/   # one <sessionId>.json per window
```

Packaging a `.vsix` and running `code --install-extension` on it is the
alternative if sideloading ever proves unreliable across a VS Code
update.

## License

MIT
