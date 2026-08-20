package retrieval

import (
	"fmt"
	"math"
	"testing"
)

// massTolerance is the slack allowed when asserting a distribution sums to 1;
// masses are accumulated in float64 but handed back as float32.
const massTolerance = 1e-6

// fusionService builds a Service with the given params and a graph holding the
// named nodes, which is all fuseCalibrated needs (the exact-name floor is the
// only step that looks a candidate up).
func fusionService(p Params, names map[string]string) *Service {
	svc := &Service{params: p}
	if names == nil {
		return svc
	}
	nodes := make(map[string]Node, len(names))
	for id, name := range names {
		nodes[id] = Node{ID: id, Kind: "func", Package: "pkg", Name: name}
	}
	svc.graph = &Graph{NodesByID: nodes}
	return svc
}

func totalMass(fused []Scored) float64 {
	total := 0.0
	for _, f := range fused {
		total += float64(f.Score)
	}
	return total
}

func massByID(fused []Scored) map[string]float64 {
	byID := make(map[string]float64, len(fused))
	for _, f := range fused {
		byID[f.ID] = float64(f.Score)
	}
	return byID
}

func idOrder(fused []Scored) []string {
	ids := make([]string, len(fused))
	for i, f := range fused {
		ids[i] = f.ID
	}
	return ids
}

func TestSoftmaxTemperature(t *testing.T) {
	scored := []Scored{{ID: "A", Score: 2}, {ID: "B", Score: 1}, {ID: "C", Score: 0}}

	sharp := softmax(scored, 0.1)
	flat := softmax(scored, 10)

	for name, mass := range map[string]map[string]float64{"sharp": sharp, "flat": flat} {
		total := 0.0
		for _, m := range mass {
			total += m
		}
		if math.Abs(total-1) > massTolerance {
			t.Errorf("%s mass sums to %v, want 1", name, total)
		}
	}

	// A low temperature concentrates the mass on the leading score.
	if sharp["A"] < 0.99 {
		t.Errorf("sharp A mass = %v, want > 0.99", sharp["A"])
	}
	// A high temperature flattens the same scores toward uniform.
	if spread := flat["A"] - flat["C"]; spread > 0.1 {
		t.Errorf("flat spread A−C = %v, want < 0.1", spread)
	}
	if sharp["A"] <= flat["A"] {
		t.Errorf("temperature did not sharpen: sharp A = %v, flat A = %v", sharp["A"], flat["A"])
	}
}

func TestSoftmaxEmpty(t *testing.T) {
	if mass := softmax(nil, 1); len(mass) != 0 {
		t.Errorf("softmax of no candidates = %v, want empty", mass)
	}
}

func TestFuseCalibratedWeightExtremes(t *testing.T) {
	dense := []Scored{{ID: "A", Score: 0.9}, {ID: "B", Score: 0.8}, {ID: "C", Score: 0.7}}
	lexical := []Scored{{ID: "D", Score: 10}, {ID: "E", Score: 8}, {ID: "C", Score: 5}}

	fuse := func(beta float64) []Scored {
		p := DefaultParams()
		p.DenseWeight = beta
		// No graph: the exact-name floor cannot fire, leaving the convex mix
		// as the only thing under test.
		return fusionService(p, nil).fuseCalibrated("unrelated phrase", dense, lexical)
	}

	t.Run("beta 1 reproduces dense ordering", func(t *testing.T) {
		got := idOrder(fuse(1))[:3]
		want := []string{"A", "B", "C"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want dense order %v", got, want)
			}
		}
	})

	t.Run("beta 0 reproduces lexical ordering", func(t *testing.T) {
		got := idOrder(fuse(0))[:3]
		want := []string{"D", "E", "C"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want lexical order %v", got, want)
			}
		}
	})

	t.Run("both channels sum to one", func(t *testing.T) {
		fused := fuse(0.5)
		if len(fused) != 5 {
			t.Fatalf("fused candidates = %d, want 5 (union of both channels)", len(fused))
		}
		if total := totalMass(fused); math.Abs(total-1) > massTolerance {
			t.Errorf("fused mass sums to %v, want 1", total)
		}
		// C is the only candidate both channels found, so it draws from both.
		mass := massByID(fused)
		if mass["C"] <= 0 {
			t.Errorf("shared candidate C mass = %v, want > 0", mass["C"])
		}
	})
}

func TestFuseCalibratedLexicalOnly(t *testing.T) {
	lexical := []Scored{{ID: "A", Score: 10}, {ID: "B", Score: 8}, {ID: "C", Score: 1}}

	p := DefaultParams()
	fused := fusionService(p, nil).fuseCalibrated("unrelated phrase", nil, lexical)

	if total := totalMass(fused); math.Abs(total-1) > massTolerance {
		t.Errorf("lexical-only mass sums to %v, want 1 (no β scaling)", total)
	}

	// With no dense channel the fused mass is exactly the BM25 softmax.
	want := softmax(lexical, p.LexTemp)
	for id, got := range massByID(fused) {
		if math.Abs(got-want[id]) > massTolerance {
			t.Errorf("%s mass = %v, want pure softmax %v", id, got, want[id])
		}
	}
}

func TestFuseCalibratedExactNameFloor(t *testing.T) {
	// Nine near-tied decoys plus one poor lexical match whose name is exactly
	// what was asked for — the case a plain softmax buries.
	names := map[string]string{"pkg.Target": "Target"}
	lexical := []Scored{{ID: "pkg.Target", Score: 0}}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("pkg.Decoy%d", i)
		names[id] = fmt.Sprintf("Decoy%d", i)
		lexical = append(lexical, Scored{ID: id, Score: 5})
	}

	p := DefaultParams()
	svc := fusionService(p, names)

	unboosted := massByID(svc.fuseCalibrated("no matching token here", nil, lexical))
	// Query casing differs from the symbol's: the match is case-insensitive.
	boosted := svc.fuseCalibrated("target lookup", nil, lexical)

	if total := totalMass(boosted); math.Abs(total-1) > massTolerance {
		t.Errorf("boosted mass sums to %v, want 1 after renormalization", total)
	}
	if boosted[0].ID != "pkg.Target" {
		t.Errorf("top hit = %s, want pkg.Target lifted to the front", boosted[0].ID)
	}

	got := massByID(boosted)["pkg.Target"]
	if got <= unboosted["pkg.Target"] {
		t.Errorf("Target mass = %v, want above its unboosted %v", got, unboosted["pkg.Target"])
	}
	// Renormalizing after the lift divides by at most 1+floor, so that is the
	// floor the boosted candidate actually keeps.
	if min := p.ExactNameBoost / (1 + p.ExactNameBoost); got < min-massTolerance {
		t.Errorf("Target mass = %v, want >= %v", got, min)
	}
}

func TestFuseCalibratedNoNameMatchLeavesMassUntouched(t *testing.T) {
	names := map[string]string{"pkg.Alpha": "Alpha", "pkg.Beta": "Beta"}
	lexical := []Scored{{ID: "pkg.Alpha", Score: 10}, {ID: "pkg.Beta", Score: 1}}

	p := DefaultParams()
	svc := fusionService(p, names)
	fused := massByID(svc.fuseCalibrated("something else entirely", nil, lexical))

	want := softmax(lexical, p.LexTemp)
	for id, got := range fused {
		if math.Abs(got-want[id]) > massTolerance {
			t.Errorf("%s mass = %v, want untouched softmax %v", id, got, want[id])
		}
	}
}
