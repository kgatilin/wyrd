package git

import (
	"reflect"
	"testing"
)

func TestHunksReadsNewSideRanges(t *testing.T) {
	patch := `diff --git a/internal/core/core.go b/internal/core/core.go
index 1234567..89abcde 100644
--- a/internal/core/core.go
+++ b/internal/core/core.go
@@ -10,3 +10,5 @@ func Existing() {
 	keep()
+	added()
+	alsoAdded()
 }
@@ -40 +42 @@
-	old()
+	new()
`
	want := []Hunk{{Start: 10, End: 14}, {Start: 42, End: 42}}
	if got := Hunks(patch); !reflect.DeepEqual(got, want) {
		t.Errorf("Hunks() = %+v, want %+v", got, want)
	}
}

// A hunk that only removes lines contributes nothing to the new side, so git
// reports a zero count. Anchoring it on the line the removal left behind keeps
// the range able to overlap a symbol's span.
func TestHunksAnchorsPureDeletions(t *testing.T) {
	patch := "@@ -10,4 +9,0 @@\n-gone()\n"
	want := []Hunk{{Start: 9, End: 9}}
	if got := Hunks(patch); !reflect.DeepEqual(got, want) {
		t.Errorf("Hunks() = %+v, want %+v", got, want)
	}
}

// A patch without headers — binary, mode-only, truncated — costs line
// precision, not an error: the file list already said the file changed.
func TestHunksToleratesPatchesWithoutHeaders(t *testing.T) {
	for _, patch := range []string{
		"",
		"Binary files a/logo.png and b/logo.png differ\n",
		"diff --git a/x b/x\nold mode 100644\nnew mode 100755\n",
		"@@ malformed @@\n",
	} {
		if got := Hunks(patch); got != nil {
			t.Errorf("Hunks(%q) = %+v, want nil", patch, got)
		}
	}
}

// A patch body that quotes a hunk header inside a content line is content, not
// a header: only a line that starts with @@ counts.
func TestHunksIgnoresQuotedHeadersInContent(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n+// the header reads @@ -9,9 +9,9 @@ in the docs\n context\n"
	want := []Hunk{{Start: 1, End: 3}}
	if got := Hunks(patch); !reflect.DeepEqual(got, want) {
		t.Errorf("Hunks() = %+v, want %+v", got, want)
	}
}
