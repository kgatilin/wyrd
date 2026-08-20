package archreview

import (
	"math"
	"sort"

	"github.com/kgatilin/archai/internal/policy"
)

// policyViolations tags package edges with the dependency policy's verdict,
// keyed by the edge. Only direct-edge verdicts land here: a reachability or
// chokepoint breach is about a path, and pinning it on one of the path's edges
// would name the wrong culprit.
//
// The policy is not re-derived here — internal/policy parses and evaluates it,
// the same evaluator archai's CI gate runs.
func policyViolations(s *side) map[Edge]string {
	out := map[Edge]string{}
	if s.overlay == nil || !s.overlay.Policy.Defined() {
		return out
	}
	spec, err := policy.Parse(s.overlay.Policy)
	if err != nil {
		return out
	}
	violations, err := policy.Check(spec, s.models, s.overlay)
	if err != nil {
		return out
	}
	for _, v := range violations {
		switch v.Kind {
		case "forbidden-edge", "unlisted-edge":
			out[Edge{From: v.From, To: v.To}] = v.Message
		}
	}
	return out
}

// godPackages returns the packages whose degree is an outlier: at least the
// mean plus one and a half standard deviations, and at least four. Both
// thresholds are the ones the analysis lenses already use, so a package the
// report calls overloaded is the one the lenses call overloaded.
func godPackages(s *side, degree map[string]int) []string {
	if len(s.pkgs) < 4 {
		return nil
	}
	var sum float64
	for _, pkg := range s.pkgs {
		sum += float64(degree[pkg])
	}
	mean := sum / float64(len(s.pkgs))
	var variance float64
	for _, pkg := range s.pkgs {
		delta := float64(degree[pkg]) - mean
		variance += delta * delta
	}
	threshold := mean + 1.5*math.Sqrt(variance/float64(len(s.pkgs)))

	var out []string
	for _, pkg := range s.pkgs {
		if degree[pkg] >= 4 && float64(degree[pkg]) >= threshold {
			out = append(out, pkg)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if degree[out[i]] != degree[out[j]] {
			return degree[out[i]] > degree[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
