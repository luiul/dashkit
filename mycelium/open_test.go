package mycelium

import (
	"errors"
	"slices"
	"testing"
)

// fakeDeps returns deps with every field faked to safe no-op defaults
// (VS Code not running, code CLI present, every command "succeeds",
// every Ghostty call a no-op), so each test only needs to override the
// one or two fields it cares about instead of restating the whole
// struct, and so unit tests never shell out to osascript or the real
// `code` CLI.
func fakeDeps() deps {
	return deps{
		lookPathCode:         func() (string, bool) { return "/usr/local/bin/code", true },
		runCommand:           func(args []string) (bool, string) { return true, "" },
		vscodeWindows:        func() ([]string, bool, error) { return nil, false, nil },
		raiseWindow:          func(title string) (bool, error) { return true, nil },
		activateCode:         func() error { return nil },
		home:                 func() string { return "/Users/x" },
		toplevel:             func(dir string) string { return "" },
		ghosttyWindows:       func() ([]string, bool, error) { return nil, false, nil },
		ghosttyWindowsDeep:   func() ([]string, bool, error) { return nil, false, nil },
		raiseGhosttyWindow:   func(title string) (bool, error) { return true, nil },
		ghosttyOpenNewWindow: func(cwd string) error { return nil },
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestOpenVSCodeFocusesTheMatchedWindowByExactTitle(t *testing.T) {
	// A window titled with the path (plus branch, which is never
	// matched) is raised by its exact title, and the `code` CLI is
	// never invoked: focus is AXRaise, not --reuse-window.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles — main"}, true, nil
	}
	var gotTitle string
	d.raiseWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }
	cliCalled := false
	d.runCommand = func(args []string) (bool, string) { cliCalled = true; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "~/dotfiles — main" {
		t.Fatalf("raised %q, want the exact window title", gotTitle)
	}
	if cliCalled {
		t.Fatal("the code CLI ran on a focus path; focusing is AXRaise only")
	}
	if !contains(result.Message, "Focused") {
		t.Fatalf("got message %q, want a focus confirmation", result.Message)
	}
}

func TestOpenVSCodeMatchlessListingActivatesAndRelistsOnce(t *testing.T) {
	// macOS culls the AX tree of a backgrounded app, so a matchless
	// listing while Code runs isn't believed: Code is activated (which
	// re-materializes the tree) and the listing re-run once. Here the
	// second listing sees the window and focuses it.
	d := fakeDeps()
	listCalls := 0
	d.vscodeWindows = func() ([]string, bool, error) {
		listCalls++
		if listCalls == 1 {
			return []string{"~/canopy — main"}, true, nil
		}
		return []string{"~/canopy — main", "~/dotfiles — main"}, true, nil
	}
	activations := 0
	d.activateCode = func() error { activations++; return nil }
	var gotTitle string
	d.raiseWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if listCalls != 2 {
		t.Fatalf("listed %d times, want exactly 2 (one re-list)", listCalls)
	}
	if activations != 1 {
		t.Fatalf("activated %d times, want 1", activations)
	}
	if gotTitle != "~/dotfiles — main" {
		t.Fatalf("raised %q, want the re-listed window's title", gotTitle)
	}
}

func TestOpenVSCodeStillMatchlessAfterRelistOpensANewWindow(t *testing.T) {
	// Both listings agree nothing is open on path: -n is proven safe.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return []string{"~/canopy — main"}, true, nil
	}
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"/usr/local/bin/code", "-n", "/Users/x/dotfiles"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
}

func TestOpenVSCodeEmptyListingWhileRunningIsNotBelieved(t *testing.T) {
	// Zero windows listed while Code runs is the AX-cull signature: it
	// also gets the activate-and-relist treatment before -n is allowed.
	d := fakeDeps()
	listCalls := 0
	d.vscodeWindows = func() ([]string, bool, error) {
		listCalls++
		if listCalls == 1 {
			return nil, true, nil
		}
		return []string{"~/dotfiles — main"}, true, nil
	}
	var gotTitle string
	d.raiseWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if listCalls != 2 {
		t.Fatalf("listed %d times, want 2", listCalls)
	}
	if gotTitle != "~/dotfiles — main" {
		t.Fatalf("raised %q, want the re-listed window's title", gotTitle)
	}
}

func TestOpenVSCodeNotRunningOpensNewWindowWithoutRelisting(t *testing.T) {
	// VS Code closed is a definitive nothing-open: no activate, no
	// re-list, straight to -n.
	d := fakeDeps()
	listCalls := 0
	d.vscodeWindows = func() ([]string, bool, error) { listCalls++; return nil, false, nil }
	activated := false
	d.activateCode = func() error { activated = true; return nil }
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if listCalls != 1 {
		t.Fatalf("listed %d times, want 1 (VS Code closed is definitive)", listCalls)
	}
	if activated {
		t.Fatal("activated Code even though it isn't running")
	}
	want := []string{"/usr/local/bin/code", "-n", "/Users/x/dotfiles"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
}

func TestOpenVSCodeNeverCallsReuseWindow(t *testing.T) {
	// The CLI is only ever asked to open, never to reuse: --reuse-window
	// re-runs its own matching and hijacks the last-active window on a
	// miss. That call is the bug; it must not appear on any path.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles — main"}, true, nil
	}
	d.raiseWindow = func(string) (bool, error) { return false, nil } // window vanished
	var calls [][]string
	d.runCommand = func(args []string) (bool, string) { calls = append(calls, args); return true, "" }

	if result := openVSCode(d, "/Users/x/dotfiles"); !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	for _, args := range calls {
		if slices.Contains(args, "--reuse-window") {
			t.Fatalf("--reuse-window called: %v", args)
		}
	}
}

func TestOpenVSCodeVanishedMatchOpensANewWindow(t *testing.T) {
	// The matched window closed between listing and raise (raise
	// answers false): fall through to a genuinely new window.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles — main"}, true, nil
	}
	d.raiseWindow = func(string) (bool, error) { return false, nil }
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"/usr/local/bin/code", "-n", "/Users/x/dotfiles"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
}

func TestOpenVSCodeListingErrorSurfacesAndOpensNothing(t *testing.T) {
	// The window check itself couldn't run (Automation permission
	// pending, most likely): opening blind could stack a duplicate, so
	// fail with the actionable message instead.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return nil, false, newPermissionError("grant Automation permission")
	}
	cliCalled := false
	d.runCommand = func(args []string) (bool, string) { cliCalled = true; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "Automation permission") {
		t.Fatalf("got message %q", result.Message)
	}
	if cliCalled {
		t.Fatal("opened a window even though the already-open check couldn't run")
	}
}

func TestOpenVSCodeRelistErrorSurfacesAndOpensNothing(t *testing.T) {
	// Same closed failure on the re-list: the first listing may have
	// been culled, so a failed re-list leaves "what is open" unknown.
	d := fakeDeps()
	listCalls := 0
	d.vscodeWindows = func() ([]string, bool, error) {
		listCalls++
		if listCalls == 1 {
			return []string{"~/canopy — main"}, true, nil
		}
		return nil, false, errors.New("osascript failed")
	}
	cliCalled := false
	d.runCommand = func(args []string) (bool, string) { cliCalled = true; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if cliCalled {
		t.Fatal("opened a window even though the re-list failed")
	}
}

func TestOpenVSCodeRaiseErrorSurfacesAndOpensNothing(t *testing.T) {
	// The matched window is known to be open; if raising it fails,
	// opening a new window would stack a duplicate. Surface the error.
	d := fakeDeps()
	d.vscodeWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles — main"}, true, nil
	}
	d.raiseWindow = func(string) (bool, error) { return false, errors.New("AXRaise failed") }
	cliCalled := false
	d.runCommand = func(args []string) (bool, string) { cliCalled = true; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "AXRaise failed") {
		t.Fatalf("got message %q", result.Message)
	}
	if cliCalled {
		t.Fatal("opened a duplicate next to a window that merely failed to raise")
	}
}

func TestOpenVSCodeFallsBackToOpenWhenCodeCLIMissing(t *testing.T) {
	d := fakeDeps()
	d.lookPathCode = func() (string, bool) { return "", false }
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"open", "-a", "Visual Studio Code", "/Users/x/dotfiles"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
}

func TestOpenVSCodeWithoutAPathFailsClearly(t *testing.T) {
	result := openVSCode(fakeDeps(), "")
	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "path") {
		t.Fatalf("got message %q, want it to mention path", result.Message)
	}
}

func TestOpenGhosttyFocusesTheMatchedWindowByExactTitle(t *testing.T) {
	// A window titled with the path (plus branch, never matched) is
	// raised by its exact title, and no new window is opened.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles • main"}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }
	openedNew := false
	d.ghosttyOpenNewWindow = func(string) error { openedNew = true; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "~/dotfiles • main" {
		t.Fatalf("raised %q, want the exact window title", gotTitle)
	}
	if openedNew {
		t.Fatal("opened a new window on a focus path")
	}
	if !contains(result.Message, "Focused") {
		t.Fatalf("got message %q, want a focus confirmation", result.Message)
	}
}

func TestOpenGhosttyParsesRunningCommandAndPiTitles(t *testing.T) {
	// The title grammar keeps the root path-bearing while a command
	// runs ("cmd • root • branch") and while a pi session runs
	// ("π • root • branch", set by the session-title extension): both
	// must match the path the same way the idle title does.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"go test • ~/dashkit • fix/x", "π • ~/worktrees/canopy • feat/y"}, true, nil
	}
	var raised []string
	d.raiseGhosttyWindow = func(title string) (bool, error) { raised = append(raised, title); return true, nil }

	if result := openGhostty(d, "", "/Users/x/dashkit"); !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if result := openGhostty(d, "", "/Users/x/worktrees/canopy"); !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"go test • ~/dashkit • fix/x", "π • ~/worktrees/canopy • feat/y"}
	if !slices.Equal(raised, want) {
		t.Fatalf("raised %v, want %v", raised, want)
	}
}

func TestOpenGhosttyUnparseableTitleNeverMatches(t *testing.T) {
	// pi's own title ("π - basename", no path) cannot identify a
	// window: it must not match, so the call opens a new window rather
	// than focusing a guess.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"π - dotfiles"}, true, nil
	}
	raised := false
	d.raiseGhosttyWindow = func(string) (bool, error) { raised = true; return true, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if raised {
		t.Fatal("focused a window identified by basename only")
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("opened new window at %q", gotCwd)
	}
}

func TestOpenGhosttyWorkTreeRootStage(t *testing.T) {
	// The agent's cwd can be a subdirectory: a window titled with the
	// work-tree root still matches, exactly as on the VS Code path.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles • main"}, true, nil
	}
	d.toplevel = func(dir string) string { return "/Users/x/dotfiles" }
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "", "/Users/x/dotfiles/sub")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "~/dotfiles • main" {
		t.Fatalf("raised %q, want the work-tree root window", gotTitle)
	}
}

func TestOpenGhosttyTruncatedTitleMatchesByTail(t *testing.T) {
	// A narrow window truncates a long title from the left
	// ("…/hellofresh/review-zau/global-ops" for a window on
	// ~/worktrees/hellofresh/review-zau/global-ops): the visible tail
	// still identifies the window, so the call focuses it rather than
	// opening a duplicate. A sibling path that doesn't share the tail
	// still gets a new window.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"…/hellofresh/review-zau/global-ops"}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/worktrees/hellofresh/review-zau/global-ops")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "…/hellofresh/review-zau/global-ops" {
		t.Fatalf("raised %q, want the truncated window", gotTitle)
	}
	if gotCwd != "" {
		t.Fatalf("opened a new window at %q although the truncated window matches", gotCwd)
	}

	gotTitle = ""
	result = openGhostty(d, "", "/Users/x/worktrees/hellofresh/review-zau/global-ops-2")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "" {
		t.Fatalf("raised %q for a sibling path, want no match", gotTitle)
	}
	if gotCwd != "/Users/x/worktrees/hellofresh/review-zau/global-ops-2" {
		t.Fatalf("opened new window at %q, want the sibling path", gotCwd)
	}
}

func TestOpenGhosttyTruncatedTitleMidComponentCut(t *testing.T) {
	// The truncation cut can land inside a directory name
	// ("…fresh/review-zau/global-ops" for "…/hellofresh/
	// review-zau/global-ops"): the match retries from the tail's first
	// separator, which restores the element boundary.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"…fresh/review-zau/global-ops"}, true, nil
	}
	raised := false
	d.raiseGhosttyWindow = func(string) (bool, error) { raised = true; return true, nil }

	result := openGhostty(d, "", "/Users/x/worktrees/hellofresh/review-zau/global-ops")

	if !result.OK || !raised {
		t.Fatalf("want the truncated window focused, got %+v raised=%v", result, raised)
	}
}

func TestOpenGhosttyTruncatedTailTooWeakNeverMatches(t *testing.T) {
	// A fragment with no element boundary left ("…global-ops", a cut
	// that consumed every separator) is too weak to anchor a match:
	// opening a new window beats focusing a guess. So is a truncation
	// that left only non-path segments ("… • main • ttys004").
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"…global-ops", "… • main • ttys004"}, true, nil
	}
	raised := false
	d.raiseGhosttyWindow = func(string) (bool, error) { raised = true; return true, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/worktrees/hellofresh/review-zau/global-ops")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if raised {
		t.Fatal("focused a window on a too-weak tail")
	}
	if gotCwd == "" {
		t.Fatal("want a new window opened instead")
	}
}

func TestOpenGhosttyFullPathWindowWinsOverTruncatedTail(t *testing.T) {
	// When a full-path window and a truncated window could both match,
	// the full-path window is raised: the tail stage stays the lowest
	// priority, so a truncated match can never displace a solid one.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{
			"…/hellofresh/review-zau/global-ops",
			"~/worktrees/hellofresh/review-zau/global-ops • review-zau/global-ops",
		}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "", "/Users/x/worktrees/hellofresh/review-zau/global-ops")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := "~/worktrees/hellofresh/review-zau/global-ops • review-zau/global-ops"
	if gotTitle != want {
		t.Fatalf("raised %q, want the full-path window %q", gotTitle, want)
	}
}

func TestOpenGhosttyMatchlessListingDeepRelistsOnce(t *testing.T) {
	// Same AX-cull self-heal as the VS Code path (luiul/dashkit#9), in
	// the multi-instance shape: a matchless plain listing while Ghostty
	// runs gets one deep listing — every instance frontmost in turn,
	// its titles collected while it is frontmost — before a new window
	// is allowed. Here the deep listing sees the window and focuses it.
	d := fakeDeps()
	listCalls := 0
	d.ghosttyWindows = func() ([]string, bool, error) {
		listCalls++
		return []string{"~/canopy • main"}, true, nil
	}
	deepCalls := 0
	d.ghosttyWindowsDeep = func() ([]string, bool, error) {
		deepCalls++
		return []string{"~/canopy • main", "~/dotfiles • main"}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if listCalls != 1 {
		t.Fatalf("plain listing ran %d times, want 1", listCalls)
	}
	if deepCalls != 1 {
		t.Fatalf("deep listing ran %d times, want 1", deepCalls)
	}
	if gotTitle != "~/dotfiles • main" {
		t.Fatalf("raised %q, want the deep-listed window's title", gotTitle)
	}
}

func TestOpenGhosttyStillMatchlessAfterDeepListingOpensANewWindow(t *testing.T) {
	// Both listings agree nothing is open on path: opening a new
	// window is proven safe. Mirrors the VS Code path's
	// StillMatchlessAfterRelist test.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/canopy • main"}, true, nil
	}
	deepCalls := 0
	d.ghosttyWindowsDeep = func() ([]string, bool, error) {
		deepCalls++
		return []string{"~/canopy • main"}, true, nil
	}
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if deepCalls != 1 {
		t.Fatalf("deep listing ran %d times, want 1", deepCalls)
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("opened new window at %q", gotCwd)
	}
}

func TestOpenGhosttyNotRunningOpensNewWindowWithoutRelisting(t *testing.T) {
	// Ghostty closed is a definitive nothing-open: no deep listing,
	// straight to a new window.
	d := fakeDeps()
	listCalls := 0
	d.ghosttyWindows = func() ([]string, bool, error) { listCalls++; return nil, false, nil }
	deepCalls := 0
	d.ghosttyWindowsDeep = func() ([]string, bool, error) { deepCalls++; return nil, false, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if listCalls != 1 {
		t.Fatalf("listed %d times, want 1", listCalls)
	}
	if deepCalls != 0 {
		t.Fatal("deep listed even though Ghostty isn't running")
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("opened new window at %q", gotCwd)
	}
}

func TestOpenGhosttyVanishedMatchOpensANewWindow(t *testing.T) {
	// The matched window closed between listing and raise (raise
	// answers false): fall through to a genuinely new window.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles • main"}, true, nil
	}
	d.raiseGhosttyWindow = func(string) (bool, error) { return false, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("opened new window at %q", gotCwd)
	}
}

func TestOpenGhosttyRaiseErrorSurfacesAndOpensNothing(t *testing.T) {
	// The matched window is known to be open; if raising it fails,
	// opening a new window would stack a duplicate. Surface the error.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles • main"}, true, nil
	}
	d.raiseGhosttyWindow = func(string) (bool, error) { return false, errors.New("AXRaise failed") }
	openedNew := false
	d.ghosttyOpenNewWindow = func(string) error { openedNew = true; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "AXRaise failed") {
		t.Fatalf("got message %q", result.Message)
	}
	if openedNew {
		t.Fatal("opened a duplicate next to a window that merely failed to raise")
	}
}

func TestOpenGhosttyListingErrorSurfacesAndOpensNothing(t *testing.T) {
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return nil, false, errors.New("grant Automation permission")
	}
	openedNew := false
	d.ghosttyOpenNewWindow = func(string) error { openedNew = true; return nil }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "Automation permission") {
		t.Fatalf("got message %q", result.Message)
	}
	if openedNew {
		t.Fatal("opened a window even though the already-open check couldn't run")
	}
}

func TestOpenGhosttyFailsClearlyWhenNewWindowFails(t *testing.T) {
	d := fakeDeps()
	d.ghosttyOpenNewWindow = func(string) error { return errors.New("couldn't open a new window") }

	result := openGhostty(d, "", "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "couldn't open a new window") {
		t.Fatalf("got message %q", result.Message)
	}
}

func TestOpenGhosttyWithoutAPathFailsClearly(t *testing.T) {
	result := openGhostty(fakeDeps(), "", "")
	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "path") {
		t.Fatalf("got message %q, want it to mention path", result.Message)
	}
}

func TestOpenGhosttyPrefersTheTtyMatchedWindow(t *testing.T) {
	// Two agents side by side in the same repo on the same branch: the
	// titles differ only by the tty segment, and the tty identifies the
	// exact window. The path match alone can't pick between them.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{
			"π • ~/dotfiles • main • ttys001",
			"π • ~/dotfiles • main • ttys002",
		}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "ttys002", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "π • ~/dotfiles • main • ttys002" {
		t.Fatalf("raised %q, want the tty-matched window", gotTitle)
	}
}

func TestOpenGhosttyTtyMatchRequiresThePathToMatch(t *testing.T) {
	// A tty match whose window path doesn't match is ignored (a recycled
	// tty name must not focus an unrelated window): the call falls back
	// to the plain path match instead.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{
			"π • ~/canopy • main • ttys002",
			"π • ~/dotfiles • main",
		}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "ttys002", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "π • ~/dotfiles • main" {
		t.Fatalf("raised %q, want the path-matched window", gotTitle)
	}
}

func TestOpenGhosttyUnknownTtyFallsBackToPathMatch(t *testing.T) {
	// No window carries the tty (the agent predates the session-title
	// extension, or the extension couldn't resolve it): the same-path
	// window is focused rather than opening a duplicate.
	d := fakeDeps()
	d.ghosttyWindows = func() ([]string, bool, error) {
		return []string{"~/dotfiles • main"}, true, nil
	}
	var gotTitle string
	d.raiseGhosttyWindow = func(title string) (bool, error) { gotTitle = title; return true, nil }

	result := openGhostty(d, "ttys009", "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotTitle != "~/dotfiles • main" {
		t.Fatalf("raised %q, want the path-matched window", gotTitle)
	}
}
