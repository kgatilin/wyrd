package bm25

import (
	"path/filepath"
	"testing"
)

func TestIndex_UpsertAndSearch(t *testing.T) {
	idx := New()

	idx.Upsert("state", "type State struct { packages map[string]Package }")
	idx.Upsert("handler", "type Handler interface { Handle(ctx context.Context) error }")
	idx.Upsert("newstate", "func NewState(cfg Config) *State")

	if idx.Len() != 3 {
		t.Errorf("expected 3 documents, got %d", idx.Len())
	}

	// Search for "State" - should find documents containing state
	results := idx.Search("State", 10)
	if len(results) < 2 {
		t.Errorf("expected at least 2 results for 'State', got %d", len(results))
	}

	// "state" and "newstate" should be in results
	found := make(map[string]bool)
	for _, r := range results {
		found[r.ID] = true
	}
	if !found["state"] {
		t.Error("expected 'state' document in results")
	}
	if !found["newstate"] {
		t.Error("expected 'newstate' document in results")
	}
}

func TestIndex_SearchRanking(t *testing.T) {
	idx := New()

	// Document with term appearing multiple times should rank higher
	idx.Upsert("multi", "state state state")
	idx.Upsert("single", "state once")
	idx.Upsert("none", "something else")

	results := idx.Search("state", 10)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// "multi" should rank higher than "single" due to term frequency
	if results[0].ID != "multi" {
		t.Errorf("expected 'multi' to rank first, got %q", results[0].ID)
	}
	if results[1].ID != "single" {
		t.Errorf("expected 'single' to rank second, got %q", results[1].ID)
	}
}

func TestIndex_SearchDescendingOrder(t *testing.T) {
	idx := New()

	idx.Upsert("a", "foo bar baz")
	idx.Upsert("b", "foo foo foo")
	idx.Upsert("c", "foo")
	idx.Upsert("d", "bar")

	results := idx.Search("foo", 10)

	// Verify descending order
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not in descending order: %f > %f at positions %d, %d",
				results[i].Score, results[i-1].Score, i-1, i)
		}
	}
}

func TestIndex_IdentifierSearch(t *testing.T) {
	idx := New()

	idx.Upsert("newstate", "func NewState(cfg Config) *State")
	idx.Upsert("newhandler", "func NewHandler() Handler")
	idx.Upsert("httpserver", "type HTTPServer struct")

	// Search for camelCase term
	results := idx.Search("NewState", 10)
	if len(results) == 0 {
		t.Fatal("expected results for 'NewState'")
	}
	if results[0].ID != "newstate" {
		t.Errorf("expected 'newstate' first, got %q", results[0].ID)
	}

	// Search for partial identifier
	results = idx.Search("handler", 10)
	found := false
	for _, r := range results {
		if r.ID == "newhandler" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'newhandler' in results for 'handler' search")
	}

	// Search for acronym
	results = idx.Search("http", 10)
	found = false
	for _, r := range results {
		if r.ID == "httpserver" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'httpserver' in results for 'http' search")
	}
}

func TestIndex_Remove(t *testing.T) {
	idx := New()

	idx.Upsert("a", "hello world")
	idx.Upsert("b", "hello there")

	if idx.Len() != 2 {
		t.Errorf("expected 2 documents, got %d", idx.Len())
	}

	idx.Remove("a")

	if idx.Len() != 1 {
		t.Errorf("expected 1 document after remove, got %d", idx.Len())
	}

	// Search should not find removed document
	results := idx.Search("world", 10)
	for _, r := range results {
		if r.ID == "a" {
			t.Error("removed document should not appear in results")
		}
	}

	// But should still find "b"
	results = idx.Search("hello", 10)
	if len(results) != 1 || results[0].ID != "b" {
		t.Error("expected 'b' in results")
	}
}

func TestIndex_PersistenceRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bm25.json")

	// Create and populate index
	idx1 := New()
	idx1.Upsert("state", "type State struct")
	idx1.Upsert("handler", "type Handler interface")

	if err := idx1.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load into new index
	idx2 := New()
	if err := idx2.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if idx2.Len() != 2 {
		t.Errorf("expected 2 documents after load, got %d", idx2.Len())
	}

	// Search should work
	results := idx2.Search("state", 1)
	if len(results) != 1 || results[0].ID != "state" {
		t.Errorf("search after load failed: got %v", results)
	}
}

func TestIndex_LoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")

	idx := New()
	if err := idx.Load(path); err != nil {
		t.Errorf("Load should not error on missing file: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("expected 0 documents for missing file, got %d", idx.Len())
	}
}

func TestIndex_SearchEmptyIndex(t *testing.T) {
	idx := New()
	results := idx.Search("anything", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty index, got %d", len(results))
	}
}

func TestIndex_SearchEmptyQuery(t *testing.T) {
	idx := New()
	idx.Upsert("a", "hello world")

	results := idx.Search("", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestIndex_UpsertReplacesDocument(t *testing.T) {
	idx := New()

	idx.Upsert("doc", "hello world")
	results := idx.Search("world", 10)
	if len(results) != 1 {
		t.Error("expected 1 result for 'world'")
	}

	// Replace document content
	idx.Upsert("doc", "goodbye universe")

	// Old content should not be searchable
	results = idx.Search("world", 10)
	if len(results) != 0 {
		t.Error("expected 0 results for 'world' after update")
	}

	// New content should be searchable
	results = idx.Search("universe", 10)
	if len(results) != 1 {
		t.Error("expected 1 result for 'universe'")
	}

	// Document count should remain 1
	if idx.Len() != 1 {
		t.Errorf("expected 1 document, got %d", idx.Len())
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{
			"NewState",
			[]string{"new", "state", "newstate"},
		},
		{
			"HTTPServer",
			[]string{"http", "server", "httpserver"},
		},
		{
			"get_user_name",
			[]string{"get", "user", "name"},
		},
		{
			"hello world",
			[]string{"hello", "world"},
		},
		{
			"func NewState(cfg Config) *State",
			[]string{"func", "new", "state", "newstate", "cfg", "config", "state"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := Tokenize(tc.input)

			// Check that all expected tokens are present
			gotSet := make(map[string]bool)
			for _, tok := range got {
				gotSet[tok] = true
			}

			for _, expected := range tc.want {
				if !gotSet[expected] {
					t.Errorf("expected token %q not found in %v", expected, got)
				}
			}
		})
	}
}

func TestSplitIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"NewState", []string{"New", "State", "NewState"}},
		{"HTTPServer", []string{"HTTP", "Server", "HTTPServer"}},
		{"get_user", []string{"get", "user", "get_user"}}, // snake_case also adds original
		{"simple", []string{"simple"}},
		{"", nil},
		{"a", []string{"a"}},
		{"AB", []string{"AB"}},
		{"ABc", []string{"A", "Bc", "ABc"}}, // uppercase followed by lowercase triggers split
		{"aBC", []string{"aBC"}},            // lowercase followed by uppercase at end doesn't split
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := splitIdentifier(tc.input)
			if len(got) != len(tc.want) {
				t.Errorf("splitIdentifier(%q) = %v, want %v", tc.input, got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("splitIdentifier(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestIndex_WithCustomParameters(t *testing.T) {
	idx := New(WithK1(2.0), WithB(0.5))

	idx.Upsert("a", "test document")

	// Just verify it works with custom params
	results := idx.Search("test", 1)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
