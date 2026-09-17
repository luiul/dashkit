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
		ghosttyFocusByCwd:    func(cwd string) (bool, error) { return false, nil },
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

func TestOpenGhosttyFocusesByCwd(t *testing.T) {
	var gotCwd string
	d := fakeDeps()
	d.ghosttyFocusByCwd = func(cwd string) (bool, error) { gotCwd = cwd; return true, nil }

	result := openGhostty(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("got cwd %q", gotCwd)
	}
}

func TestOpenGhosttyWithoutAPathFailsClearly(t *testing.T) {
	result := openGhostty(fakeDeps(), "")
	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
}

func TestOpenGhosttyOpensNewWindowWhenNoTerminalMatches(t *testing.T) {
	d := fakeDeps()
	d.ghosttyFocusByCwd = func(string) (bool, error) { return false, nil }
	var gotCwd string
	d.ghosttyOpenNewWindow = func(cwd string) error { gotCwd = cwd; return nil }

	result := openGhostty(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if gotCwd != "/Users/x/dotfiles" {
		t.Fatalf("got cwd %q", gotCwd)
	}
}

func TestOpenGhosttyFailsClearlyWhenNewWindowFails(t *testing.T) {
	d := fakeDeps()
	d.ghosttyFocusByCwd = func(string) (bool, error) { return false, nil }
	d.ghosttyOpenNewWindow = func(string) error { return errors.New("couldn't open a new window") }

	result := openGhostty(d, "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "couldn't open a new window") {
		t.Fatalf("got message %q", result.Message)
	}
}

func TestOpenGhosttySurfacesAutomationPermissionErrors(t *testing.T) {
	d := fakeDeps()
	d.ghosttyFocusByCwd = func(string) (bool, error) { return false, errors.New("grant Automation permission") }

	result := openGhostty(d, "/Users/x/dotfiles")

	if result.OK {
		t.Fatalf("want not ok, got %+v", result)
	}
	if !contains(result.Message, "Automation permission") {
		t.Fatalf("got message %q", result.Message)
	}
}
