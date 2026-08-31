# Conventions

The decisions, conventions, and best practices shared by
[canopy](https://github.com/luiul/canopy),
[understory](https://github.com/luiul/understory), and this repo, stated
once so each project's own docs can stay about what that project does.

These are living rules. They came out of real bugs, real drift between
the two dashboards, and one consistency pass
([understory#4](https://github.com/luiul/understory/issues/4)) that
wrote them down. When a new decision applies to more than one repo, add
it here.

## Shared behavior lives in dashkit

The meta-convention behind everything else: anything both dashboards
need exists exactly once, here, never copied into both trees to quietly
drift apart. The app repos keep only domain specifics: their binding
tables, prompt texts, verbs, and the dispatch each confirmation runs.
Everything else (the modal state machine, the help overlay renderer,
table coloring, column resizing, window open-or-focus) is a dashkit
package both apps import.

This is also why dashkit is one repo rather than per-package repos:
every package in it serves the same two consumers for the same reason,
and the go.mod/tag/README overhead of splitting them bought nothing (see
History in the README).

## Keybindings

One set of keybinding conventions across both dashboards, so muscle
memory transfers:

- **Lowercase keys act on the selected row or are reversible**: `x`,
  `c`, `p`, `y`, `m`.
- **Uppercase keys are the bulk or stronger form of their lowercase
  sibling**: `X`, `C`, `D`, `P`, `M`.
- **No key means different things in the two apps.** When a collision is
  found, the minority usage moves (understory's prune moved `p` to `P`,
  copy moved `c` to `y`, vim's "yank"), the majority usage stays.
- **Reserved and identical everywhere**: `enter` is the primary action,
  `?` opens help, `r` refreshes, `q` quits, and `ctrl+c` always quits:
  from the table, from a modal, from the help overlay. The simplest
  possible invariant to state.
- **Verbs stay domain-correct** rather than forced-identical: `enter` is
  "open/focus" in understory (a worktree window) and "jump" in canopy (a
  session's window); canopy's `c` is "dismiss".
- Navigation is whatever `bubbles/table` provides, documented
  identically in both help overlays: `↑/↓, k/j` move, `pgup/pgdn, b/f`
  page, `u/d` half page, `g/G, home/end` top/bottom.

## The confirmation modal

Every destructive action asks first, and both apps ask the same way. The
state machine lives in [confirm](confirm/README.md); the discipline is:

- **`y` confirms; `n`, `esc`, or `enter` cancel; every other key is
  swallowed; `ctrl+c` quits.** Enter cancels because the prompt says
  `[y/N]`: the capitalized letter is the default answer, and enter
  selects it. Swallowing everything else means the state the prompt
  describes cannot shift under an in-flight answer: impossible to
  confirm or cancel by accident.
- **An unanswered prompt cancels itself after 10s**
  (`confirm.Timeout`), because rows keep repolling and reordering
  underneath it. The timeout notifies ("cancelled: no answer within
  10s"); an explicit cancel is silent.
- **Every poll revalidates the armed prompt's targets**
  (`confirm.Refresh`): targets that vanished from the fresh poll drop
  out, survivors are re-stamped with their fresh copies (a current
  uptime sample is what canopy's process-identity guard compares
  against), and a prompt left with nothing to act on cancels itself.
- **Color tiers**: yellow for the ordinary destructive action (`x`,
  `P`, `M`, `D`), red for the force tier (`X`).
- **The mouse is guarded**: drags are ignored while a prompt (or the
  help overlay) is open.
- **The payload and the prompt text stay with the caller.**
  `confirm.State[T]` holds the app's own type (a worktree batch plus
  removal kind, a process list plus signal); the package owns only the
  modal machinery around it.

## Phrasing

One voice across notifications, prompts, footers, and help text:

- **Notifications are terse lowercase fragments with no trailing
  period**, matching the voice the footers already use: "terminated pi
  (pid 86872)", "no stale worktrees to prune", "copied ~/worktrees/…".
  Result notifications may carry detail (signal names, `wt`'s own
  refusal reason, a hint at `X` when a dirty worktree was refused).
- **Prompts follow one template**: `<Verb> <target>? <Consequence
  sentence>. [y/N]`. Plain verbs in prompts, never signal names
  ("Terminate pi (pid 42, ~/path)? Currently working. [y/N]"); signal
  names stay in result notifications. One verb per operation, even
  across single and batch forms ("Prune" for both the single stale
  registration and the batch).
- **One user-facing word per action**, the same in the footer, the help
  overlay, and the README ("dismiss", not "complete" here and "mark
  seen" there). Code may keep its own domain term (canopy's code says
  "acknowledge").
- **The footer hint line lists only the few most-used bindings**, in a
  fixed format: `↑/↓ move · enter <verb> · <domain keys> · ? help · q
  quit`.

## The help overlay

`?` opens the full keybinding list, rendered by `loam.HelpView` in both
apps so the two cannot drift:

- The header stays visible (app identity), the body swaps to the list,
  the footer carries the close hint: "press any key to close".
- The title is "keybindings". Any key closes the overlay; `ctrl+c`
  quits (per the invariant above).
- Key-column padding is runewidth-aware (display width, not byte
  length), so the multi-byte `↑/↓` glyphs don't skew the description
  column.
- The list covers every binding `Update` handles, the full navigation
  set, and the one mouse-only interaction, verbatim in both apps: key
  "mouse", description "drag a column border on the header row to
  resize the two columns it joins". The `?` entry reads "this help";
  the quit entry is `q, ctrl+c`.
- Each app keeps only its own binding table (`[]loam.HelpBinding`) and
  the surrounding header/footer composition.

## CLI help text

Both `cmd/*/main.go` help texts use the same paragraph skeleton and the
same granularity (arrows, `enter`, the destructive key, `r`, `q`), and
point at `?` for the full list instead of duplicating it. The overlay
is the source of truth; the CLI text is a summary.

## Table rendering (loam)

Hard-won rendering rules, each with a bug behind it:

- **Never put ANSI-styled strings into `table.Row`.** bubbles/table
  v1's truncation (`runewidth.Truncate`) is not ANSI-aware: escape
  codes count as visible width and get sliced mid-sequence, corrupting
  the row. Post-process the table's already-rendered plain-text view
  instead (`loam.ColorizeRows`), so the table's own width math always
  runs over plain text.
- **The selected row is a full-width subtle background, not a leading
  marker column.** The row is marked with a zero-width Unicode sentinel
  (`loam.Tag`) in an always-populated, never-truncated column;
  `ColorizeRows` finds it in the rendered text (bubbles/table v1
  doesn't expose scroll offset), highlights the line, and strips the
  tag back out before returning.
- **Nested ANSI styles need their outer style reapplied.** Every
  lipgloss render ends with a full SGR reset, which kills any outer
  style wrapped around it; `loam.HighlightRow` reapplies its opening
  sequence after every inner reset it finds.
- **All width math is display width, never byte length**: runewidth for
  padding, `x/ansi.Cut` for slicing styled lines. Multi-byte runes (the
  `↑/↓` glyphs, truncation ellipses, unicode names) otherwise misalign
  every later column.
- **Header borders are drawn on the header row only**
  (`loam.DrawHeaderBorders`): data rows may already carry ANSI from
  coloring, the header never does, so marking only the header sidesteps
  the nesting hazard while giving mouse drags something visible to aim
  at.
- **`--no-color` / `NO_COLOR` yields plain text** in both apps.

## Column layout and resizing (trellis)

- **Columns are ordered by what you're scanning for, left to right**:
  canopy leads with urgency (State/Since: what needs you, and for how
  long) and ends with identifiers (Kind, PID, deliberately narrow);
  understory leads with identity and freshness (Repo/Branch/Created)
  and puts state (Worktree/Merge) just before the path.
- **One flex column absorbs whatever width the terminal leaves**
  (Location in canopy, Path in understory, both rightmost), dipping
  below its preferred floor on a tight terminal rather than letting the
  table overflow and clip the other columns.
- **Every other column's floor is its widest realistic value**, so a
  drag can truncate only the header title, never a value.
- **A drag trades width between exactly the two columns the border
  joins**; the table's total width never changes. No distant flex
  column secretly absorbs a drag (an earlier trellis version worked
  that way; it made one column's own border stop responding while every
  other border resized that one column instead of its neighbor).
- **A resize sticks across polls but resets on terminal resize.**
  Widths are a live UI decision, not model data: nothing persists them.
  The app keeps the user's overrides in its Model and reapplies them
  when polls rebuild the columns; a terminal resize recomputes the flex
  column from scratch anyway.
- **`trellis.Handle` needs the exact line index of the table's header
  row** (`originY`): count every line your `View` emits before it,
  including blank separators. An off-by-one makes every header drag
  silently miss.
- Mouse support requires `tea.WithMouseCellMotion()` on the program.

## State, polling, and async

- **Dashboard state is derived from shared, externally observable
  sources** (`ps`, `lsof`, `wt list`, git), not owned. The one
  exception (canopy's record of which `done` rows you've acknowledged)
  syncs across instances through the filesystem
  (`~/.pi/agent/canopy-status/acks/`), within one poll interval, with
  no daemon and no locking.
- **Merge each fresh poll against the previous one** so a single missed
  scan doesn't flicker rows away (canopy's `internal/registry`).
- **Every delayed self-message carries a token.** Arming or resolving
  increments the token; the tick is ignored unless it matches. This is
  how the confirmation timeout (`confirm.Msg`) and the notification
  auto-clear both avoid acting on stale state.
- **Read-only methods on state stored inside a Bubble Tea model use
  value receivers.** Models are passed around by value, and type
  assertions back out of `tea.Model` produce non-addressable copies;
  pointer-receiver reads are uncallable exactly where consumers need
  them.
- **A successful mutation refreshes immediately** rather than waiting
  for the next poll.
- **Poll intervals match the cost of the scan.** canopy polls every 2s
  (`ps` is cheap); understory every 15s (`wt list` runs several git
  subprocesses per worktree).

## Safety rules

- **Never signal a process without re-checking its identity first**:
  pid plus lifetime from a fresh `ps` snapshot, so a recycled pid is
  never signaled by mistake (canopy's `internal/kill`).
- **Window matching fails safe.** Ambiguity opens a new window rather
  than focusing an arbitrary one; generic branch names (`main`,
  `master`, `develop`, `trunk`) never match by branch alone; "inside
  the same worktree" is decided by `git rev-parse --show-toplevel`
  roots, not path prefixes (mycelium).
- **Fail loudly at startup, not silently later**: canopy checks for
  macOS and exits with a clear error anywhere else; a failed process
  scan shows a warning banner rather than looking identical to "no
  sessions".
- **Smoke tests never confirm a destructive prompt against live
  sessions.** Only the cancel paths get exercised against the real
  machine.

## Testing

- **External calls sit behind package-level function variables**
  (`killProcess`, `openVSCode`, the scan functions), swapped out in
  tests so keybind flows run without signaling real processes or
  opening real windows.
- **State machines are tested through `Update` with synthetic
  messages**, including the unpleasant cases: stale tokens, swallowed
  keys, `ctrl+c` from inside a modal, targets vanishing mid-prompt.
- **TUIs are smoke-tested headlessly in tmux**: launch the real binary
  in a detached session, send keys with `send-keys`, assert on
  `capture-pane` output.
- Every repo runs `go build`, `go vet`, `go test`, and `gofmt -l .`;
  canopy adds `-race` and `golangci-lint` (`make check`).

## Code organization and documentation style

- **One Go package per concern** under `internal/`; `cmd/` is a thin
  entry point (flags, version).
- **Doc comments explain why, not what.** Package docs say what the
  package is for and what deliberately isn't in it; each split-out
  file's doc says why it's split out.
- **READMEs document user-facing conventions and deliberate
  omissions**; deeper design docs live in `docs/` (canopy's
  `docs/agent-state-machine.md` is the model).
- **Multi-step work is planned and tracked as GitHub issues**, not repo
  markdown (understory#4, the consistency pass these conventions came
  out of, is the canonical example).

## Releasing dashkit

- **Semver tags; consumers pin via go.mod** and bump deliberately (`go
  get github.com/luiul/dashkit@vX.Y.Z`).
- **Never move or delete a tag once it may have been fetched.**
  proxy.golang.org caches versions immutably at first fetch, so a moved
  tag permanently mismatches proxy versus direct fetches. Fixes ship as
  a new patch version (v0.4.0 was fetched within minutes of tagging and
  had to become v0.4.1).
- **Package names**: garden metaphors for the primitives (loam,
  trellis, mycelium), plainly descriptive when the package is exactly
  what it says (confirm).
- **Every package has its own README** with a "why this needs to exist"
  section, and stays self-contained and independently importable.

## Deliberate omissions

Things consciously not built, so nobody "fixes" their absence:

- **understory has no notion of "live"** (an agent currently working in
  a worktree): that process-discovery machinery is canopy's, and
  duplicating it would recouple two otherwise independent tools.
- **canopy is same-machine, same-user, macOS-only**, and keyboard-only
  (no row click; bubbles/table doesn't ship row-click handling).
- **Column widths are never persisted**: a resize is a live UI
  decision, and the next terminal resize recomputes the flex column
  anyway.
- **No new features to fix a convention problem.** The consistency pass
  explicitly excluded adding features (a clipboard copy in canopy,
  say); conventions first, features on their own merits.
