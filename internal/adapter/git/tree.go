package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// ListTree returns every tracked path at rev, repository-relative and
// slash-separated.
//
// Its caller wants a handful of declaration files out of a commit, not the
// commit. ExportTree materializes the whole thing, which is the right trade
// when a Go model has to be parsed and the wrong one when the answer is nine
// YAML files: on a large repository that is tens of thousands of writes to
// disk for a diff of an event model.
func ListTree(repoPath, rev string) ([]string, error) {
	if rev == "" {
		return nil, fmt.Errorf("list tree: empty revision")
	}
	out, err := run(repoPath, "ls-tree", "-r", "--name-only", "-z", rev)
	if err != nil {
		return nil, fmt.Errorf("list tree %s: %w", rev, err)
	}
	// -z so a path with a newline or a quotable byte in it arrives whole
	// rather than as git's quoted rendering of itself.
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// ShowFile returns the contents of one repository-relative path at rev.
//
// A missing path is an error rather than empty bytes: every caller here got
// the path out of ListTree at the same rev, so absence means the tree moved
// under it, and a silently empty file parses as a declaration that declares
// nothing.
func ShowFile(repoPath, rev, path string) ([]byte, error) {
	if rev == "" {
		return nil, fmt.Errorf("show file: empty revision")
	}
	cmd := exec.Command("git", "-C", repoPath, "show", rev+":"+path)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("show %s:%s: %w: %s", rev, path, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("show %s:%s: %w", rev, path, err)
	}
	return out, nil
}
