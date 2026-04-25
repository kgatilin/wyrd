// Package buildinfo exposes the version, VCS commit and Go toolchain
// versions of the running archai binary in a single, JSON-friendly
// shape. The CLI's `archai version` command and the HTTP /api/version
// endpoint both render the same Info struct so users never see
// different values for the same build.
//
// Resolution precedence:
//
//  1. Linker-injected Version (set via `-X
//     github.com/kgatilin/archai/internal/buildinfo.Version=v0.1.0`).
//  2. debug.ReadBuildInfo().Main.Version when (1) is still "dev" and
//     module info is available (e.g. `go install`).
//  3. The literal string "dev".
//
// Commit is taken from the VCS settings recorded by `go build` (the
// `vcs.revision` build setting). It is "" when the binary was built
// outside a VCS checkout (e.g. `go run` from a tarball).
package buildinfo

import "runtime/debug"

// Version is overridden at build time via:
//
//	go build -ldflags "-X github.com/kgatilin/archai/internal/buildinfo.Version=v0.1.0" ./cmd/archai
//
// It defaults to "dev" so unversioned builds remain identifiable.
var Version = "dev"

// Info is the resolved build identity reported by the CLI and HTTP
// surfaces. Field tags pin the JSON shape so the HTTP contract is
// stable across refactors.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Go      string `json:"go"`
}

// Resolve returns the effective build identity. It never returns the
// zero value: Version always falls back to "dev" and Go always carries
// the runtime toolchain version. Commit may be empty when no VCS info
// was embedded at build time.
func Resolve() Info {
	info := Info{
		Version: Version,
		Go:      goVersion(),
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if info.Version == "" || info.Version == "dev" {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			info.Version = v
		}
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			info.Commit = s.Value
			break
		}
	}
	return info
}

// goVersion returns the Go toolchain version embedded in the binary,
// or "unknown" when build info is unavailable. Split out so tests can
// stub it via Resolve() if needed in the future.
func goVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.GoVersion != "" {
		return bi.GoVersion
	}
	return "unknown"
}
