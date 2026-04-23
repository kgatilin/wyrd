package target

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	yamlv3 "gopkg.in/yaml.v3"
)

// Directory/file name constants used throughout the storage layout.
const (
	archDirName      = ".arch"
	targetsDirName   = "targets"
	currentFileName  = "CURRENT"
	metaFileName     = "meta.yaml"
	overlayFileName  = "overlay.yaml"
	overlaySource    = "archai.yaml"
	modelDirName     = "model"
	pubYAMLFileName  = "pub.yaml"
	intYAMLFileName  = "internal.yaml"
)

// LockOptions configures a Lock call.
type LockOptions struct {
	// Description is an optional human-readable description stored in meta.yaml.
	Description string
}

// Lock freezes the current per-package .arch/*.yaml snapshots and the
// project's archai.yaml into .arch/targets/<id>/.
//
// It does NOT run `archai diagram generate` — callers must ensure the
// per-package .arch/*.yaml files are up to date (the CLI runs generate
// before invoking Lock; tests populate .arch/ directly).
//
// The project layout after Lock looks like:
//
//	.arch/targets/<id>/meta.yaml
//	.arch/targets/<id>/overlay.yaml              (copy of archai.yaml, optional)
//	.arch/targets/<id>/model/<pkg>/pub.yaml
//	.arch/targets/<id>/model/<pkg>/internal.yaml
//
// Returns an error if the target already exists or if the project
// contains no .arch/*.yaml files to freeze.
func Lock(projectRoot, id string, opts LockOptions) error {
	if id == "" {
		return errors.New("target: id must not be empty")
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("target: id %q must not contain path separators", id)
	}

	targetDir := filepath.Join(projectRoot, archDirName, targetsDirName, id)
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("target: target %q already exists", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("target: stat %s: %w", targetDir, err)
	}

	pkgs, err := findPackagesWithArchYAML(projectRoot)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return errors.New("target: no packages with .arch/*.yaml found — run `archai diagram generate --format yaml` first")
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("target: create %s: %w", targetDir, err)
	}

	// Freeze per-package model snapshots.
	for _, rel := range pkgs {
		srcArch := filepath.Join(projectRoot, rel, archDirName)
		dstModel := filepath.Join(targetDir, modelDirName, rel)
		if err := os.MkdirAll(dstModel, 0o755); err != nil {
			return fmt.Errorf("target: create %s: %w", dstModel, err)
		}
		for _, fn := range []string{pubYAMLFileName, intYAMLFileName} {
			src := filepath.Join(srcArch, fn)
			if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return fmt.Errorf("target: stat %s: %w", src, err)
			}
			dst := filepath.Join(dstModel, fn)
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}

	// Freeze overlay (archai.yaml) if present.
	overlaySrc := filepath.Join(projectRoot, overlaySource)
	if _, err := os.Stat(overlaySrc); err == nil {
		if err := copyFile(overlaySrc, filepath.Join(targetDir, overlayFileName)); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("target: stat %s: %w", overlaySrc, err)
	}

	meta := TargetMeta{
		ID:          id,
		BaseCommit:  gitHead(projectRoot),
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Description: opts.Description,
	}
	if err := writeMeta(filepath.Join(targetDir, metaFileName), meta); err != nil {
		return err
	}
	return nil
}

// List returns metadata for every locked target under .arch/targets/,
// sorted by ID. Entries without a readable meta.yaml are skipped.
func List(projectRoot string) ([]TargetMeta, error) {
	dir := filepath.Join(projectRoot, archDirName, targetsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("target: read %s: %w", dir, err)
	}

	var out []TargetMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(dir, e.Name(), metaFileName)
		meta, err := readMeta(metaPath)
		if err != nil {
			// Skip malformed entries; they're not valid targets.
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Show returns the meta.yaml for <id> together with the list of package
// paths (relative to projectRoot) contained in its model/ directory.
func Show(projectRoot, id string) (*TargetMeta, []string, error) {
	targetDir := filepath.Join(projectRoot, archDirName, targetsDirName, id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("target: target %q not found", id)
		}
		return nil, nil, fmt.Errorf("target: stat %s: %w", targetDir, err)
	}

	meta, err := readMeta(filepath.Join(targetDir, metaFileName))
	if err != nil {
		return nil, nil, err
	}

	pkgs, err := collectModelPackages(filepath.Join(targetDir, modelDirName))
	if err != nil {
		return nil, nil, err
	}
	return &meta, pkgs, nil
}

// Use marks <id> as the active target by writing it to .arch/targets/CURRENT.
// It errors if the target directory does not exist.
func Use(projectRoot, id string) error {
	targetDir := filepath.Join(projectRoot, archDirName, targetsDirName, id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("target: target %q not found", id)
		}
		return fmt.Errorf("target: stat %s: %w", targetDir, err)
	}

	currentPath := filepath.Join(projectRoot, archDirName, targetsDirName, currentFileName)
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o755); err != nil {
		return fmt.Errorf("target: create targets dir: %w", err)
	}
	if err := os.WriteFile(currentPath, []byte(id), 0o644); err != nil {
		return fmt.Errorf("target: write CURRENT: %w", err)
	}
	return nil
}

// Delete removes .arch/targets/<id>/. If <id> is the active target (CURRENT),
// Delete fails unless force is true; when forced, CURRENT is also removed.
func Delete(projectRoot, id string, force bool) error {
	targetDir := filepath.Join(projectRoot, archDirName, targetsDirName, id)
	if _, err := os.Stat(targetDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("target: target %q not found", id)
		}
		return fmt.Errorf("target: stat %s: %w", targetDir, err)
	}

	cur, _ := Current(projectRoot)
	if cur == id && !force {
		return fmt.Errorf("target: %q is the current target; re-run with --force to delete", id)
	}
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("target: remove %s: %w", targetDir, err)
	}
	if cur == id {
		_ = os.Remove(filepath.Join(projectRoot, archDirName, targetsDirName, currentFileName))
	}
	return nil
}

// Current returns the active target id from .arch/targets/CURRENT,
// or an empty string if no CURRENT file exists.
func Current(projectRoot string) (string, error) {
	path := filepath.Join(projectRoot, archDirName, targetsDirName, currentFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("target: read CURRENT: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// --- helpers ---

// findPackagesWithArchYAML walks projectRoot looking for directories
// containing a .arch/ subdirectory with at least one of pub.yaml or
// internal.yaml. Returns relative paths (relative to projectRoot) of
// the parent (package) directories, e.g. "internal/service".
// The .arch/targets/ tree itself is skipped.
func findPackagesWithArchYAML(projectRoot string) ([]string, error) {
	var out []string
	targetsTree := filepath.Join(projectRoot, archDirName, targetsDirName)
	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		// Skip the targets tree so nested .arch/targets/*/model/.arch dirs
		// (if any) don't get picked up.
		if path == targetsTree || strings.HasPrefix(path, targetsTree+string(os.PathSeparator)) {
			return filepath.SkipDir
		}
		if filepath.Base(path) != archDirName {
			return nil
		}
		// Found a .arch directory — check for yaml specs.
		hasPub := fileExists(filepath.Join(path, pubYAMLFileName))
		hasInt := fileExists(filepath.Join(path, intYAMLFileName))
		if !hasPub && !hasInt {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(projectRoot, filepath.Dir(path))
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}
		out = append(out, rel)
		return filepath.SkipDir // don't descend into .arch/
	})
	if err != nil {
		return nil, fmt.Errorf("target: walk %s: %w", projectRoot, err)
	}
	sort.Strings(out)
	return out, nil
}

// collectModelPackages walks a target's model/ directory and returns
// the relative package paths that contain at least one YAML spec.
func collectModelPackages(modelDir string) ([]string, error) {
	if _, err := os.Stat(modelDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(modelDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != pubYAMLFileName && name != intYAMLFileName {
			return nil
		}
		rel, err := filepath.Rel(modelDir, filepath.Dir(path))
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("target: walk %s: %w", modelDir, err)
	}
	// Deduplicate and sort.
	seen := make(map[string]struct{}, len(out))
	uniq := out[:0]
	for _, p := range out {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		uniq = append(uniq, p)
	}
	sort.Strings(uniq)
	return uniq, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("target: open %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("target: mkdir for %s: %w", dst, err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("target: create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("target: copy %s -> %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("target: close %s: %w", dst, err)
	}
	return nil
}

func writeMeta(path string, meta TargetMeta) error {
	data, err := yamlv3.Marshal(meta)
	if err != nil {
		return fmt.Errorf("target: marshal meta: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("target: write %s: %w", path, err)
	}
	return nil
}

func readMeta(path string) (TargetMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TargetMeta{}, fmt.Errorf("target: read %s: %w", path, err)
	}
	var m TargetMeta
	if err := yamlv3.Unmarshal(data, &m); err != nil {
		return TargetMeta{}, fmt.Errorf("target: parse %s: %w", path, err)
	}
	return m, nil
}

// gitHead returns the current git HEAD hash for projectRoot. If git is
// unavailable or the directory is not a git repo, it returns an empty
// string — the lack of a commit id is not fatal for locking a target.
func gitHead(projectRoot string) string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
