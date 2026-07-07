package policy

import (
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

const testModule = "example.com/m"

// full returns a fully-qualified package path, as overlay.Merge leaves on
// PackageModel.Path in the real pipeline.
func full(rel string) string { return testModule + "/" + rel }

// pkg builds a PackageModel at a module-relative path with a layer and a set
// of internal dependencies (given as module-relative target paths).
func pkg(rel, layer string, deps ...string) domain.PackageModel {
	m := domain.PackageModel{Path: full(rel), Layer: layer}
	for _, d := range deps {
		m.Dependencies = append(m.Dependencies, domain.Dependency{
			From: domain.SymbolRef{Package: full(rel), Symbol: "S"},
			To:   domain.SymbolRef{Package: full(d), Symbol: "T"},
			Kind: domain.DependencyUses,
		})
	}
	return m
}

func boolPtr(b bool) *bool { return &b }

func cfg() *overlay.Config { return &overlay.Config{Module: testModule} }

func mustParse(t *testing.T, pc overlay.PolicyConfig) *Spec {
	t.Helper()
	spec, err := Parse(pc)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	return spec
}

func TestParseValid(t *testing.T) {
	pc := overlay.PolicyConfig{
		Allow:        []string{"@app -> @domain, @infra", "a -> b (type)"},
		Forbid:       []string{"@domain/* !-> @domain/*"},
		Reachability: []string{"a !~> c", "a ~> c via b"},
	}
	spec := mustParse(t, pc)
	if !spec.DenyByDefault {
		t.Errorf("DenyByDefault: want true by default, got false")
	}
	if len(spec.Rules) != 5 {
		t.Fatalf("rules: want 5, got %d", len(spec.Rules))
	}

	// "@app -> @domain, @infra"
	r0 := spec.Rules[0]
	if r0.Op != OpAllow || len(r0.LHS) != 1 || r0.LHS[0].Layer != "app" {
		t.Errorf("rule0 LHS: %+v", r0)
	}
	if len(r0.RHS) != 2 || r0.RHS[0].Layer != "domain" || r0.RHS[1].Layer != "infra" {
		t.Errorf("rule0 RHS: %+v", r0.RHS)
	}

	// "a -> b (type)" — qualifier parsed, glob selectors.
	r1 := spec.Rules[1]
	if r1.LHS[0].Glob != "a" || r1.RHS[0].Glob != "b" {
		t.Errorf("rule1 selectors: %+v", r1)
	}
	if len(r1.Kinds) != 1 || r1.Kinds[0] != "type" {
		t.Errorf("rule1 kinds: want [type], got %v", r1.Kinds)
	}

	// forbid section
	if spec.Rules[2].Op != OpForbid {
		t.Errorf("rule2 op: want forbid, got %v", spec.Rules[2].Op)
	}
	// reachability
	if spec.Rules[3].Op != OpNoReach {
		t.Errorf("rule3 op: want !~>, got %v", spec.Rules[3].Op)
	}
	via := spec.Rules[4]
	if via.Op != OpRequireVia || len(via.Via) != 1 || via.Via[0].Glob != "b" {
		t.Errorf("rule4 via: %+v", via)
	}
}

func TestParseDenyByDefaultFalse(t *testing.T) {
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Forbid:        []string{"a !-> b"},
	})
	if spec.DenyByDefault {
		t.Errorf("DenyByDefault: want false")
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		pc   overlay.PolicyConfig
		want string
	}{
		{"no operator", overlay.PolicyConfig{Allow: []string{"a b"}}, "no operator"},
		{"wrong op in allow", overlay.PolicyConfig{Allow: []string{"a !-> b"}}, "not valid in"},
		{"wrong op in forbid", overlay.PolicyConfig{Forbid: []string{"a -> b"}}, "not valid in"},
		{"via without op", overlay.PolicyConfig{Allow: []string{"a -> b via c"}}, "only allowed with"},
		{"reach requires via", overlay.PolicyConfig{Reachability: []string{"a ~> b"}}, "requires a 'via'"},
		{"unknown kind", overlay.PolicyConfig{Allow: []string{"a -> b (bogus)"}}, "unknown edge kind"},
		{"empty rhs", overlay.PolicyConfig{Allow: []string{"a -> "}}, "empty selector"},
		{"empty lhs", overlay.PolicyConfig{Allow: []string{" -> b"}}, "empty selector"},
		{"empty kinds", overlay.PolicyConfig{Allow: []string{"a -> b ()"}}, "empty edge-kind"},
		{"bare @", overlay.PolicyConfig{Allow: []string{"@ -> b"}}, "no name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.pc)
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCheckDenyByDefault(t *testing.T) {
	models := []domain.PackageModel{
		pkg("d", "domain"),
		pkg("a1", "app", "d"),  // app -> domain: allowed
		pkg("a2", "app", "a1"), // app -> app: not covered by allow => unlisted
	}
	spec := mustParse(t, overlay.PolicyConfig{
		Allow: []string{"@app -> @domain"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Kind != kindUnlistedEdge || v.From != "a2" || v.To != "a1" {
		t.Errorf("violation: %+v", v)
	}
}

func TestCheckDenyByDefaultOffAllowsUnlisted(t *testing.T) {
	models := []domain.PackageModel{
		pkg("d", "domain"),
		pkg("a2", "app", "a1"),
		pkg("a1", "app", "d"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("blacklist mode with no forbid rules: want 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestForbidWinsOverAllow(t *testing.T) {
	models := []domain.PackageModel{
		pkg("d", "domain"),
		pkg("a1", "app", "d"),
		pkg("a2", "app", "a1"), // permitted by @app->@app, but explicitly forbidden
	}
	spec := mustParse(t, overlay.PolicyConfig{
		Allow:  []string{"@app -> @domain, @app"},
		Forbid: []string{"a2 !-> a1"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].Kind != kindForbiddenEdge || !strings.Contains(vs[0].Rule, "a2 !-> a1") {
		t.Errorf("violation: %+v", vs[0])
	}
}

func TestCheckNoReach(t *testing.T) {
	// a -> b -> c
	models := []domain.PackageModel{
		pkg("a", "x", "b"),
		pkg("b", "x", "c"),
		pkg("c", "x"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"a !~> c"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 reachability violation, got %d: %+v", len(vs), vs)
	}
	v := vs[0]
	if v.Kind != kindReachability {
		t.Errorf("kind: %+v", v)
	}
	if got := strings.Join(v.Path, ","); got != "a,b,c" {
		t.Errorf("path: want a,b,c got %q", got)
	}
}

func TestCheckNoReachNotReachable(t *testing.T) {
	models := []domain.PackageModel{
		pkg("a", "x", "b"),
		pkg("b", "x", "c"),
		pkg("c", "x"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"c !~> a"}, // no path c -> a
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("want 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckSelfGlobReachability(t *testing.T) {
	// two plugins, one imports the other.
	models := []domain.PackageModel{
		pkg("internal/plugins/p1", "adapters", "internal/plugins/p2"),
		pkg("internal/plugins/p2", "adapters"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"internal/plugins/* !~> internal/plugins/*"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].From != "internal/plugins/p1" || vs[0].To != "internal/plugins/p2" {
		t.Errorf("violation: %+v", vs[0])
	}
}

func TestCheckChokepointBypass(t *testing.T) {
	// a -> b directly AND a -> m -> b. The direct edge bypasses m.
	models := []domain.PackageModel{
		pkg("a", "x", "b", "m"),
		pkg("m", "x", "b"),
		pkg("b", "x"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"a ~> b via m"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want 1 chokepoint violation, got %d: %+v", len(vs), vs)
	}
	if vs[0].Kind != kindChokepoint || vs[0].From != "a" || vs[0].To != "b" {
		t.Errorf("violation: %+v", vs[0])
	}
}

func TestCheckChokepointHonored(t *testing.T) {
	// a -> m -> b only: every path passes through m, so no violation.
	models := []domain.PackageModel{
		pkg("a", "x", "m"),
		pkg("m", "x", "b"),
		pkg("b", "x"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		DenyByDefault: boolPtr(false),
		Reachability:  []string{"a ~> b via m"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("want 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckKindsQualifierIgnoredIter1(t *testing.T) {
	// The (type) qualifier must not change iteration-1 behavior: the edge is
	// allowed regardless of kind.
	models := []domain.PackageModel{
		pkg("d", "domain"),
		pkg("a1", "app", "d"),
	}
	spec := mustParse(t, overlay.PolicyConfig{
		Allow: []string{"@app -> @domain (type)"},
	})
	vs, err := Check(spec, models, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("want 0 violations, got %d: %+v", len(vs), vs)
	}
}

func TestCheckExternalDepsIgnored(t *testing.T) {
	m := domain.PackageModel{Path: full("a"), Layer: "app"}
	m.Dependencies = append(m.Dependencies, domain.Dependency{
		From: domain.SymbolRef{Package: full("a"), Symbol: "S"},
		To:   domain.SymbolRef{Package: "context", Symbol: "Context", External: true},
		Kind: domain.DependencyUses,
	})
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"@app -> @domain"}})
	vs, err := Check(spec, []domain.PackageModel{m}, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("external deps must be ignored, got %d: %+v", len(vs), vs)
	}
}

func TestCheckOutOfModuleDepIgnored(t *testing.T) {
	// The golang reader does not reliably set External for stdlib/third-party
	// targets, so an out-of-module dependency (here cobra) that arrives with
	// External=false must still be ignored — it is not in this module.
	m := domain.PackageModel{Path: full("cmd/x"), Layer: "adapters"}
	m.Dependencies = append(m.Dependencies, domain.Dependency{
		From: domain.SymbolRef{Package: full("cmd/x"), Symbol: "S"},
		To:   domain.SymbolRef{Package: "github.com/spf13/cobra", Symbol: "Command", External: false},
		Kind: domain.DependencyUses,
	})
	spec := mustParse(t, overlay.PolicyConfig{Allow: []string{"@adapters -> @domain"}})
	vs, err := Check(spec, []domain.PackageModel{m}, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("out-of-module dep must be ignored, got %d: %+v", len(vs), vs)
	}
}

func TestCheckEmptySpec(t *testing.T) {
	vs, err := Check(&Spec{}, []domain.PackageModel{pkg("a", "x", "b")}, cfg())
	if err != nil {
		t.Fatal(err)
	}
	if vs != nil {
		t.Errorf("empty spec: want nil violations, got %+v", vs)
	}
}
