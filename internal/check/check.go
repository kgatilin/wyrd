// Package check runs the CI-facing architecture gates: overlay layer
// rules, the dependency policy, and drift against a locked target.
//
// It exists so the gate a developer runs locally (`wyrd overlay check`,
// `wyrd policy check`, `wyrd validate`) and the gate CI runs
// (`wyrd-check`, the slim validation-only binary) are literally the same
// code — including the wording of the report, which is the thing people
// grep in build logs.
//
// The package deliberately depends on nothing heavy: overlay, policy,
// diff, target and the domain model, plus whatever ModelReader the caller
// wires in. That is what lets cmd/wyrd-check link at a fraction of the
// full binary's size — no diagram renderer, no HTTP server, no MCP
// surface, no embedded review UI.
package check

import (
	"context"

	"github.com/kgatilin/wyrd/internal/service"
)

// Checker runs the architecture gates against a project. Readers are
// injected: the CLI decides that "source" means Go packages and "specs"
// means .arch/*.yaml, this package only knows it can ask for models.
type Checker struct {
	source service.ModelReader
	specs  service.ModelReader
}

// New returns a Checker reading current code through source and frozen
// target/spec models through specs.
func New(source, specs service.ModelReader) *Checker {
	return &Checker{source: source, specs: specs}
}

// defaultPaths is the package pattern used when a caller passes none.
var defaultPaths = []string{"./..."}

func resolvePaths(paths []string) []string {
	if len(paths) == 0 {
		return defaultPaths
	}
	return paths
}

// ensureCtx guards against a nil context handed down from cobra.
func ensureCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
