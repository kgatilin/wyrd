package main

import (
	"os"

	"github.com/kgatilin/wyrd/internal/clientconfig"
)

// An explicit --daemon selects a local process; otherwise user configuration
// wins over local discovery, including when a local daemon is already running.
func resolveClientURL(daemonArg string) (string, error) {
	if daemonArg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		base, err := clientconfig.URL(cwd)
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
