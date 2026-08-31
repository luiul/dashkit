package confirm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds the tea.KeyMsg a real keypress produces, for the keys
// the answer discipline cares about (runes, plus enter/esc/ctrl+c).
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestClassifyPinsTheAnswerDiscipline is the whole point of the package:
// one answer set both dashboards share. y confirms; n, esc, and enter
// cancel (enter honors the prompt's "[y/N]"); ctrl+c quits; everything
// else is swallowed.
func TestClassifyPinsTheAnswerDiscipline(t *testing.T) {
	cases := []struct {
		key  string
		want Answer
	}{
		{"y", Confirm},
		{"n", Cancel},
		{"esc", Cancel},
		{"enter", Cancel},
		{"ctrl+c", Quit},
		{"Y", Swallow}, // uppercase is not a confirmation
		{"N", Swallow}, // nor an uppercase cancel
		{"q", Swallow},
		{"x", Swallow},
		{"?", Swallow},
		{"j", Swallow},
	}
	for _, c := range cases {
		if got := Classify(keyMsg(c.key)); got != c.want {
			t.Errorf("Classify(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestArmActivatesThePromptAndSchedulesTheTick(t *testing.T) {
	var s State[string]
	if s.Active() {
		t.Fatal("want the zero value to have no prompt armed")
	}

	cmd := s.Arm("target")
	if !s.Active() {
		t.Fatal("want the prompt armed after Arm")
	}
	if s.Payload == nil || *s.Payload != "target" {
		t.Fatalf("got payload %+v, want %q", s.Payload, "target")
	}
	if cmd == nil {
		t.Fatal("want Arm to return the auto-cancel tick command")
	}
}

func TestResolveClosesThePromptAndInvalidatesThePendingTick(t *testing.T) {
	var s State[string]
	s.Arm("target")
	stale := s.Token()

	s.Resolve()

	if s.Active() {
		t.Fatal("want the prompt closed after Resolve")
	}
	// The tick scheduled by the earlier Arm must never cancel a prompt
	// armed later: Resolve bumped the token, so the old one is stale.
	s.Arm("other")
	if s.Tick(Msg{Token: stale}) {
		t.Fatal("want the stale tick ignored after Resolve")
	}
	if !s.Active() || *s.Payload != "other" {
		t.Fatalf("got payload %+v, want the newer prompt untouched", s.Payload)
	}
}

func TestTickCancelsOnlyThePromptWhoseTokenMatches(t *testing.T) {
	var s State[string]
	s.Arm("target")

	if s.Tick(Msg{Token: s.Token() - 1}) {
		t.Fatal("want a stale token ignored")
	}
	if !s.Active() {
		t.Fatal("want the prompt to survive a stale tick")
	}

	if !s.Tick(Msg{Token: s.Token()}) {
		t.Fatal("want the prompt's own tick to fire")
	}
	if s.Active() {
		t.Fatal("want the prompt cancelled once its own token fires")
	}

	// A tick with no prompt armed at all is ignored, matching token or
	// not (there is nothing to cancel).
	if s.Tick(Msg{Token: s.Token()}) {
		t.Fatal("want a tick with no armed prompt ignored")
	}
}

func TestTimeoutTextNamesTheTimeout(t *testing.T) {
	if got, want := TimeoutText(), "cancelled: no answer within 10s"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.Contains(TimeoutText(), Timeout.String()) {
		t.Fatalf("got %q, want it built from Timeout (%s)", TimeoutText(), Timeout)
	}
}

func TestRefreshKeepsSurvivingTargetsRestamped(t *testing.T) {
	type target struct {
		key   string
		fresh int
	}
	targets := []target{{"a", 1}, {"b", 1}, {"c", 1}}

	// "b" vanished from the poll; "a" and "c" survive with fresh data.
	got := Refresh(targets, func(t target) (target, bool) {
		if t.key == "b" {
			return target{}, false
		}
		t.fresh = 2
		return t, true
	})

	if len(got) != 2 || got[0].key != "a" || got[1].key != "c" {
		t.Fatalf("got %+v, want only a and c left", got)
	}
	for _, g := range got {
		if g.fresh != 2 {
			t.Fatalf("got %+v, want survivors re-stamped with their fresh copies", got)
		}
	}
}

func TestRefreshWithNothingLeftReturnsEmpty(t *testing.T) {
	got := Refresh([]string{"a"}, func(string) (string, bool) { return "", false })
	if len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}
