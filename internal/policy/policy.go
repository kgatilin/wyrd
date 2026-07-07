// Package policy evaluates archai's dependency policy: a concise,
// path-based description of which package-to-package edges are allowed,
// forbidden, or constrained by graph reachability. It compiles the policy
// DSL carried in the archai.yaml overlay (overlay.PolicyConfig) into rules
// over the package dependency graph and reports violations.
//
// The policy is deliberately terse. A rule is one line:
//
//	<selectors> OP <selectors> [via <selectors>] [(kinds)]
//
// where a selector is either "@name" (an overlay layer) or a package glob
// (pkg, pkg/*, pkg/...), and OP is one of:
//
//	->    allow a direct edge          A may depend directly on B
//	!->   forbid a direct edge         A must not depend directly on B
//	!~>   forbid reachability          A must not reach B by any path
//	~>    require a chokepoint          every path A→…→B must pass "via" C
//
// Under deny-by-default (the default), an observed edge is a violation
// unless some allow rule permits it — including edges within one layer.
// See docs/features/dependency-policy/design.md.
package policy

import "github.com/kgatilin/archai/internal/domain"

// Op is a policy operator.
type Op int

const (
	// OpAllow — "A -> B": permit a direct edge A→B. The set of allow rules
	// forms the allow-list consulted under deny-by-default.
	OpAllow Op = iota
	// OpForbid — "A !-> B": forbid a direct edge A→B. A forbid always wins
	// over an allow for the same pair.
	OpForbid
	// OpNoReach — "A !~> B": forbid any transitive path A→…→B.
	OpNoReach
	// OpRequireVia — "A ~> B via C": every transitive path A→…→B must pass
	// through some node in C. A path that reaches B while avoiding all of C
	// is a violation.
	OpRequireVia
)

// String returns the operator's DSL token.
func (o Op) String() string {
	switch o {
	case OpAllow:
		return "->"
	case OpForbid:
		return "!->"
	case OpNoReach:
		return "!~>"
	case OpRequireVia:
		return "~>"
	default:
		return "?"
	}
}

// Selector names a set of packages. Exactly one of Layer or Glob is set.
//   - Layer: an "@name" reference to an overlay layer (later: a tag).
//   - Glob:  a package glob using overlay glob syntax (pkg, pkg/*, pkg/...),
//     matched against module-relative package paths.
type Selector struct {
	Raw   string // the token as written, e.g. "@domain" or "internal/plugins/*"
	Layer string // non-empty when this is an @layer selector
	Glob  string // non-empty when this is a package glob
}

// Rule is one parsed policy rule.
type Rule struct {
	Raw   string     // the original rule text, for diagnostics
	Op    Op         // the operator
	LHS   []Selector // left-hand selectors (the source set)
	RHS   []Selector // right-hand selectors (the target set)
	Via   []Selector // waypoint selectors, only for OpRequireVia
	Kinds []string   // edge-kind qualifier tokens; parsed but not yet enforced (iteration 2)
}

// Spec is a parsed, validated policy.
type Spec struct {
	// DenyByDefault, when true, makes an observed edge a violation unless an
	// allow rule permits it. When false, only forbid/reachability rules apply.
	DenyByDefault bool
	// Rules are all parsed rules across the allow/forbid/reachability sections.
	Rules []Rule
}

// Defined reports whether the spec carries any rule.
func (s *Spec) Defined() bool { return s != nil && len(s.Rules) > 0 }

// Violation is a single policy breach over the package dependency graph.
// All package fields are module-relative paths.
type Violation struct {
	// Kind classifies the breach:
	//   - "forbidden-edge": a !-> rule matched the edge.
	//   - "unlisted-edge":  deny-by-default and no allow rule matched.
	//   - "reachability":   a !~> rule found a forbidden path.
	//   - "chokepoint":     a ~> via rule found a path bypassing the waypoint.
	Kind string
	// Rule is the breached rule text. Empty for an unlisted edge under
	// deny-by-default (no specific rule — the default policy denied it).
	Rule string
	// From and To are the endpoints: the observed edge for edge kinds, or the
	// path endpoints for reachability/chokepoint kinds.
	From string
	To   string
	// Path is an example offending path From→…→To (reachability/chokepoint).
	Path []string
	// Message is a human-readable description.
	Message string
}

// kind constants for Violation.Kind.
const (
	kindForbiddenEdge = "forbidden-edge"
	kindUnlistedEdge  = "unlisted-edge"
	kindReachability  = "reachability"
	kindChokepoint    = "chokepoint"
)

// dependencyKindKnown reports whether k is a domain edge kind the policy DSL
// recognizes in a (kinds) qualifier. It exists so the parser can validate
// qualifier tokens against the real domain vocabulary even though iteration 1
// does not yet enforce them.
func dependencyKindKnown(k string) bool {
	switch domain.DependencyKind(k) {
	case domain.DependencyUses, domain.DependencyReturns,
		domain.DependencyImplements, domain.DependencyExtends,
		domain.DependencyNestedIn:
		return true
	}
	// "type" is a DSL shorthand for the type-reference kinds (uses/returns),
	// resolved in iteration 2; accept it at parse time.
	return k == "type"
}
