package mycelium

import (
	"errors"
	"testing"
)

// fakeDeps returns deps with every field faked to safe no-op defaults
// (registry absent, code CLI present, every command "succeeds", every
// Ghostty call a no-op), so each test only needs to override the one or
// two fields it cares about instead of restating the whole struct, and
// so unit tests never shell out to osascript or the real `code` CLI.
func fakeDeps() deps {
	return deps{
		lookPathCode:         func() (string, bool) { return "/usr/local/bin/code", true },
		runCommand:           func(args []string) (bool, string) { return true, "" },
		readRegistry:         func() ([]registryEntry, bool) { return nil, false },
		logFallback:          func(reason, path string) {},
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

func TestOpenVSCodeFocusesViaTheRegistry(t *testing.T) {
	// A fresh registry entry names the window's folder, so focusing is
	// `code --reuse-window <folder>`.
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) {
		return []registryEntry{{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}}}, true
	}
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"/usr/local/bin/code", "--reuse-window", "/Users/x/dotfiles"}
	if len(gotArgs) != len(want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("got %v, want %v", gotArgs, want)
		}
	}
}

func TestOpenVSCodeRegistryFocusUsesTheWorkspaceFileForMultiRootWindows(t *testing.T) {
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) {
		return []registryEntry{{
			SessionID:     "1",
			Folders:       []string{"/Users/x/tardis-community", "/Users/x/tardis-community/scm-analytics-engineers"},
			WorkspaceFile: "/Users/x/tardis-community.code-workspace",
		}}, true
	}
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }

	result := openVSCode(d, "/Users/x/tardis-community/scm-analytics-engineers")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	want := []string{"/usr/local/bin/code", "--reuse-window", "/Users/x/tardis-community.code-workspace"}
	if len(gotArgs) != len(want) {
		t.Fatalf("got %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("got %v, want %v", gotArgs, want)
		}
	}
}

func TestOpenVSCodeRegistryMissOpensAGenuinelyNewWindow(t *testing.T) {
	// The registry answered and nothing matches: nothing is open on
	// this path, proven, so -n is safe.
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) {
		return []registryEntry{{SessionID: "1", Folders: []string{"/Users/x/canopy"}}}, true
	}
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

func TestOpenVSCodeEmptyRegistryOpensAGenuinelyNewWindow(t *testing.T) {
	// The registry answered and is empty (VS Code closed, or no window
	// has a folder open): nothing is open, proven, so -n is safe.
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) { return nil, true }
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

func TestOpenVSCodeRegistryMatchButFailedFocusOpensANewWindow(t *testing.T) {
	// The window the registry pointed at could not be focused (it may
	// have closed inside the staleness window): fall through to a new
	// window, same as a clean miss.
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) {
		return []registryEntry{{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}}}, true
	}
	var calls [][]string
	d.runCommand = func(args []string) (bool, string) {
		calls = append(calls, args)
		return len(calls) > 1, "" // the --reuse-window fails, the -n succeeds
	}

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	if len(calls) != 2 || calls[0][1] != "--reuse-window" || calls[1][1] != "-n" {
		t.Fatalf("got calls %v, want --reuse-window then -n", calls)
	}
}

func TestOpenVSCodeDegradesToReuseWindowWhenTheRegistryIsMissing(t *testing.T) {
	// Extension not installed: there is no way to tell what is open, so
	// the CLI's own best-effort --reuse-window is the least-bad option,
	// and the miss is logged so a broken extension is visible.
	d := fakeDeps()
	var gotArgs []string
	d.runCommand = func(args []string) (bool, string) { gotArgs = args; return true, "" }
	var loggedReason string
	d.logFallback = func(reason, path string) { loggedReason = reason }

	result := openVSCode(d, "/Users/x/dotfiles")

	if !result.OK {
		t.Fatalf("want ok, got %+v", result)
	}
	found := false
	for _, a := range gotArgs {
		if a == "-n" {
			t.Fatalf("got args %v, want no -n when the registry can't answer", gotArgs)
		}
		if a == "--reuse-window" {
			found = true
		}
	}
	if !found {
		t.Fatalf("got args %v, want --reuse-window", gotArgs)
	}
	if loggedReason != "registry-missing" {
		t.Fatalf("got logged reason %q, want registry-missing", loggedReason)
	}
}

func TestOpenVSCodeFallsBackToOpenWhenCodeCLIMissing(t *testing.T) {
	d := fakeDeps()
	d.readRegistry = func() ([]registryEntry, bool) { return nil, true }
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
