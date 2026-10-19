package mycelium

import (
	"os"
	"os/exec"
	"strings"
)

// gitToplevel returns the root of the git work tree containing dir, or ""
// when dir isn't inside one (not a repo, or git not installed — either way
// the nested-window match simply has nothing solid to work with, and fails
// closed rather than guessing from a raw path prefix).
//
// A path that is itself a work-tree root is answered in process
// (see worktreeRoot): understory polls one IsOpen per worktree row on
// every poll, every row is a work-tree root, and the memoized map only
// saves repeat queries within one snapshot — the first query per row
// paid a git subprocess whose exec tax (endpoint-security agents tax
// every spawn by a quarter second or more) blocked the TUI's Update
// goroutine for seconds per poll. Only a path that is not itself a
// root spawns git, to walk up to the enclosing tree (canopy hands an
// agent's cwd over as-is, deep inside a checkout). A broken .git
// entry falls back to git, which fails the same way and returns "".
func gitToplevel(dir string) string {
	if root, ok := worktreeRoot(dir); ok {
		return root
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// worktreeRoot reports whether dir is itself a git work-tree root, the
// answer `git rev-parse --show-toplevel` would return for it: a .git
// directory whose HEAD is readable (main checkout), or a .git pointer
// file whose `gitdir:` target holds a readable HEAD (linked worktree).
// The check mirrors the dotfiles zsh title hook's _my_title_repo_info:
// in-process filesystem reads only, no subprocess. False means "not a
// root, or broken": the caller spawns git to walk up or fail closed.
func worktreeRoot(dir string) (string, bool) {
	gitPath := dir + "/.git"
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		if headReadable(gitPath + "/HEAD") {
			return dir, true
		}
		return "", false
	}
	// Linked worktree: .git is a "gitdir: <path>" pointer file.
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(data), "\n")
	pointer, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", false
	}
	pointer = strings.TrimSpace(pointer)
	if pointer == "" || !headReadable(pointer+"/HEAD") {
		return "", false
	}
	return dir, true
}

// headReadable reports whether path names a readable regular file —
// the one plausibility check that separates a real worktree admin dir
// from a stray directory named .git, without paying a subprocess.
func headReadable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
