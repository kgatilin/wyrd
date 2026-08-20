package git

import (
	"strconv"
	"strings"
)

// Hunk is one changed region of a file, in post-change line numbers: the
// lines a reader of the working tree would see highlighted.
//
// Start and End are 1-based and inclusive. A hunk that only deletes lines
// adds nothing to the new side, so it collapses to the single line it was
// removed from — the region still moved, and a symbol spanning it changed.
type Hunk struct {
	Start int
	End   int
}

// Hunks parses the "@@ -a,b +c,d @@" headers of a unified patch and returns
// the new-side line ranges, in file order. A patch with no headers (a binary
// file, a mode-only change, a truncated patch that lost them) yields nil
// rather than an error: the caller learns the file changed from the diff's
// file list, and a missing hunk only costs line precision.
func Hunks(patch string) []Hunk {
	if patch == "" {
		return nil
	}
	var out []Hunk
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		h, ok := parseHunkHeader(line)
		if !ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

// parseHunkHeader reads the new-side range out of "@@ -a,b +c,d @@ context".
func parseHunkHeader(line string) (Hunk, bool) {
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return Hunk{}, false
	}
	for _, field := range strings.Fields(rest[:end]) {
		if !strings.HasPrefix(field, "+") {
			continue
		}
		start, count, ok := parseRange(field[1:])
		if !ok {
			return Hunk{}, false
		}
		if count == 0 {
			// Pure deletion: git points at the line the removed block
			// followed, so anchor the hunk there instead of producing an
			// empty range that overlaps nothing.
			return Hunk{Start: start, End: start}, true
		}
		return Hunk{Start: start, End: start + count - 1}, true
	}
	return Hunk{}, false
}

// parseRange reads "start" or "start,count"; a missing count means one line.
func parseRange(s string) (start, count int, ok bool) {
	count = 1
	if comma := strings.IndexByte(s, ','); comma >= 0 {
		n, err := strconv.Atoi(s[comma+1:])
		if err != nil || n < 0 {
			return 0, 0, false
		}
		count = n
		s = s[:comma]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, 0, false
	}
	return n, count, true
}
