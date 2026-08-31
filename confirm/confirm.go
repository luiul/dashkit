// Package confirm is the shared confirmation-modal state machine behind
// canopy's and understory's destructive-action prompts (x/X/P/M in
// understory, x/X/D in canopy): one answer discipline, one auto-cancel
// timeout, one poll-revalidation hook, so the two dashboards' modals
// cannot drift apart again.
//
// The discipline, stated once: y confirms; n, esc, or enter cancel
// (honoring the prompt's own "[y/N]", where enter is the capitalized
// default answer); every other key is swallowed, so the state the
// prompt describes cannot shift under an in-flight answer; and ctrl+c
// quits the app, as it does from everywhere, modal or not. An
// unanswered prompt cancels itself after Timeout, since rows keep
// repolling and reordering underneath it. And every poll revalidates
// the armed prompt's targets (see Refresh), closing the prompt when
// nothing it named still exists.
//
// The prompt's payload — what actually gets acted on once confirmed —
// stays with the caller as State's type parameter: understory stores a
// worktree batch plus which removal kind to run, canopy a process list
// plus which signal to send. The prompt's text likewise stays with the
// caller; this package owns only the modal machinery around it.
package confirm

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Timeout is how long an armed prompt waits for an answer before
// cancelling itself. An unattended modal would otherwise wedge its app
// forever (it swallows every other key), and a stale prompt is
// dangerous anyway: rows keep repolling and reordering underneath it,
// so a prompt answered long after it appeared may no longer mean what
// it said when it appeared.
const Timeout = 10 * time.Second

// Msg is the auto-cancel tick's message, delivered Timeout after a
// prompt is armed. Token identifies the prompt the tick was scheduled
// for, so a stale tick never cancels a newer prompt (the same token
// pattern a notification's auto-clear uses).
type Msg struct{ Token int }

// Answer classifies a keypress against an armed prompt.
type Answer int

const (
	// Swallow is any key that answers nothing: the prompt stays armed
	// and the key does nothing at all — it must not act on the
	// dashboard underneath (reordering rows, arming a second prompt,
	// quitting around the prompt) nor cancel by accident.
	Swallow Answer = iota
	// Confirm is y: answer yes.
	Confirm
	// Cancel is n, esc, or enter: answer no. Enter cancels rather than
	// confirming because the prompt says "[y/N]": the capitalized
	// letter is the convention's default answer, and enter selects it.
	Cancel
	// Quit is ctrl+c, the one key an armed prompt never swallows: it
	// quits the app, as it does from everywhere.
	Quit
)

// Classify maps a keypress to its Answer. This is the one place the
// answer discipline lives; both dashboards' Update loops switch on it.
func Classify(key tea.KeyMsg) Answer {
	switch key.String() {
	case "y":
		return Confirm
	case "n", "esc", "enter":
		return Cancel
	case "ctrl+c":
		return Quit
	default:
		return Swallow
	}
}

// State is the modal half of a confirmation prompt: the armed prompt's
// payload (nil while no prompt is open) and the token its auto-cancel
// tick must match. The zero value is ready to use: no prompt armed.
type State[T any] struct {
	// Payload is the armed prompt's target data, set by Arm and read by
	// the caller's prompt rendering and confirmed-action dispatch. Nil
	// whenever no prompt is open.
	Payload *T
	token   int
}

// Active reports whether a prompt is currently armed.
func (s *State[T]) Active() bool { return s.Payload != nil }

// Token exposes the current auto-cancel token, primarily so tests can
// construct matching (or deliberately stale) Msg values without waiting
// out the real Timeout.
func (s *State[T]) Token() int { return s.token }

// Arm opens the prompt with payload and returns the auto-cancel tick
// command. Arming while a prompt is already open replaces it; callers
// never do this (a modal prompt swallows the keys that would arm
// another), but the token discipline makes it safe regardless.
func (s *State[T]) Arm(payload T) tea.Cmd {
	s.Payload = &payload
	s.token++
	return timeoutCmd(s.token)
}

// Resolve closes the prompt, whether answered or explicitly cancelled,
// invalidating the pending auto-cancel tick.
func (s *State[T]) Resolve() {
	s.Payload = nil
	s.token++
}

// Tick handles the auto-cancel message: when a prompt is armed and msg
// carries its current token, the prompt closes and Tick reports true
// (the caller shows TimeoutText); anything else is a stale tick and is
// ignored. The token is deliberately not bumped here: the tick that
// fired is spent, and the next Arm bumps it anyway.
func (s *State[T]) Tick(msg Msg) bool {
	if s.Payload != nil && msg.Token == s.token {
		s.Payload = nil
		return true
	}
	return false
}

// TimeoutText is the notification text shown when a prompt cancels
// itself unanswered: "cancelled: no answer within 10s". It lives here
// so both dashboards phrase it identically.
func TimeoutText() string {
	return "cancelled: no answer within " + Timeout.String()
}

func timeoutCmd(token int) tea.Cmd {
	return tea.Tick(Timeout, func(time.Time) tea.Msg { return Msg{Token: token} })
}

// Refresh re-stamps an armed prompt's target set against a fresh poll:
// every target lookup still reports is replaced by its fresh copy (so
// whatever the payload's own identity guard compares against stays
// current), and every target lookup reports gone — removed or exited on
// its own while the prompt was open — drops out of the set. When
// nothing remains there is nothing left to confirm: the caller closes
// the prompt with Resolve rather than leaving a stale one aimed at rows
// that no longer mean what the user thinks.
func Refresh[T any](targets []T, lookup func(T) (T, bool)) []T {
	out := make([]T, 0, len(targets))
	for _, t := range targets {
		if fresh, ok := lookup(t); ok {
			out = append(out, fresh)
		}
	}
	return out
}
