package retrieval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A lexical index that cannot be asked is an error, not an empty answer:
// "no such code" and "nothing could be asked" must not look the same.
func TestSearch_LexicalFailureIsAnErrorAndAFault(t *testing.T) {
	g := callGraph([][2]string{{"a", "b"}})
	down := errors.New("connection refused")
	svc := &Service{
		graph:  g,
		corpus: NewCorpus(nil, nil, stubLexical{err: down}, ""),
		view:   identityView(g),
		params: DefaultParams(),
	}

	_, err := svc.Search(context.Background(), "a", SearchOptions{K: 5})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Search err = %v, want the index's failure", err)
	}
	faults := svc.corpus.Faults()
	if !strings.Contains(faults[FaultLexical], "connection refused") {
		t.Errorf("faults = %v, want the lexical channel's failure", faults)
	}
}

// A channel that answers again is no longer at fault.
func TestCorpus_FaultClearsOnTheNextSuccess(t *testing.T) {
	c := NewCorpus(nil, nil, stubLexical{}, "")
	c.report(FaultLexical, errors.New("timeout"))
	c.report(FaultVectors, errors.New("timeout"))
	c.report(FaultLexical, nil)
	faults := c.Faults()
	if _, ok := faults[FaultLexical]; ok || faults[FaultVectors] == "" || len(faults) != 1 {
		t.Errorf("faults = %v, want only the vectors channel", faults)
	}
}

// flakyLexical fails until it is healed.
type flakyLexical struct {
	stubLexical
	down  *bool
	calls *int
}

func (f flakyLexical) Search(ctx context.Context, query string, k int, keep func(string) bool) ([]Scored, error) {
	*f.calls++
	if *f.down {
		return nil, errors.New("connection refused")
	}
	return nil, nil
}

// Recheck is what brings a daemon back once its backend answers again: it
// asks the channels at fault, and only those.
func TestCorpus_RecheckClearsAFaultWhoseBackendAnswers(t *testing.T) {
	down, calls := true, 0
	c := NewCorpus(nil, nil, flakyLexical{down: &down, calls: &calls}, "")

	if got := c.Recheck(context.Background()); len(got) != 0 || calls != 0 {
		t.Fatalf("a healthy corpus was probed: faults=%v calls=%d", got, calls)
	}
	c.report(FaultLexical, errors.New("connection refused"))
	if got := c.Recheck(context.Background()); got[FaultLexical] == "" || calls != 1 {
		t.Fatalf("still down: faults=%v calls=%d", got, calls)
	}
	down = false
	if got := c.Recheck(context.Background()); len(got) != 0 || calls != 2 {
		t.Fatalf("healed: faults=%v calls=%d", got, calls)
	}
}
