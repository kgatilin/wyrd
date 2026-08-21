package http

import (
	nethttp "net/http"
	"strconv"

	"github.com/kgatilin/wyrd/internal/clustering"
)

// domainsSchema versions the payload the review UI's domains canvas consumes.
const domainsSchema = "wyrd.domains/1"

// domainsJSON is the analysis plus the scope it was run at. The partition
// itself is clustering.Result, embedded verbatim: the browser draws the two
// sides against each other, so it needs the whole thing, and the whole thing is
// a shared node list with an integer label per node per side.
type domainsJSON struct {
	Schema   string `json:"schema"`
	Worktree string `json:"worktree,omitempty"`
	Scope    string `json:"scope"`
	Package  string `json:"package,omitempty"`
	clustering.Result
}

// handleArchMotifDomains serves the latent-domains partition for the active
// worktree, in full.
//
// It exists because the same analysis reached through the MCP tool endpoint
// cannot answer this question. That endpoint clamps every result at 256 KiB —
// a ceiling that protects an agent's context window and the NATS bridge behind
// it — and a partition of a few thousand symbols is larger than that however
// tightly it is encoded. A browser fetching the data for the page it is already
// rendering is neither of those callers, so the partition goes out whole, with
// no MCP envelope around it.
//
// The MCP `latent_domains` tool still answers the same question for an agent,
// as the verdict plus a sample of each side. Both call clustering.LatentDomains;
// only the rendering differs.
func (s *Server) handleArchMotifDomains(w nethttp.ResponseWriter, r *nethttp.Request) {
	if r.Method != nethttp.MethodGet {
		nethttp.Error(w, "method not allowed", nethttp.StatusMethodNotAllowed)
		return
	}
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "state unavailable", nethttp.StatusServiceUnavailable)
		return
	}

	svc := state.Retrieval()
	if svc == nil {
		nethttp.Error(w, "retrieval not initialized — refresh the index first", nethttp.StatusServiceUnavailable)
		return
	}
	// Both sides of the comparison come from one node set, and the semantic
	// side of it is the embeddings. Saying so is better than half a grid.
	vectors := svc.VectorIndexWithLookup()
	if vectors == nil {
		nethttp.Error(w, "vector index not available — the embedder is not configured, or the index needs a refresh", nethttp.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query()
	scope, selector := domainsSelector(query)

	snap := state.Snapshot()
	in := clustering.Input{
		Packages: snap.Packages,
		Overlay:  snap.Overlay,
		Vectors:  vectors,
		Selector: selector,
		K:        atoiOr(query.Get("k"), 0),
		KNN:      atoiOr(query.Get("knn"), 0),
	}
	if selector.Diff {
		base, err := s.reviewBase(r.Context(), s.currentWorktree(r), reviewBaseRefOf(r))
		if err != nil {
			nethttp.Error(w, "loading review base: "+err.Error(), nethttp.StatusInternalServerError)
			return
		}
		in.Base = base.Models
	}

	res, err := clustering.LatentDomains(in)
	if err != nil {
		// Every error this analysis returns is a statement about the question
		// asked — an empty scope, a missing base, too few embedded symbols — so
		// it is the caller's to fix and reads as a bad request.
		nethttp.Error(w, err.Error(), nethttp.StatusBadRequest)
		return
	}

	writeJSON(w, domainsJSON{
		Schema:   domainsSchema,
		Worktree: s.currentWorktree(r),
		Scope:    scope,
		Package:  selector.Package,
		Result:   res,
	})
}

// domainsSelector reads the scope off the query. It mirrors the canvas's three
// choices: the change region, the whole repository (types and functions only,
// which is a few hundred nodes instead of a few thousand), or one package and
// its subpackages.
func domainsSelector(query map[string][]string) (scope string, sel clustering.Selector) {
	scope = first(query["scope"])
	switch scope {
	case "diff":
		return scope, clustering.Selector{Diff: true}
	case "package":
		return scope, clustering.Selector{Package: first(query["package"])}
	default:
		return "repo", clustering.Selector{NodeKinds: []string{"type", "fn"}}
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// atoiOr reads a positive integer query parameter, falling back to def for
// anything the analysis would reject anyway.
func atoiOr(raw string, def int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	return n
}
