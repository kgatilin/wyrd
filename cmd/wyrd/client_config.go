package main

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

// configuredClientURL resolves the user's repo endpoint independently of the
// local process registry. A configured server must never trigger local autostart.
func configuredClientURL(root string) (string, error) {
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
		if samePathOnDisk(key, repo) {
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

// An explicit --daemon selects a local process; otherwise user configuration
// wins over local discovery, including when a local daemon is already running.
func resolveClientURL(daemonArg string) (string, error) {
	if daemonArg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base, err := configuredClientURL(cwd)
		if err != nil || base != "" {
			return base, err
		}
	}
	rec, err := resolveDaemonTarget(daemonArg)
	if err != nil {
		return "", err
	}
	return daemonBaseURL(rec), nil
}
