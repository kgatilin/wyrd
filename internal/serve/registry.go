// Package serve implements daemon registry for global daemon discovery.
// This file provides the global daemon registry under ~/.arch/daemons/.

package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kgatilin/archai/internal/worktree"
)

// registryDirName is the subdirectory under archaiHome for daemon records.
const registryDirName = "daemons"

// DaemonRecord is a global daemon record written to ~/.arch/daemons/<key>.
// One record per repo root, keyed by a hash of the repo root path.
type DaemonRecord struct {
	RepoRoot   string   `json:"repo_root"`
	HTTPAddr   string   `json:"http_addr"`
	PID        int      `json:"pid"`
	Caps       []string `json:"caps,omitempty"` // "mcp", "ui", "multi"
	StartedAt  string   `json:"started_at"`     // RFC3339
	Worktrees  []string `json:"worktrees,omitempty"`
	BaseBranch string   `json:"base_branch,omitempty"`
}

// HasCap reports whether the daemon has the given capability.
func (r *DaemonRecord) HasCap(cap string) bool {
	for _, c := range r.Caps {
		if c == cap {
			return true
		}
	}
	return false
}

// archaiHome returns the base directory for archai global state.
// Uses ARCHAI_HOME env var if set, otherwise ~/.arch.
func archaiHome() (string, error) {
	if env := os.Getenv("ARCHAI_HOME"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("registry: cannot determine home dir: %w", err)
	}
	return filepath.Join(home, ".arch"), nil
}

// registryDir returns the path to the daemon registry directory.
func registryDir() (string, error) {
	base, err := archaiHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, registryDirName), nil
}

// repoKey returns a unique filename-safe key for a repo root path.
// Uses SHA256 truncated to 16 chars for brevity while avoiding collisions.
func repoKey(repoRoot string) string {
	// Normalize the path
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		abs = repoRoot
	}
	abs = filepath.Clean(abs)

	h := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(h[:])[:16]
}

// recordPath returns the full path to the record file for a repo root.
func recordPath(repoRoot string) (string, error) {
	dir, err := registryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, repoKey(repoRoot)+".json"), nil
}

// WriteGlobalRecord writes a daemon record to the global registry.
// The containing directory is created if necessary. The write is atomic.
func WriteGlobalRecord(rec DaemonRecord) error {
	dir, err := registryDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("registry: create %s: %w", dir, err)
	}

	path, err := recordPath(rec.RepoRoot)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: marshal record: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("registry: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("registry: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// ReadGlobalRecord reads the daemon record for the given repo root.
// Returns (nil, nil) when no record exists. A stale record (dead PID)
// is treated as non-existent: it is removed and nil is returned.
func ReadGlobalRecord(repoRoot string) (*DaemonRecord, error) {
	path, err := recordPath(repoRoot)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("registry: read %s: %w", path, err)
	}

	var rec DaemonRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		// Malformed file — treat as non-existent and remove.
		_ = os.Remove(path)
		return nil, nil
	}

	// Check if the daemon is still alive.
	if !worktree.PIDAlive(rec.PID) {
		// Stale record — daemon died without cleanup. Remove it.
		_ = os.Remove(path)
		return nil, nil
	}

	return &rec, nil
}

// RemoveGlobalRecord removes the daemon record for the given repo root.
// A missing file is not an error.
func RemoveGlobalRecord(repoRoot string) error {
	path, err := recordPath(repoRoot)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("registry: remove %s: %w", path, err)
	}
	return nil
}

// LiveGlobalDaemon pairs a global daemon record with parsed metadata.
type LiveGlobalDaemon struct {
	Record    DaemonRecord
	StartedAt time.Time // parsed from Record.StartedAt when valid
}

// ListGlobalDaemons scans the global registry and returns all live daemons.
// Stale records (dead PIDs) are cleaned up and not included.
// Results are sorted by repo root path.
func ListGlobalDaemons() ([]LiveGlobalDaemon, error) {
	dir, err := registryDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("registry: read %s: %w", dir, err)
	}

	var out []LiveGlobalDaemon
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var rec DaemonRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			// Malformed — clean up.
			_ = os.Remove(path)
			continue
		}

		if !worktree.PIDAlive(rec.PID) {
			// Stale — clean up.
			_ = os.Remove(path)
			continue
		}

		started, _ := time.Parse(time.RFC3339, rec.StartedAt)
		out = append(out, LiveGlobalDaemon{
			Record:    rec,
			StartedAt: started,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Record.RepoRoot < out[j].Record.RepoRoot
	})
	return out, nil
}

// DiscoverGlobalDaemon looks up the running daemon for the repo containing cwd.
// It resolves the repo root from cwd, then reads the global registry.
// Returns the record (nil when no live daemon is registered) and the repo root.
func DiscoverGlobalDaemon(cwd string) (*DaemonRecord, string, error) {
	repoRoot, ok := worktree.RepoRoot(cwd)
	if !ok {
		// Not in a git repo — fall back to cwd itself.
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return nil, "", fmt.Errorf("registry: resolve %s: %w", cwd, err)
		}
		repoRoot = abs
	}

	rec, err := ReadGlobalRecord(repoRoot)
	if err != nil {
		return nil, repoRoot, err
	}
	return rec, repoRoot, nil
}
