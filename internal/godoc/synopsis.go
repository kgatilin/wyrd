// Package godoc extracts the synopsis of a Go doc comment: its leading
// paragraph, cut at the first sentence.
//
// It is a leaf package because two layers need the same answer and neither
// owns it. Retrieval bounds the doc it hands out per node; the MCP renderer
// bounds the doc it prints per line. Each had its own fixed-width cut, and a
// fixed-width cut of a wrapped comment lands mid-clause — the reader gets a
// severed subordinate clause where a whole sentence would have been shorter
// and said more.
package godoc

import (
	"strings"
	"unicode/utf8"
)

// Synopsis renders a doc comment as its first sentence: the leading paragraph
// unwrapped onto one line, cut at the sentence end, and clipped to max bytes
// on a word boundary if no sentence end arrives first. A clipped result ends
// in an ellipsis; a complete sentence does not.
func Synopsis(doc string, max int) string {
	doc = FirstParagraph(doc)
	if doc == "" {
		return ""
	}
	if n := FirstSentenceLen(doc); n < len(doc) {
		doc = doc[:n]
	}
	return ClipWords(doc, max)
}

// FirstParagraph joins the doc's leading paragraph into a single line. A doc
// comment's line breaks are wrapping, not structure, so they collapse to
// spaces; the first blank line ends the paragraph.
func FirstParagraph(doc string) string {
	var out []string
	for _, ln := range strings.Split(strings.TrimSpace(doc), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			break
		}
		out = append(out, ln)
	}
	return strings.Join(out, " ")
}

// FirstSentenceLen returns the byte length of the first sentence of s, or
// len(s) when it holds none. A period ends the sentence when a space follows
// it and it is not the dot of a single-letter initial ("R. Pike", "U.S. law")
// — the same rule go/doc applies to package synopses.
func FirstSentenceLen(s string) int {
	var ppp, pp, p byte
	for i := 0; i < len(s); i++ {
		q := s[i]
		if q == ' ' {
			switch p {
			case '.':
				if !isASCIIUpper(pp) || isASCIIUpper(ppp) {
					return i
				}
			case '!', '?':
				return i
			}
		}
		ppp, pp, p = pp, p, q
	}
	return len(s)
}

// ClipWords truncates s to at most max bytes, pulling the cut back to the last
// word boundary and marking it with an ellipsis. Prose meant to be read pays
// a whole word for a mid-token cut and more than that for a byte count.
func ClipWords(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	// Only honour a word boundary that still spends most of the budget; a
	// single long token would otherwise collapse the synopsis to nothing.
	if sp := strings.LastIndexByte(s[:cut], ' '); sp > max/2 {
		cut = sp
	}
	return strings.TrimRight(s[:cut], " \t\n,;:") + " …"
}

// isASCIIUpper reports whether b is an ASCII capital letter.
func isASCIIUpper(b byte) bool { return 'A' <= b && b <= 'Z' }
