# Tool conventions

TLDR: Build small tools with clear boundaries, predictable behavior, and safe defaults. Keep entry points thin, collect data before rendering, measure performance, and test without disturbing live work. Share proven primitives, not whole application frameworks.

## Scope and use

This is the shared guide for the tools in the `luiul` GitHub account, including [dashkit](https://github.com/luiul/dashkit), [canopy](https://github.com/luiul/canopy), [understory](https://github.com/luiul/understory), and [Coppice](https://github.com/luiul/coppice).
Apply the general rules everywhere. Apply dashboard and language rules only where they fit.
These are review criteria, not a claim that every current tool already meets them.

- Read the relevant sections before changing code. Keep existing behavior unless the task explicitly changes it.
- Record project-specific behavior and deliberate exceptions in that project's docs. Do not silently weaken safety rules.
- Keep this file about decisions. Put API details, examples, and bug history in package READMEs or `docs/`, then link here.
- Follow the [global agent instructions](https://github.com/luiul/dotfiles/blob/main/pi/.pi/agent/AGENTS.md) for approval, worktrees, publication, and deployment. This guide does not grant those permissions.

## Code structure

- **One clear responsibility per module or package.** Split by reasons to change, not an arbitrary line limit. Avoid catch-all utility modules and unnecessary layers.
- **Thin entry points.** Parse arguments and wire dependencies there. Put command workflows in cohesive handlers, not in argument declarations or renderers.
- **Separate collection, decisions, presentation, and mutations.** Collect external data once per needed target. Apply state rules to prepared values. Rendering must not launch commands or walk directories.
- **Share stable primitives.** Go dashboards use dashkit for confirmation, filtering, rendering, layout, and window helpers. Keep domain bindings, prompt text, selection rules, and action dispatch in the apps.
- **Do not force cross-language reuse.** Python tools follow the same behavior contracts without importing a Go library. Share vocabulary and tests where useful, not duplicated implementation assumptions. When a port is unavoidable, name its source of truth and pin the shared contract with fixtures.
- **Use explicit boundary types.** Validate external replies. Distinguish missing data, failed checks, and empty results. Keep internal types small; avoid a model hierarchy without a concrete need.
- **Keep dependencies pointing inward.** State rules must not import CLI or dashboard code. External integrations should not depend on presentation.

For Go apps, `cmd/` is thin and application-only packages belong under `internal/`. Dashkit's public packages remain independently importable.
For new packaged Python CLIs, prefer a `src/` layout. Preserve established layouts unless a measured or maintenance need justifies migration. Keep parser definitions separate from substantial workflows. Use `uv` for Python operations.

## Safety and external state

- **Uncertainty is not success.** A failed scan must not look like an empty result. Refuse a destructive action when a required identity or preservation check fails.
- **Revalidate before acting.** Use stable identity, not a row index, basename, title substring, or PID alone. Check the target's current eligibility after confirmation.
- **Preserve work.** Protect dirty, parked, current, and protected worktrees according to the command's contract. Review valuable ignored files before removal. Force and yes flags do not bypass mandatory guards.
- **Respect ownership.** For repos managed by Worktrunk (`wt`), `wt` owns worktree paths, lifecycle, and hooks. Orchestrators own agent sessions. Never replace those owners with a second competing implementation.
- **Bound external work.** Set deadlines, cap concurrency, and define cancellation and child cleanup. Ctrl-C must stop the tool promptly, with a bounded cleanup. Every background task needs an owner and an explicit shutdown or expiry rule.
- **Keep reads honest.** Read commands avoid writes. If a read self-heals state, such as pruning a stale registry entry, document it and report it. A dry run changes no worktree, branch, or setting. If it refreshes remote-tracking refs to preview accurately, document that cache effect.
- **Fail loudly at startup.** Check the platform and required tools first, then exit with a clear error. A failed scan shows a warning, never an empty result.
- **Persist only owned state.** Examples include preferences, acknowledgements, and retry records. Make writes safe under interruption and concurrent access. Do not treat cached observations as fresh proof for destructive actions.

Process actions check PID and start time or lifetime against a fresh snapshot.
Worktree identity is its path; branch is a ref, not a location. Resolve the caller's current worktree independently of a subprocess's working directory.
Window actions bind identification and focus to the same verified window. Never call `code --reuse-window`; it can focus an unrelated window.
Canopy and Understory open or focus Ghostty. Preserve their existing opener and title matching fallback; changing matching is not convention cleanup. See the [dashkit README](README.md) for the current window APIs.

## CLI contracts

- Keep names, flags, defaults, and exit codes stable. Exit zero only on success. Exit non-zero on failure, including partial failure. New behavior must not quietly change an existing command's scope.
- Primary results go to stdout. Errors and progress go to stderr. Machine output must contain only the documented data, without prompts, spinners, ANSI styles, or extra prose.
- Offer machine output when callers need it. Treat its fields and meaning as a compatibility contract; validate changes with fixtures.
- Prompt only when stdin is a terminal. Otherwise require explicit options or return a clear error; never hang on a prompt. Keep real terminal input separate from piped data.
- Explain destructive scope and consequences before confirmation. Cancellation must be visible and must not perform the action.
- Report partial failure honestly. A created worktree with a failed handoff is not a completed handoff. Give a safe retry when possible.
- Disable color when the output stream is not a terminal, `TERM=dumb`, or `NO_COLOR` is set and non-empty. Explicit user color options can override these defaults. `NO_COLOR` disables color, not every ANSI attribute. A tool's `--no-color` option must honor its documented promise. Machine output is always unstyled.
- Measure terminal display cells, not bytes or string length. Use a maintained width library (Go: `go-runewidth` and `x/ansi`; Python: Rich `cell_len` or `wcwidth`), not hand-built tables, and never split a grapheme cluster. Truncate paths toward their identifying tail. Match and filter full values, not shortened labels.

Reference: [Command Line Interface Guidelines](https://clig.dev/) and the [NO_COLOR contract](https://no-color.org/).

## Language and documentation

- Use simple words, short active sentences, and one idea at a time. No filler or punctuation dashes. Do not hard-wrap Markdown prose at a fixed column.
- Use one user-facing word per action across help, output, and docs. Each repo keeps a `docs/glossary.md` and uses its terms verbatim. Say "open or focus" for windows; reserve "reuse" for VS Code's own `--reuse-window` flag.
- Start substantial reports and guides with `TLDR:`. Omit empty sections. Separate required next steps from optional ideas.
- Dashboard notifications are lowercase fragments without trailing periods, such as "cancelled" or "no stale worktrees to prune". CLI prose can use complete sentences.
- Confirmation prompts state the action, target, and consequence: `<Verb> <target>? <Consequence>. [y/N]`. Keep result details separate from the question.
- API docs explain behavior, inputs, errors, side effects, and concurrency guarantees where relevant. Internal comments explain non-obvious reasons and constraints, not a line-by-line translation of code.
- READMEs cover usage and deliberate omissions. Package READMEs cover responsibilities and examples. Use full clickable URLs for every online resource, issue, and PR, never a bare `repo#12`.
- Diagrams follow the [dotfiles diagram convention](https://github.com/luiul/dotfiles/blob/main/docs/diagrams.md): one `.drawio.svg` per diagram with its source embedded. Never paste raw draw.io XML or Mermaid into Markdown.
- Approved multi-step personal work belongs in GitHub issues with verifiable acceptance criteria, not root-level plan files. Tracker creation and commits still need the approvals in the global instructions.

## Dashboard interaction

These rules apply to interactive row-based dashboards, not to every CLI.

- Keep shared keys consistent: `enter` is the primary row action, `?` help, `r` refresh, `q` quit, and `/` filter. `ctrl+c` quits from every mode.
- Lowercase action keys normally affect one row or a reversible action. Uppercase keys are the bulk or stronger form where that relationship exists.
- No shared action key means different operations across Canopy and Understory. Resolve collisions across apps, not just within one. The primary row action keeps its domain-specific verb.
- Keep verbs domain-correct. Opening a worktree and jumping to a session are related primary actions, not identical operations.
- Filter input consumes printable keys as query text. `/` seeds the applied query; `enter` retains the primary action; `esc` leaves input without clearing, then clears the filter in normal mode. Matching lives in [sieve](sieve/).
- Document the complete navigation set supported by the pinned table dependency. Footer hints show only common actions: `↑/↓ move · enter <verb> · <domain keys> · ? help · q quit`.
- Help uses the shared renderer and each app's binding table. Keep the header visible, title it "keybindings", and close on any key except `ctrl+c`, which quits.

Destructive dashboard actions use [confirm](confirm/README.md):

- `y` confirms. `n`, `esc`, and `enter` cancel. Other keys are swallowed; `ctrl+c` quits. Unanswered prompts cancel after `confirm.Timeout` (currently 10 seconds).
- Revalidate armed targets on each poll and immediately before mutation. Cancel if no eligible targets remain. Ignore table drags while confirmation or help is open.
- Every resolution reports a result. Use the shared explicit-cancel and timeout text. Yellow marks ordinary destructive actions; red marks force actions.

## Dashboard rendering and polling

- Dashboards go further than the general rule: `--no-color` or `NO_COLOR` yields plain text with no ANSI styling, and meaning stays readable without color.
- For `bubbles/table` v1, keep row values free of ANSI styles. Apply colors after table rendering through [loam](loam/README.md). Use its selection tag and style helpers instead of duplicating width or nested-reset workarounds.
- Use [trellis](trellis/README.md) for allocation and resizing. Measure unfiltered labels so filters do not move borders. Keep compact fields readable and show a notice below the hard width minimum.
- A border drag trades width only between its two neighboring columns. Freeze widths during the gesture. Keep desired proportions across polls and resizes; do not save widths to disk.
- Put long optional row text on attached detail lines when columns would become unreadable. Include those lines in the height budget.
- Derive dashboard observations from their owning sources. Retain missed observations only through an explicit stale-data policy; never let retention override fresh action checks.
- Run all I/O in `tea.Cmd` commands. Never block `Update` or `View`, and avoid goroutines in `Update`. Delayed messages carry tokens or generations so stale timeouts and replies cannot overwrite newer state.
- Read-only methods on state stored inside a Bubble Tea model use value receivers. Models are passed by value, so pointer-receiver reads cannot be called on type-asserted copies.
- Refresh after successful mutations. Choose polling intervals from measured scan cost, not a universal fixed number.

## Performance and verification

- **Measure before optimizing.** Record the source revision, workload, scope, environment, and repeated timings. Report medians and ranges; keep live and simulated results separate.
- **Remove unnecessary work first.** Batch external calls and reuse results within one invocation. Persistent caching needs an explicit freshness and invalidation policy.
- **Keep concurrency bounded.** Choose threads, processes, or async from measurements and platform constraints. Do not assume more workers means faster execution.
- **Test contracts, not private layout.** Cover arguments, output, state rules, and safety failures. Put external calls behind replaceable boundaries; do not require one injection mechanism in every language.
- **Isolate tests and benchmarks.** Use temporary repos, config, registries, and app stubs. Never confirm destructive prompts against live work or open/focus the user's windows during automated checks.
- **Run existing checks after changes.** Go projects run formatting, build, vet, and tests; use race checks for concurrent state. Python projects run their formatter, linter, type checker, and pytest through `uv`.
- **Verify real interaction safely.** Use owned, bounded tmux sessions named `ag-<task>` for TUI smoke tests. Register each with `reap-strays --register-tmux`, and kill it when the task ends. Test on disposable fixtures. Passing unit tests does not prove live behavior or deployment.

## Shared library maintenance

- Consumers pin released versions and update deliberately. Go consumers pin dashkit through `go.mod`.
- Never move or delete a published tag. Ship a new version for a fix. Respect compatibility and document migration when behavior changes.
- Keep packages independently importable, with clear API docs and examples. Name new packages plainly for what they do. Do not add metaphor names; the existing garden names are legacy, not a pattern to extend.
- Add a shared rule only when it serves multiple tools. Conventions are not permission to add features or remove deliberate omissions.

## Deliberate omissions

These are choices, not gaps. Do not "fix" them without an explicit request.

- Understory has no notion of a live agent. Canopy has no notion of git worktrees. Each owns its own discovery, and duplicating the other's would recouple the tools.
- Canopy is same-machine, same-user, and macOS-only. Dashboards have no row click; the mouse only resizes columns.
- No third long-text column in Canopy's table. Measured with real data in [canopy#12](https://github.com/luiul/canopy/issues/12), it crushed Location and Model and still truncated the text. Use an attached detail line.
- Column widths are never saved to disk.
- Convention work does not add features. Features are judged on their own merits.
