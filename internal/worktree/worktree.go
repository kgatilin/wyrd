// Package worktree provides per-worktree state management for the archai
// daemon. Multiple git worktrees of the same project share the committed
// .arch/targets/ tree but maintain independent runtime state (active
// target pointer, running-daemon record) under .arch/.worktree/<name>/.
//
// Layout:
//
//	.arch/targets/<id>/                 (shared, committed)
//	.arch/archai.yaml                   (shared, committed — if present)
//	.arch/.worktree/<name>/CURRENT      (per-worktree, gitignored)
//	.arch/.worktree/<name>/serve.json   (per-worktree, gitignored)
//
// The worktree <name> is derived from filepath.Base of `git rev-parse
// --show-toplevel`; callers that are not in a git repo fall back to the
// base name of the project root.
package worktree

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Directory/file name constants.
const (
	archDirName      = ".arch"
	worktreeDirName  = ".worktree"
	currentFileName  = "CURRENT"
	serveFileName    = "serve.json"
	legacyTargetsDir = "targets" // .arch/targets/CURRENT migration path
)

// Name returns the worktree name for projectRoot. It invokes
// `git -C <projectRoot> rev-parse --show-toplevel` and takes the base
// name; when git is unavailable or the directory is not a git repo, it
// falls back to filepath.Base(projectRoot).
func Name(projectRoot string) string {
	if top, ok := gitTopLevel(projectRoot); ok {
		return filepath.Base(top)
	}
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return filepath.Base(projectRoot)
	}
	return filepath.Base(abs)
}

// Dir returns the per-worktree state directory
// (.arch/.worktree/<name>/) under projectRoot. The directory is NOT
// created by this function.
func Dir(projectRoot, name string) string {
	return filepath.Join(projectRoot, archDirName, worktreeDirName, name)
}

// CurrentPath returns the absolute path to this worktree's CURRENT
// pointer.
func CurrentPath(projectRoot, name string) string {
	return filepath.Join(Dir(projectRoot, name), currentFileName)
}

// ServePath returns the absolute path to this worktree's serve.json.
func ServePath(projectRoot, name string) string {
	return filepath.Join(Dir(projectRoot, name), serveFileName)
}

// LegacyCurrentPath returns the pre-M9 CURRENT location under
// .arch/targets/CURRENT. Kept as a migration fallback.
func LegacyCurrentPath(projectRoot string) string {
	return filepath.Join(projectRoot, archDirName, legacyTargetsDir, currentFileName)
}

// ReadCurrent reads the active target id for the given worktree. When
// .arch/.worktree/<name>/CURRENT is missing, it falls back to the
// legacy .arch/targets/CURRENT location; when neither exists, it
// returns an empty id and no error. The second return value is true
// when the legacy path was used (callers log a deprecation warning on
// the first such read).
func ReadCurrent(projectRoot, name string) (id string, fromLegacy bool, err error) {
	primary := CurrentPath(projectRoot, name)
	data, err := os.ReadFile(primary)
	if err == nil {
		return strings.TrimSpace(string(data)), false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", false, fmt.Errorf("worktree: read %s: %w", primary, err)
	}

	// Fallback: legacy shared CURRENT file.
	legacy := LegacyCurrentPath(projectRoot)
	data, err = os.ReadFile(legacy)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("worktree: read %s: %w", legacy, err)
	}
	return strings.TrimSpace(string(data)), true, nil
}

// WriteCurrent sets the active target id for the given worktree. The
// file is written atomically (temp file + rename).
func WriteCurrent(projectRoot, name, id string) error {
	if err := os.MkdirAll(Dir(projectRoot, name), 0o755); err != nil {
		return fmt.Errorf("worktree: create %s: %w", Dir(projectRoot, name), err)
	}
	return atomicWrite(CurrentPath(projectRoot, name), []byte(id), 0o644)
}

// RemoveCurrent deletes the per-worktree CURRENT pointer. A missing
// file is not an error.
func RemoveCurrent(projectRoot, name string) error {
	err := os.Remove(CurrentPath(projectRoot, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ServeRecord is the JSON payload written to serve.json by a running
// daemon so peer tools (`archai where`, `archai list-daemons`) can
// discover live daemons without probing ports.
type ServeRecord struct {
	PID       int    `json:"pid"`
	HTTPAddr  string `json:"http_addr"`
	StartedAt string `json:"started_at"` // RFC3339
}

// WriteServe writes rec to .arch/.worktree/<name>/serve.json atomically.
// The containing directory is created if necessary.
func WriteServe(projectRoot, name string, rec ServeRecord) error {
	if err := os.MkdirAll(Dir(projectRoot, name), 0o755); err != nil {
		return fmt.Errorf("worktree: create %s: %w", Dir(projectRoot, name), err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("worktree: marshal serve.json: %w", err)
	}
	return atomicWrite(ServePath(projectRoot, name), data, 0o644)
}

// ReadServe reads this worktree's serve.json. Returns (nil, nil) when
// the file does not exist.
func ReadServe(projectRoot, name string) (*ServeRecord, error) {
	return readServeFile(ServePath(projectRoot, name))
}

// RemoveServe deletes this worktree's serve.json. A missing file is
// not an error.
func RemoveServe(projectRoot, name string) error {
	err := os.Remove(ServePath(projectRoot, name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// LiveDaemon pairs a worktree name with the decoded serve.json record
// and its mtime (used as a best-effort uptime source). Only daemons
// whose PID responds to signal 0 (i.e. the process exists) are
// returned by ListDaemons.
type LiveDaemon struct {
	Worktree  string
	Record    ServeRecord
	StartedAt time.Time // parsed from Record.StartedAt when valid
}

// ListDaemons scans .arch/.worktree/*/serve.json under projectRoot and
// returns records whose PID still refers to a live process. Stale
// records (process exited without cleanup) are skipped. The result is
// sorted by worktree name.
func ListDaemons(projectRoot string) ([]LiveDaemon, error) {
	root := filepath.Join(projectRoot, archDirName, worktreeDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("worktree: read %s: %w", root, err)
	}

	var out []LiveDaemon
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wt := e.Name()
		rec, err := readServeFile(filepath.Join(root, wt, serveFileName))
		if err != nil || rec == nil {
			continue
		}
		if !PIDAlive(rec.PID) {
			continue
		}
		started, _ := time.Parse(time.RFC3339, rec.StartedAt)
		out = append(out, LiveDaemon{
			Worktree:  wt,
			Record:    *rec,
			StartedAt: started,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Worktree < out[j].Worktree })
	return out, nil
}

// PIDAlive reports whether pid refers to a process we can signal (i.e.
// the process exists and is visible to this user). A zero/negative pid
// is treated as dead.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; signal 0 probes existence.
	if err := proc.Signal(syscallZero); err != nil {
		return false
	}
	return true
}

// --- helpers ---

// atomicWrite writes data to path via <path>.tmp + rename so readers
// never see a half-written file. The parent directory is assumed to
// exist.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("worktree: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("worktree: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// gitTopLevel returns the output of `git -C dir rev-parse
// --show-toplevel`, trimmed. The second return value is false when git
// is unavailable or dir is not inside a git worktree.
func gitTopLevel(dir string) (string, bool) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return "", false
	}
	out := strings.TrimSpace(buf.String())
	if out == "" {
		return "", false
	}
	return out, true
}

// readServeFile decodes a serve.json record from path, tolerating
// missing files (returns nil, nil) and surfaces other errors.
func readServeFile(path string) (*ServeRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("worktree: read %s: %w", path, err)
	}
	var rec ServeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("worktree: parse %s: %w", path, err)
	}
	return &rec, nil
}
