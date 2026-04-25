package main

import (
	"fmt"

	"github.com/kgatilin/archai/internal/buildinfo"
	"github.com/spf13/cobra"
)

// Version is retained as a CLI-package alias so existing
// `-ldflags "-X main.Version=..."` invocations keep working. At
// init time we forward whatever the linker injected into the
// canonical buildinfo.Version, which is what Resolve() actually
// reads.
//
// New build scripts should target buildinfo.Version directly:
//
//	go build -ldflags "-X github.com/kgatilin/archai/internal/buildinfo.Version=v0.1.0" ./cmd/archai
var Version = "dev"

func init() {
	if Version != "" && Version != "dev" {
		buildinfo.Version = Version
	}
}

// resolveVersion returns the effective version string used by
// `archai version`. It delegates to buildinfo.Resolve() so the CLI
// and HTTP surfaces never disagree.
//
// Forwarding rules:
//   - When main.Version was overridden (non-"dev"), it wins — mirrors
//     the legacy `-X main.Version=...` build path.
//   - When main.Version is "dev" but buildinfo.Version was overridden
//     directly (`-X internal/buildinfo.Version=...`), we leave it alone
//     so the new ldflag path takes effect.
//   - Only when both are at their zero value do we fall through to
//     debug.ReadBuildInfo() inside Resolve().
func resolveVersion() string {
	if Version != "" && Version != "dev" {
		buildinfo.Version = Version
	}
	return buildinfo.Resolve().Version
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
