package git

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExportTree materializes the tracked tree of rev into dest.
//
// Reviewing a branch against a merge base means parsing the code as it was
// at that commit, and the Go model reader needs real files on disk. A
// `git worktree add` would do it, but it would also enlist the checkout in
// `git worktree list` — where it would surface in this UI's branch picker
// and in every other tool the user points at the repo. Streaming
// `git archive` into a plain directory keeps the materialized base
// invisible to git.
//
// Only tracked files at rev are written, which is exactly the definition of
// the commit. dest is created if missing; existing files are overwritten.
func ExportTree(repoPath, rev, dest string) error {
	if rev == "" {
		return fmt.Errorf("export tree: empty revision")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("export tree: create %s: %w", dest, err)
	}

	cmd := exec.Command("git", "-C", repoPath, "archive", "--format=tar", rev)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("export tree: pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("export tree: start git archive: %w", err)
	}

	extractErr := extractTar(stdout, dest)
	// Drain whatever is left so git never blocks on a full pipe when
	// extraction stopped early.
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("export tree %s: %w: %s", rev, err, strings.TrimSpace(stderr.String()))
	}
	return extractErr
}

func extractTar(r io.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("export tree: read archive: %w", err)
		}
		target, err := safeJoin(dest, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("export tree: mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("export tree: mkdir %s: %w", filepath.Dir(target), err)
			}
			if err := writeFile(target, tr, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("export tree: mkdir %s: %w", filepath.Dir(target), err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("export tree: symlink %s: %w", target, err)
			}
		default:
			// Anything else (devices, fifos) cannot appear in a git tree.
		}
	}
}

func writeFile(path string, r io.Reader, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o644
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("export tree: create %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("export tree: write %s: %w", path, err)
	}
	return nil
}

// safeJoin resolves an archive entry against dest, rejecting anything that
// would escape it. git archive never emits such a path, but an extractor
// that trusts its input is a well-known way to write outside the target.
func safeJoin(dest, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("export tree: refusing entry outside destination: %q", name)
	}
	return filepath.Join(dest, clean), nil
}
