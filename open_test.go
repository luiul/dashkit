package mycelium

import (
	"errors"
	"testing"
)

// fakeDeps returns deps with every field faked to safe no-op defaults
// (no window already open, code CLI present, every command "succeeds",
// every Ghostty call a no-op), so each test only needs to override the
// one or two fields it cares about instead of restating the whole
// struct, and so unit tests never shell out to osascript or the real
// `code` CLI.
func fakeDeps() deps {
	return deps{
		lookPathCode:         func() (string, bool) { return "/usr/local/bin/code", true },
		runCommand:           func(args []string) (bool, string) { return true, "" },
		windowTitles:         func() ([]string, error) { return nil, nil },
		matchWindowTitle:     func(titles []string, path string) (string, bool) { return "", false },
		raiseWindow:          func(title string) (bool, error) { return false, nil },
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

func TestOpenVSCodeRaisesTheExistingWindowInsteadOfShellingOutToCode(t *testing.T) {
	// The switch-to-already-open half: when a window already has this
	// path's folder open, OpenVSCode should raise it directly and never
	// touch the `code` CLI at all (that's the whole point of checking
	// first, instead of trusting `code --reuse-window` to guess right).
	d := fakeDeps()
	d.windowTitles = func() ([]string, error) { return []string{"dotfiles — main"}, nil }
	d.matchWindowTitle = func(titles []string, path string) (string, bool) { return "dotfiles — main", true }
	var raisedTitle string
	d.raiseWindow = func(title string) (bool, error) { raisedTitle = title; return true, nil }
	codeCalled := false
	d.runCommand = func(args []string) (bool, string) { codeCalled = true; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if raisedTitle != "dotfiles — main" {
		t.Fatalf("got raised title %q, want %q", raisedTitle, "dotfiles — main")
	}
	if codeCalled {
		t.Fatalf("want the code CLI never invoked once an existing window was raised")
	}
}

func TestOpenVSCodeForcesANewWindowWhenNoneIsAlreadyOpen(t *testing.T) {
	// The case a path that's never been opened before always hits:
	// windowTitles finds nothing, so OpenVSCode must force a genuinely
	// new window (-n) rather than handing --reuse-window to the CLI and
	// letting it fall back to hijacking some unrelated window.
	d := fakeDeps()
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"/usr/local/bin/code", "-n", "/Users/x/dotfiles"}
	if len(gotArgs) != len(want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("got %v, want %v", gotArgs, want)
		}
	}
}

func TestOpenVSCodeForcesANewWindowWhenTheMatchedWindowIsGone(t *testing.T) {
	// windowTitles can be stale: the matched window may have closed
	// between that check and the raise attempt. raiseWindow reporting
	// "not found" (false, nil) should fall through to opening fresh
	// (still forced via -n, since the check itself did succeed), not
	// report failure.
	d := fakeDeps()
	d.windowTitles = func() ([]string, error) { return []string{"dotfiles — main"}, nil }
	d.matchWindowTitle = func(titles []string, path string) (string, bool) { return "dotfiles — main", true }
	d.raiseWindow = func(title string) (bool, error) { return false, nil }
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	found := false
	for _, a := range gotArgs {
		if a == "-n" {
			found = true
		}
	}
	if !found {
		t.Fatalf("got args %v, want -n (forced new window)", gotArgs)
	}
}

func TestOpenVSCodeFallsBackToReuseWindowWhenTheAlreadyOpenCheckItselfErrors(t *testing.T) {
	// windowTitles erroring (e.g. the Automation permission for
	// scripting VS Code hasn't been granted yet) means OpenVSCode
	// genuinely doesn't know whether a window is already open. Falling
	// back to --reuse-window here, rather than unconditionally forcing
	// -n, keeps repeated presses on the same path from stacking up
	// duplicate windows for anyone who hasn't granted that permission.
	d := fakeDeps()
	d.windowTitles = func() ([]string, error) { return nil, errors.New("not authorized") }
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	openVSCode(d, "/x")

	found := false
	for _, a := range gotArgs {
		if a == "-n" {
			t.Fatalf("got args %v, want no -n when the already-open check errored", gotArgs)
		}
		if a == "--reuse-window" {
			found = true
		}
	}
	if !found {
		t.Fatalf("got args %v, want --reuse-window", gotArgs)
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
	if len(gotArgs) != len(want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("got %v, want %v", gotArgs, want)
		}
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
