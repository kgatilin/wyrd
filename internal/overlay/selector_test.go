package overlay

import "testing"

func TestPackageSelectorMatches(t *testing.T) {
	tests := []struct {
		name     string
		selector PackageSelector
		pkg      string
		want     bool
	}{
		{"empty include matches everything", PackageSelector{}, "internal/a", true},
		{"exact", PackageSelector{Include: []string{"internal/a"}}, "internal/a", true},
		{"exact rejects a child", PackageSelector{Include: []string{"internal/a"}}, "internal/a/b", false},
		{"recursive", PackageSelector{Include: []string{"internal/a/..."}}, "internal/a/b/c", true},
		{"recursive matches the root", PackageSelector{Include: []string{"internal/a/..."}}, "internal/a", true},
		{"single segment", PackageSelector{Include: []string{"internal/*"}}, "internal/a", true},
		{"single segment stops at one", PackageSelector{Include: []string{"internal/*"}}, "internal/a/b", false},
		{
			"exclude wins",
			PackageSelector{Include: []string{"internal/..."}, Exclude: []string{"internal/a/..."}},
			"internal/a/b", false,
		},
		{"leading slash is normalised", PackageSelector{Include: []string{"/internal/a"}}, "internal/a", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.selector.Matches(tc.pkg); got != tc.want {
				t.Errorf("Matches(%q) = %v, want %v", tc.pkg, got, tc.want)
			}
		})
	}
}

// Groups are evaluated in lexical key order and the first match owns the
// package, which is what makes a group assignment deterministic across the
// review UI and the review report.
func TestReviewGroupOfTakesTheFirstMatchInKeyOrder(t *testing.T) {
	cfg := &Config{ReviewGroups: map[string]ReviewGroup{
		"beta":  {Packages: PackageSelector{Include: []string{"internal/..."}}},
		"alpha": {Packages: PackageSelector{Include: []string{"internal/a/..."}}},
	}}

	name, child, ok := cfg.ReviewGroupOf("internal/a/x")
	if !ok || name != "alpha" || child != "" {
		t.Errorf("ReviewGroupOf = (%q, %q, %v), want alpha", name, child, ok)
	}
	name, _, ok = cfg.ReviewGroupOf("internal/b/x")
	if !ok || name != "beta" {
		t.Errorf("ReviewGroupOf = (%q, _, %v), want beta", name, ok)
	}
	if _, _, ok := cfg.ReviewGroupOf("cmd/app"); ok {
		t.Error("ReviewGroupOf matched a package no group covers")
	}
	if _, _, ok := (*Config)(nil).ReviewGroupOf("internal/a"); ok {
		t.Error("a nil config owns no package")
	}
}

// per_directory splits one group into a sub-group per direct child directory;
// a package sitting at the prefix itself stays in the base group.
func TestReviewGroupOfSplitsPerDirectory(t *testing.T) {
	cfg := &Config{ReviewGroups: map[string]ReviewGroup{
		"plugins": {
			Packages:     PackageSelector{Include: []string{"internal/plugins/..."}},
			PerDirectory: "internal/plugins",
		},
	}}

	if _, child, _ := cfg.ReviewGroupOf("internal/plugins/events/api"); child != "events" {
		t.Errorf("child = %q, want the direct child directory", child)
	}
	if _, child, _ := cfg.ReviewGroupOf("internal/plugins"); child != "" {
		t.Errorf("child = %q, want the prefix itself to stay in the base group", child)
	}
}
