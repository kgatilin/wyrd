package archmotif

import "testing"

// TestTrophicVerdict pins the thresholds at their boundaries. Two surfaces read
// this table — the trophic_layers lens and the review report — so a shifted
// boundary would make them disagree about the same graph.
func TestTrophicVerdict(t *testing.T) {
	cases := []struct {
		f0   float64
		want string
	}{
		{0, "layered"},
		{0.049, "layered"},
		{0.05, "mostly_layered"},
		{0.249, "mostly_layered"},
		{0.25, "partially_layered"},
		{0.449, "partially_layered"},
		{0.45, "tangled"},
		{1, "tangled"},
	}
	for _, tc := range cases {
		if got := TrophicVerdict(tc.f0); got != tc.want {
			t.Errorf("TrophicVerdict(%v) = %q, want %q", tc.f0, got, tc.want)
		}
	}
}
