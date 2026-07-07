package policy

import (
	"fmt"
	"strings"

	"github.com/kgatilin/archai/internal/overlay"
)

// Parse compiles the raw overlay policy config into a validated Spec. Each
// section constrains which operators its rules may use:
//
//   - allow:        only "->"
//   - forbid:       only "!->"
//   - reachability: "!~>" and "~> … via …"
//
// A malformed rule aborts the whole parse with an error naming the offending
// line, so a bad policy fails fast rather than silently under-enforcing.
func Parse(cfg overlay.PolicyConfig) (*Spec, error) {
	spec := &Spec{DenyByDefault: true}
	if cfg.DenyByDefault != nil {
		spec.DenyByDefault = *cfg.DenyByDefault
	}
	for _, c := range cfg.Components {
		c = strings.TrimSpace(c)
		if c == "" {
			return nil, fmt.Errorf("policy.components: empty component glob")
		}
		spec.Components = append(spec.Components, c)
	}

	sections := []struct {
		name    string
		rules   []string
		allowed map[Op]bool
	}{
		{"allow", cfg.Allow, map[Op]bool{OpAllow: true}},
		{"forbid", cfg.Forbid, map[Op]bool{OpForbid: true}},
		{"reachability", cfg.Reachability, map[Op]bool{OpNoReach: true, OpRequireVia: true}},
	}

	for _, sec := range sections {
		for _, raw := range sec.rules {
			rule, err := parseRule(raw, sec.allowed, sec.name)
			if err != nil {
				return nil, fmt.Errorf("policy.%s: %q: %w", sec.name, raw, err)
			}
			spec.Rules = append(spec.Rules, rule)
		}
	}
	return spec, nil
}

// operatorTokens lists the operators longest/most-specific first so op
// detection never mistakes "!->" for "->" or "!~>" for "~>".
var operatorTokens = []struct {
	tok string
	op  Op
}{
	{"!~>", OpNoReach},
	{"!->", OpForbid},
	{"~>", OpRequireVia},
	{"->", OpAllow},
}

// parseRule parses one rule string. allowed restricts which operators are
// valid in this section; section is used only for error messages.
func parseRule(raw string, allowed map[Op]bool, section string) (Rule, error) {
	body, kinds, err := extractKinds(strings.TrimSpace(raw))
	if err != nil {
		return Rule{}, err
	}

	op, lhsText, rhsText, ok := splitOp(body)
	if !ok {
		return Rule{}, fmt.Errorf("no operator found (expected one of ->, !->, !~>, ~>)")
	}
	if !allowed[op] {
		return Rule{}, fmt.Errorf("operator %q is not valid in the %q section", op, section)
	}

	lhs, err := parseSelectorList(lhsText)
	if err != nil {
		return Rule{}, fmt.Errorf("left side: %w", err)
	}

	rule := Rule{Raw: strings.TrimSpace(raw), Op: op, LHS: lhs, Kinds: kinds}

	if op == OpRequireVia {
		rhsPart, viaPart, hasVia := splitVia(rhsText)
		if !hasVia {
			return Rule{}, fmt.Errorf("~> requires a 'via' clause (A ~> B via C)")
		}
		if rule.RHS, err = parseSelectorList(rhsPart); err != nil {
			return Rule{}, fmt.Errorf("right side: %w", err)
		}
		if rule.Via, err = parseSelectorList(viaPart); err != nil {
			return Rule{}, fmt.Errorf("via side: %w", err)
		}
		return rule, nil
	}

	if _, _, hasVia := splitVia(rhsText); hasVia {
		return Rule{}, fmt.Errorf("'via' is only allowed with the ~> operator")
	}
	if rule.RHS, err = parseSelectorList(rhsText); err != nil {
		return Rule{}, fmt.Errorf("right side: %w", err)
	}
	return rule, nil
}

// extractKinds splits a trailing "(a, b)" edge-kind qualifier off the rule
// body. It returns the body without the qualifier and the parsed kind tokens
// (nil when absent). Package selectors never contain parentheses, so a
// trailing paren group is unambiguously the qualifier.
func extractKinds(s string) (body string, kinds []string, err error) {
	if !strings.HasSuffix(s, ")") {
		return s, nil, nil
	}
	open := strings.LastIndex(s, "(")
	if open < 0 {
		return "", nil, fmt.Errorf("unbalanced ')' in rule")
	}
	inner := strings.TrimSpace(s[open+1 : len(s)-1])
	body = strings.TrimSpace(s[:open])
	if inner == "" {
		return "", nil, fmt.Errorf("empty edge-kind qualifier '()'")
	}
	for tok := range strings.SplitSeq(inner, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return "", nil, fmt.Errorf("empty edge kind in qualifier")
		}
		if !dependencyKindKnown(tok) {
			return "", nil, fmt.Errorf("unknown edge kind %q (want one of: type, uses, returns, implements, extends, nested-in)", tok)
		}
		kinds = append(kinds, tok)
	}
	return body, kinds, nil
}

// splitOp finds the rule's operator and splits the text into the left and
// right operand strings (trimmed). ok is false when no operator is present.
func splitOp(s string) (op Op, lhs, rhs string, ok bool) {
	best := -1
	var bestOp Op
	var bestTok string
	for _, o := range operatorTokens {
		i := strings.Index(s, o.tok)
		if i < 0 {
			continue
		}
		// Prefer the earliest operator; on a tie prefer the more specific
		// (longer) token, which operatorTokens already orders first.
		if best == -1 || i < best {
			best, bestOp, bestTok = i, o.op, o.tok
		}
	}
	if best < 0 {
		return 0, "", "", false
	}
	lhs = strings.TrimSpace(s[:best])
	rhs = strings.TrimSpace(s[best+len(bestTok):])
	return bestOp, lhs, rhs, true
}

// splitVia splits "B via C" into its two halves. hasVia is false when there
// is no " via " token.
func splitVia(s string) (rhs, via string, hasVia bool) {
	before, after, found := strings.Cut(s, " via ")
	if !found {
		return s, "", false
	}
	return strings.TrimSpace(before), strings.TrimSpace(after), true
}

// parseSelectorList splits a comma-separated selector list and parses each
// entry. The list must be non-empty.
func parseSelectorList(s string) ([]Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty selector list")
	}
	var out []Selector
	for tok := range strings.SplitSeq(s, ",") {
		sel, err := parseSelector(strings.TrimSpace(tok))
		if err != nil {
			return nil, err
		}
		out = append(out, sel)
	}
	return out, nil
}

// parseSelector parses one selector token: "@name" is a layer reference,
// anything else is a package glob.
func parseSelector(tok string) (Selector, error) {
	if tok == "" {
		return Selector{}, fmt.Errorf("empty selector")
	}
	if strings.ContainsAny(tok, " \t") {
		return Selector{}, fmt.Errorf("selector %q contains whitespace (did you forget a comma?)", tok)
	}
	if strings.HasPrefix(tok, "@") {
		name := tok[1:]
		if name == "" {
			return Selector{}, fmt.Errorf("layer selector %q has no name", tok)
		}
		return Selector{Raw: tok, Layer: name}, nil
	}
	return Selector{Raw: tok, Glob: tok}, nil
}
