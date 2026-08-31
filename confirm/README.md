# confirm

A tiny Go library for the confirmation modal behind a
[Bubble Tea](https://github.com/charmbracelet/bubbletea) dashboard's
destructive keybindings: the state machine that arms a prompt, collects
the user's answer, and cancels it safely — shared by
[canopy](https://github.com/luiul/canopy) (agent-session dashboard) and
[understory](https://github.com/luiul/understory) (git-worktree
dashboard) so the two cannot drift apart.

Unlike the other dashkit packages, the name is plainly descriptive: the
package is exactly what it says, a confirmation modal, and a garden
metaphor would only obscure that.

## The discipline

One answer set, stated once here and enforced identically in both apps:

- `y` confirms.
- `n`, `esc`, or `enter` cancel — `enter` cancels because the prompt
  says `[y/N]`, where the capitalized letter is the convention's
  default answer.
- Every other key is swallowed: it must not act on the dashboard
  underneath (reordering rows, arming a second prompt, quitting around
  the prompt) nor cancel the prompt by accident.
- `ctrl+c` quits the app, as it does from everywhere, modal or not.

Plus two safety nets around the answer itself:

- **Auto-cancel timeout.** An unanswered prompt cancels itself after
  `Timeout` (10s): an unattended modal would otherwise wedge the app
  forever (it swallows every other key), and a stale prompt is
  dangerous anyway, since rows keep repolling and reordering underneath
  it. A token scheme (the same one a notification's auto-clear uses)
  keeps a stale tick from cancelling a newer prompt.
- **Poll revalidation.** Every poll re-stamps the armed prompt's
  targets against fresh data (`Refresh`): targets that vanished in the
  meantime drop out, survivors stay current, and a prompt left with
  nothing to act on closes itself rather than dangling.

The prompt's payload — what gets acted on once confirmed — stays with
the caller as `State`'s type parameter: understory stores a worktree
batch plus which removal kind to run, canopy a process list plus which
signal to send. The prompt's text likewise stays with the caller; this
package owns only the modal machinery around it.

## Usage

```go
import "github.com/luiul/dashkit/confirm"

// In the model:
type Model struct {
	// ... prompt confirm.State[payload] — zero value is ready to use
}

// Arming (a destructive key is pressed):
cmd := m.prompt.Arm(payload{entries: targets})

// In Update's key handling, before any other binding:
if m.prompt.Active() {
	switch confirm.Classify(msg) {
	case confirm.Confirm:
		p := m.prompt.Payload
		m.prompt.Resolve()
		return m, doTheThing(p)
	case confirm.Cancel:
		m.prompt.Resolve()
		return m, nil
	case confirm.Quit:
		return m, tea.Quit
	default: // swallowed
		return m, nil
	}
}

// The auto-cancel message:
case confirm.Msg:
	if m.prompt.Tick(msg) {
		return m, notify(confirm.TimeoutText())
	}

// On every poll, keeping an armed prompt honest:
m.prompt.Payload.entries = confirm.Refresh(m.prompt.Payload.entries, lookup)
if len(m.prompt.Payload.entries) == 0 {
	m.prompt.Resolve()
}
```

## Development

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .   # should print nothing
```
