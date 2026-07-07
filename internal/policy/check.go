package policy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// Check evaluates spec against the package dependency graph in models and
// returns the violations, sorted deterministically. models must already be
// layer-annotated by overlay.Merge (Check reads PackageModel.Layer to resolve
// @layer selectors); cfg supplies the module path used to normalize package
// ids. A nil or empty spec yields no violations.
func Check(spec *Spec, models []domain.PackageModel, cfg *overlay.Config) ([]Violation, error) {
	if !spec.Defined() {
		return nil, nil
	}
	module := ""
	if cfg != nil {
		module = cfg.Module
	}
	g := newGraph(models, module)

	// Pre-resolve each rule's selector sets once, grouped by operator.
	var allowRules, forbidRules, noReachRules, viaRules []resolvedRule
	for _, r := range spec.Rules {
		rr := resolvedRule{rule: r, lhs: g.resolve(r.LHS), rhs: g.resolve(r.RHS)}
		switch r.Op {
		case OpAllow:
			allowRules = append(allowRules, rr)
		case OpForbid:
			forbidRules = append(forbidRules, rr)
		case OpNoReach:
			noReachRules = append(noReachRules, rr)
		case OpRequireVia:
			rr.via = g.resolve(r.Via)
			viaRules = append(viaRules, rr)
		}
	}

	// Precompute each package's component once so the same-component allowance
	// is a cheap lookup in the edge loop.
	compOf := make(map[string]string, len(g.pkgs))
	for _, p := range g.pkgs {
		compOf[p] = componentOf(p, spec.Components)
	}
	sameComponent := func(a, b string) bool {
		return len(spec.Components) > 0 && compOf[a] == compOf[b]
	}

	var violations []Violation

	// Direct-edge rules over the observed edges. Forbid wins over everything;
	// otherwise a same-component edge (cohesion) or an allow-listed edge passes,
	// and under deny-by-default anything else is a violation.
	for _, e := range g.edges() {
		if fr, ok := firstMatch(forbidRules, e); ok {
			violations = append(violations, Violation{
				Kind: kindForbiddenEdge, Rule: fr.rule.Raw,
				From: e.from, To: e.to,
				Message: fmt.Sprintf("%s must not depend on %s (forbidden by %q)",
					e.from, e.to, fr.rule.Raw),
			})
			continue
		}
		if sameComponent(e.from, e.to) {
			continue
		}
		if spec.DenyByDefault {
			if _, ok := firstMatch(allowRules, e); !ok {
				violations = append(violations, Violation{
					Kind: kindUnlistedEdge, From: e.from, To: e.to,
					Message: fmt.Sprintf("%s depends on %s, which no allow rule permits (deny-by-default)",
						e.from, e.to),
				})
			}
		}
	}

	// Reachability: "A !~> B" — any path from a source to a target is a breach.
	for _, rr := range noReachRules {
		for _, src := range sortedKeys(rr.lhs) {
			if p := g.findPath(src, rr.rhs, nil); p != nil {
				violations = append(violations, Violation{
					Kind: kindReachability, Rule: rr.rule.Raw,
					From: p[0], To: p[len(p)-1], Path: p,
					Message: fmt.Sprintf("%s must not reach %s, but does: %s (forbidden by %q)",
						p[0], p[len(p)-1], strings.Join(p, " -> "), rr.rule.Raw),
				})
			}
		}
	}

	// Chokepoint: "A ~> B via C" — a path to a target that avoids every
	// waypoint is a breach.
	for _, rr := range viaRules {
		for _, src := range sortedKeys(rr.lhs) {
			if p := g.findPath(src, rr.rhs, rr.via); p != nil {
				violations = append(violations, Violation{
					Kind: kindChokepoint, Rule: rr.rule.Raw,
					From: p[0], To: p[len(p)-1], Path: p,
					Message: fmt.Sprintf("%s reaches %s bypassing the required waypoint: %s (%q)",
						p[0], p[len(p)-1], strings.Join(p, " -> "), rr.rule.Raw),
				})
			}
		}
	}

	sortViolations(violations)
	return violations, nil
}

// resolvedRule is a rule with its selector sets pre-resolved to package ids.
type resolvedRule struct {
	rule Rule
	lhs  map[string]bool
	rhs  map[string]bool
	via  map[string]bool
}

// firstMatch returns the first rule whose lhs contains e.from and rhs contains
// e.to.
func firstMatch(rules []resolvedRule, e edge) (resolvedRule, bool) {
	for _, r := range rules {
		if r.lhs[e.from] && r.rhs[e.to] {
			return r, true
		}
	}
	return resolvedRule{}, false
}

// sortedKeys returns the map keys in ascending order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortViolations orders violations deterministically for stable output.
func sortViolations(vs []Violation) {
	sort.Slice(vs, func(i, j int) bool {
		if vs[i].From != vs[j].From {
			return vs[i].From < vs[j].From
		}
		if vs[i].To != vs[j].To {
			return vs[i].To < vs[j].To
		}
		if vs[i].Kind != vs[j].Kind {
			return vs[i].Kind < vs[j].Kind
		}
		return vs[i].Rule < vs[j].Rule
	})
}
