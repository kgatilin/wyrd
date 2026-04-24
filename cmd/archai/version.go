package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the archai binary version. Defaults to "dev" for
// unversioned builds and is overridden at build time via ldflags:
//
//	go build -ldflags "-X main.Version=v0.1.0" ./cmd/archai
//
// When Version is still "dev" at runtime we fall back to debug.ReadBuildInfo
// so `go install` / `go run` users get a meaningful version when available.
var Version = "dev"

// resolveVersion returns the effective version string used by
// `archai version`. It prefers the linker-injected Version; if that
// hasn't been set it tries debug.ReadBuildInfo so module-managed
// installs report their module version instead of the literal "dev".
func resolveVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// newVersionCmd returns the `archai version` command. Output is a
// single line: `archai <version>`.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print archai version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "archai %s\n", resolveVersion())
		},
	}
}
