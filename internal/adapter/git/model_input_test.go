package git

import "testing"

// TestIsModelInput: a worktree is clean for the model only while every file
// the model is read from matches HEAD — nested go.mod files and the overlay,
// which may list the modules read, included.
func TestIsModelInput(t *testing.T) {
	for p, want := range map[string]bool{
		"main.go":         true,
		"api/store.go":    true,
		"go.mod":          true,
		"api/go.mod":      true,
		"worker/go.sum":   true,
		"wyrd.yaml":       true,
		"archai.yaml":     true,
		"api/wyrd.yaml":   false,
		"README.md":       false,
		"docs/go.mod.txt": false,
	} {
		if got := isModelInput(p); got != want {
			t.Errorf("isModelInput(%q) = %v, want %v", p, got, want)
		}
	}
}
