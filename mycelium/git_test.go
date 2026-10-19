package mycelium

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitToplevel has two paths: the in-process worktreeRoot fast path
// (main checkout, linked worktree — the answer understory's rows are
// built from, which must not spawn a subprocess per row per poll), and
// the git fallback for anything else (a subdirectory, a broken .git,
// a non-repo). These tests exercise real repositories, because the
// fast path's whole point is equivalence with git's own answer.

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v: %s", args, err, out)
		}
	}
	run("init")
	// worktree add needs at least one commit to point the new tree at.
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
}

func TestGitToplevelMainCheckoutRoot(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	if got := gitToplevel(dir); got != dir {
		t.Fatalf("gitToplevel(%q) = %q, want the checkout root itself", dir, got)
	}
}

func TestGitToplevelLinkedWorktreeRoot(t *testing.T) {
	main := t.TempDir()
	gitInit(t, main)
	wt := filepath.Join(t.TempDir(), "wt")
	out, err := exec.Command("git", "-C", main, "worktree", "add", "--detach", wt).CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree add failed: %v: %s", err, out)
	}
	// t.TempDir sits under /var, a symlink to /private/var on macOS, and
	// git reports the resolved path; the fast path reports the worktree
	// path as given (exactly what callers match titles against), so both
	// spellings are correct answers here.
	resolved, _ := filepath.EvalSymlinks(wt)
	if got := gitToplevel(wt); got != wt && got != resolved {
		t.Fatalf("gitToplevel(%q) = %q, want the worktree root (%q or %q)", wt, got, wt, resolved)
	}
}

func TestGitToplevelSubdirectorySpawnsGitAndWalksUp(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	sub := filepath.Join(dir, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same /var symlink note as the worktree test: git resolves, so
	// either spelling of the root is a correct answer.
	resolved, _ := filepath.EvalSymlinks(dir)
	if got := gitToplevel(sub); got != dir && got != resolved {
		t.Fatalf("gitToplevel(%q) = %q, want the enclosing root (%q or %q)", sub, got, dir, resolved)
	}
}

func TestGitToplevelNonRepoIsEmpty(t *testing.T) {
	dir := t.TempDir()

	if got := gitToplevel(dir); got != "" {
		t.Fatalf("gitToplevel(%q) = %q, want \"\" outside a repo", dir, got)
	}
}

func TestGitToplevelBrokenDotGitFallsBackToGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := gitToplevel(dir); got != "" {
		t.Fatalf("gitToplevel(%q) = %q, want \"\": a .git dir without a HEAD is not a repo", dir, got)
	}
}

func TestWorktreeRootRejectsStrayDotGitFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("not a gitdir pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := worktreeRoot(dir); ok {
		t.Fatal("worktreeRoot true for a .git file without a gitdir: pointer")
	}
}
