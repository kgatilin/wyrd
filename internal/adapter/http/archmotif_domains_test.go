package http

import (
	"io"
	nethttp "net/http"
	"testing"

	"github.com/kgatilin/archai/internal/clustering"
)

func TestArchMotifDomainsAPIRejectsNonGET(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := ts.Client().Post(ts.URL+"/api/archmotif/domains", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/archmotif/domains: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// The semantic side of the grid is the embeddings, so a daemon without them
// cannot answer. It has to say that: a canvas waiting on a request that never
// resolves looks the same as one that is still thinking.
func TestArchMotifDomainsAPISaysWhenThereIsNoIndex(t *testing.T) {
	ts, _, _ := newAPITestServer(t)

	resp, err := ts.Client().Get(ts.URL + "/api/archmotif/domains?scope=repo")
	if err != nil {
		t.Fatalf("GET /api/archmotif/domains: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != nethttp.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s, want 503 without a retrieval index", resp.StatusCode, body)
	}
	if len(body) == 0 {
		t.Error("no reason given for the 503")
	}
}

// The scopes are the canvas's three buttons; the client builds the same three
// in lensSelectorForScope. Repo scope drops methods and fields on purpose — a
// few hundred nodes instead of a few thousand, which is what keeps the O(n²)
// kNN pass and the two spectral solves answerable at repository scale.
func TestDomainsSelector(t *testing.T) {
	cases := []struct {
		name      string
		query     map[string][]string
		wantScope string
		want      clustering.Selector
	}{
		{
			name:      "diff",
			query:     map[string][]string{"scope": {"diff"}},
			wantScope: "diff",
			want:      clustering.Selector{Diff: true},
		},
		{
			name:      "package",
			query:     map[string][]string{"scope": {"package"}, "package": {"internal/adapter/mcp"}},
			wantScope: "package",
			want:      clustering.Selector{Package: "internal/adapter/mcp"},
		},
		{
			name:      "repo",
			query:     map[string][]string{"scope": {"repo"}},
			wantScope: "repo",
			want:      clustering.Selector{NodeKinds: []string{"type", "fn"}},
		},
		{
			name:      "missing scope is repo",
			query:     map[string][]string{},
			wantScope: "repo",
			want:      clustering.Selector{NodeKinds: []string{"type", "fn"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scope, got := domainsSelector(c.query)
			if scope != c.wantScope {
				t.Errorf("scope = %q, want %q", scope, c.wantScope)
			}
			if got.Package != c.want.Package || got.Diff != c.want.Diff {
				t.Errorf("selector = %+v, want %+v", got, c.want)
			}
			if len(got.NodeKinds) != len(c.want.NodeKinds) {
				t.Fatalf("node kinds = %v, want %v", got.NodeKinds, c.want.NodeKinds)
			}
			for i := range got.NodeKinds {
				if got.NodeKinds[i] != c.want.NodeKinds[i] {
					t.Errorf("node kinds = %v, want %v", got.NodeKinds, c.want.NodeKinds)
				}
			}
		})
	}
}

func TestAtoiOr(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"12", 12},
		{"0", 0},  // the analysis reads 0 as "choose K yourself"
		{"-3", 0}, // as does anything it would reject
		{"nope", 0},
	}
	for _, c := range cases {
		if got := atoiOr(c.raw, 0); got != c.want {
			t.Errorf("atoiOr(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}
