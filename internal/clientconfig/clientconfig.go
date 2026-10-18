// Package clientconfig reads the user's client configuration,
// $XDG_CONFIG_HOME/wyrd/config.yaml (~/.config/wyrd/config.yaml by default):
// which daemon answers for which repository.
//
//	repos:
//	  ~/src/app:
//	    endpoint: http://app-wyrd.localhost
package clientconfig

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgatilin/wyrd/internal/worktree"
	"gopkg.in/yaml.v3"
)

// URL returns the daemon URL the user configured for the worktree at root —
// <endpoint>/w/<worktree name> — or "" when the repository has no entry.
// It is read independently of the local process registry: a configured
// server must never trigger a local autostart.
func URL(root string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		configDir = filepath.Join(home, ".config")
	}
	path := filepath.Join(configDir, "wyrd", "config.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("client config %s: %w", path, err)
	}
	var cfg struct {
		Repos map[string]struct {
			Endpoint string `yaml:"endpoint"`
		} `yaml:"repos"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return "", fmt.Errorf("client config %s: %w", path, err)
	}
	repo, ok := worktree.RepoRoot(root)
	if !ok {
		return "", nil
	}
	endpoint := ""
	for key, entry := range cfg.Repos {
		if strings.HasPrefix(key, "~/") {
			key = filepath.Join(home, key[2:])
		}
		if !filepath.IsAbs(key) {
			return "", fmt.Errorf("client config %s: repo path %q must be absolute or start with ~/", path, key)
		}
		if samePath(key, repo) {
			if endpoint != "" {
				return "", fmt.Errorf("client config %s: multiple entries for repo %s", path, repo)
			}
			endpoint = strings.TrimSpace(entry.Endpoint)
			if endpoint == "" {
				return "", fmt.Errorf("client config %s: empty endpoint for repo %s", path, repo)
			}
		}
	}
	if endpoint == "" {
		return "", nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("client config %s: endpoint %q must be an HTTP(S) origin, e.g. http://myrepo-wyrd.localhost", path, endpoint)
	}
	return strings.TrimRight(endpoint, "/") + "/w/" + url.PathEscape(worktree.Name(root)), nil
}

// samePath compares two filesystem paths for identity, resolving symlinks so
// /tmp and /private/tmp (or a symlinked checkout) do not read as different
// repositories.
func samePath(a, b string) bool {
	norm := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		return filepath.Clean(p)
	}
	a, b = norm(a), norm(b)
	return a != "" && a == b
}
