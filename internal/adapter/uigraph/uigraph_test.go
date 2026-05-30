package uigraph

import "testing"

func TestParseChangePath(t *testing.T) {
	cases := []struct {
		in         string
		wantPkg    string
		wantType   string
		wantMember string
		wantLevel  string // "package" | "type" | "member"
	}{
		{"internal/service", "internal/service", "", "", "package"},
		{"internal/service.Service", "internal/service", "Service", "", "type"},
		{"internal/service.Service.Handle", "internal/service", "Service", "Handle", "member"},
		{"github.com/x/y/pkg.T.M", "github.com/x/y/pkg", "T", "M", "member"},
	}
	for _, c := range cases {
		got := parseChangePath(c.in)
		if got.Pkg != c.wantPkg || got.Type != c.wantType || got.Member != c.wantMember || got.Level != c.wantLevel {
			t.Errorf("parseChangePath(%q) = %+v, want pkg=%q type=%q member=%q level=%q",
				c.in, got, c.wantPkg, c.wantType, c.wantMember, c.wantLevel)
		}
	}
}
