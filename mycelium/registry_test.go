package mycelium

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRegistryEntry writes one registry file into dir with the given
// name and content, stamped fresh (mtime now) unless stale is true, in
// which case it is backdated beyond the staleness cutoff.
func writeRegistryEntry(t *testing.T, dir, name, content string, stale bool) {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if stale {
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadRegistryParsesEntries(t *testing.T) {
	dir := t.TempDir()
	writeRegistryEntry(t, dir, "a.json", `{
		"sessionId": "a",
		"folders": ["/Users/x/dotfiles", "/Users/x/dots"],
		"workspaceFile": "/Users/x/dots.code-workspace",
		"updatedAt": "2026-01-01T00:00:00Z"
	}`, false)
	writeRegistryEntry(t, dir, "b.json", `{
		"sessionId": "b",
		"folders": [],
		"workspaceFile": null,
		"updatedAt": "2026-01-01T00:00:00Z"
	}`, false)

	entries, ok := readRegistry(dir, time.Now())
	if !ok {
		t.Fatal("want ok for a readable registry directory")
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	var a registryEntry
	for _, e := range entries {
		if e.SessionID == "a" {
			a = e
		}
	}
	if len(a.Folders) != 2 || a.Folders[0] != "/Users/x/dotfiles" {
		t.Fatalf("got folders %v, want the two recorded paths", a.Folders)
	}
	if a.WorkspaceFile != "/Users/x/dots.code-workspace" {
		t.Fatalf("got workspaceFile %q", a.WorkspaceFile)
	}
}

func TestReadRegistryPrunesStaleEntries(t *testing.T) {
	// A closed window cannot be relied on to delete its entry, so an
	// entry whose heartbeat stopped (mtime older than the staleness
	// cutoff) must be dropped: it is the only cleanup mechanism.
	dir := t.TempDir()
	writeRegistryEntry(t, dir, "fresh.json", `{"sessionId":"fresh","folders":["/Users/x/a"],"workspaceFile":null,"updatedAt":"x"}`, false)
	writeRegistryEntry(t, dir, "stale.json", `{"sessionId":"stale","folders":["/Users/x/b"],"workspaceFile":null,"updatedAt":"x"}`, true)

	entries, ok := readRegistry(dir, time.Now())
	if !ok {
		t.Fatal("want ok for a readable registry directory")
	}
	if len(entries) != 1 || entries[0].SessionID != "fresh" {
		t.Fatalf("got entries %v, want only the fresh one", entries)
	}
}

func TestReadRegistrySkipsTornAndForeignFiles(t *testing.T) {
	// A half-written file (the atomic write's temp, a crash mid-write)
	// or a foreign file in the directory must be skipped, never fatal:
	// one bad file must not take window detection down for every window.
	dir := t.TempDir()
	writeRegistryEntry(t, dir, "good.json", `{"sessionId":"good","folders":["/Users/x/a"],"workspaceFile":null,"updatedAt":"x"}`, false)
	writeRegistryEntry(t, dir, "torn.json", `{"sessionId":"to`, false)
	if err := os.WriteFile(filepath.Join(dir, "fallback.log"), []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.json.tmp"), []byte(`{"sessionId":"tmp"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, ok := readRegistry(dir, time.Now())
	if !ok {
		t.Fatal("want ok despite the torn file")
	}
	if len(entries) != 1 || entries[0].SessionID != "good" {
		t.Fatalf("got entries %v, want only the good one", entries)
	}
}

func TestReadRegistryMissingDirectoryReportsUnavailable(t *testing.T) {
	// Extension not installed: the caller falls back to the title
	// cascade, and "no registry" is reported distinctly from "registry
	// answered, nothing fresh".
	if _, ok := readRegistry(filepath.Join(t.TempDir(), "does-not-exist"), time.Now()); ok {
		t.Fatal("want ok=false for a missing registry directory")
	}
	if _, ok := readRegistry("", time.Now()); ok {
		t.Fatal("want ok=false for an unknown home directory")
	}
}

func TestReadRegistryEmptyDirectoryIsAvailable(t *testing.T) {
	entries, ok := readRegistry(t.TempDir(), time.Now())
	if !ok {
		t.Fatal("want ok for an existing but empty registry directory")
	}
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
}

func TestMatchRegistryExactFolder(t *testing.T) {
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/canopy"}},
		{SessionID: "2", Folders: []string{"/Users/x/dotfiles"}},
	}
	target, ok := matchRegistry(entries, "/Users/x/dotfiles", func(string) string { return "" })
	if !ok || target != "/Users/x/dotfiles" {
		t.Fatalf("got (%q, %v), want the exact folder", target, ok)
	}
}

func TestMatchRegistryWorktreeRoot(t *testing.T) {
	// canopy hands the agent's cwd over as-is, and that cwd can be a
	// subdirectory of a checkout: a window open on the work-tree root is
	// an exact-folder match for the tree and must win before the nested
	// stage runs.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/tardis-community"}},
	}
	toplevel := fakeToplevel("/Users/x/tardis-community")
	target, ok := matchRegistry(entries, "/Users/x/tardis-community/pipelines/dbt", toplevel)
	if !ok || target != "/Users/x/tardis-community" {
		t.Fatalf("got (%q, %v), want the work-tree root window", target, ok)
	}
}

func TestMatchRegistryNestedPrefix(t *testing.T) {
	// A window open on a subpackage inside the worktree is reused when
	// nothing is open on the worktree itself.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/tardis-community/pipelines/dbt"}},
	}
	target, ok := matchRegistry(entries, "/Users/x/tardis-community", func(string) string { return "" })
	if !ok || target != "/Users/x/tardis-community/pipelines/dbt" {
		t.Fatalf("got (%q, %v), want the nested window's folder", target, ok)
	}
}

func TestMatchRegistryNestedPrefixRespectsElementBoundaries(t *testing.T) {
	// "/wt-a" must not match "/wt-a-b": a raw string prefix is not a
	// path containment check.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/x/wt-a-b"}},
	}
	if target, ok := matchRegistry(entries, "/x/wt-a", func(string) string { return "" }); ok {
		t.Fatalf("got a match (%q) for a sibling prefix that is not nested", target)
	}
}

func TestMatchRegistryMultiRootWindowMatchesAnyFolder(t *testing.T) {
	// A multi-root window records every folder; a title could only ever
	// carry one root.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/tardis-community", "/Users/x/tardis-community/scm-analytics-engineers"}},
	}
	if _, ok := matchRegistry(entries, "/Users/x/tardis-community/scm-analytics-engineers", func(string) string { return "" }); !ok {
		t.Fatal("want the second folder of a multi-root window to match")
	}
}

func TestMatchRegistryPrefersTheWorkspaceFileAsFocusTarget(t *testing.T) {
	// `code --reuse-window` keys on the workspace, not on individual
	// folders, so a multi-root window is focused through its
	// .code-workspace file.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/a", "/Users/x/b"}, WorkspaceFile: "/Users/x/ab.code-workspace"},
	}
	target, ok := matchRegistry(entries, "/Users/x/b", func(string) string { return "" })
	if !ok || target != "/Users/x/ab.code-workspace" {
		t.Fatalf("got (%q, %v), want the workspace file", target, ok)
	}
}

func TestMatchRegistrySameFolderInTwoWindowsMatchesEither(t *testing.T) {
	// Both windows registered the same folder; focusing either is
	// correct, and the first one registered wins.
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/dotfiles"}},
		{SessionID: "2", Folders: []string{"/Users/x/dotfiles"}},
	}
	target, ok := matchRegistry(entries, "/Users/x/dotfiles", func(string) string { return "" })
	if !ok || target != "/Users/x/dotfiles" {
		t.Fatalf("got (%q, %v), want a match", target, ok)
	}
}

func TestMatchRegistryWindowWithNoFolderNeverMatches(t *testing.T) {
	entries := []registryEntry{{SessionID: "1", Folders: nil}}
	if _, ok := matchRegistry(entries, "/Users/x/dotfiles", func(string) string { return "" }); ok {
		t.Fatal("a window with no folder open must never match")
	}
}

func TestMatchRegistryOnWorktree(t *testing.T) {
	entries := []registryEntry{
		{SessionID: "1", Folders: []string{"/Users/x/wt/repo"}},
		{SessionID: "2", Folders: []string{"/Users/x/wt/other/pkg"}},
	}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"the worktree root itself", "/Users/x/wt/repo", true},
		{"a window scoped to a subpackage is stranded too", "/Users/x/wt/other", true},
		{"a sibling prefix is not inside", "/Users/x/wt/re", false},
		{"an unrelated path", "/Users/x/nowhere", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchRegistryOnWorktree(entries, tc.path); got != tc.want {
				t.Fatalf("matchRegistryOnWorktree(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestLogRegistryFallbackAppendsOneLine(t *testing.T) {
	dir := t.TempDir()
	logRegistryFallback(dir, "registry-empty", "/Users/x/dotfiles")
	logRegistryFallback(dir, "registry-missing", "/Users/x/canopy")

	data, err := os.ReadFile(filepath.Join(dir, "fallback.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2: %q", len(lines), data)
	}
	if !strings.Contains(lines[0], "registry-empty") || !strings.Contains(lines[0], "/Users/x/dotfiles") {
		t.Fatalf("got first line %q, want reason and path", lines[0])
	}
}

func TestLogRegistryFallbackCreatesTheDirectory(t *testing.T) {
	// The "extension never installed" case must be recorded too, so the
	// log is created from nothing rather than skipped.
	dir := filepath.Join(t.TempDir(), "vscode-windows")
	logRegistryFallback(dir, "registry-missing", "/x")
	if _, err := os.Stat(filepath.Join(dir, "fallback.log")); err != nil {
		t.Fatalf("want fallback.log created in a fresh directory: %v", err)
	}
}

func TestLogRegistryFallbackNeverFailsTheCaller(t *testing.T) {
	// Observability must not break the action it observes: an
	// unwritable directory and an unknown home both return quietly.
	logRegistryFallback("", "registry-missing", "/x")
	logRegistryFallback(filepath.Join(t.TempDir(), "blocked", "dir"), "registry-missing", "/x")
}
