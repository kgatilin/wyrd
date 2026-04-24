package worktree

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Entry describes a single git worktree as discovered by
// `git worktree list --porcelain`. Path is the absolute filesystem
// path to the worktree root; Name is filepath.Base(Path). Branch is
// the current branch (may be empty for detached HEAD). Bare is true
// for bare repositories (these are skipped by Discover since they
// have no working tree).
type Entry struct {
	Path   string
	Name   string
	Branch string
	Head   string
	Bare   bool
}

// Discover runs `git worktree list --porcelain` from projectRoot and
// returns the parsed entries, excluding bare repos. On success the
// result is sorted by Name. When git is unavailable or projectRoot is
// not inside a git repo, Discover returns a single synthetic entry
// matching projectRoot so callers can still serve the current
// directory without a git repo.
func Discover(projectRoot string) ([]Entry, error) {
	cmd := exec.Command("git", "-C", projectRoot, "worktree", "list", "--porcelain")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		// Fallback: treat projectRoot as a single standalone worktree.
		abs, aerr := filepath.Abs(projectRoot)
		if aerr != nil {
			abs = projectRoot
		}
		return []Entry{{Path: abs, Name: filepath.Base(abs)}}, nil
	}
	entries, err := ParseWorktreeList(buf.String())
	if err != nil {
		return nil, fmt.Errorf("worktree: parse list: %w", err)
	}
	var out []Entry
	for _, e := range entries {
		if e.Bare {
			continue
		}
		if e.Name == "" {
			e.Name = filepath.Base(e.Path)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ParseWorktreeList parses the porcelain output of
// `git worktree list --porcelain`. Each record is terminated by a
// blank line; fields are whitespace-separated "key value" pairs with
// a leading line of "worktree <path>". The "bare" and "detached"
// lines have no value. Unknown keys are ignored.
func ParseWorktreeList(output string) ([]Entry, error) {
	var out []Entry
	var cur *Entry
	flush := func() {
		if cur == nil {
			return
		}
		if cur.Path != "" && cur.Name == "" {
			cur.Name = filepath.Base(cur.Path)
		}
		out = append(out, *cur)
		cur = nil
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			flush()
			continue
		}
		// "worktree <path>" starts a new record.
		if strings.HasPrefix(line, "worktree ") {
			flush()
			cur = &Entry{Path: strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case line == "bare":
			cur.Bare = true
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			// Value is a ref like refs/heads/main.
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "detached":
			// No branch value. Branch stays empty.
		}
	}
	flush()
	return out, nil
}
