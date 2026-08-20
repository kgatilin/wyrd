package retrieval

// Params holds every tuning knob of the search operation: how the dense and
// lexical channels are calibrated and fused into a relevance mass, how that
// mass is diffused over the graph, and how much of the result is handed back.
// Keeping them in one struct means the ranking pipeline has a single place to
// tune instead of constants scattered across the query path.
type Params struct {
	// DenseWeight is β, the convex weight of the dense channel in fusion:
	// y = β·dense + (1−β)·lexical. 0 is lexical-only, 1 dense-only.
	DenseWeight float64

	// DenseTemp is the softmax temperature for dense cosine scores. Cosines
	// live in a narrow band, so a low temperature is what separates the top
	// hits from the pack.
	DenseTemp float64

	// LexTemp is the softmax temperature for BM25 scores, which have a much
	// wider dynamic range than cosines and want flattening instead.
	LexTemp float64

	// ExactNameBoost is the minimum mass a candidate keeps when its symbol
	// name is literally a token of the query (case-insensitive). It is a
	// floor, not a multiplier: a name typed verbatim stays visible even when
	// the semantic channel prefers its neighbours.
	ExactNameBoost float64

	// DiffusionAlpha is the teleport constant of the graph-diffusion stage
	// (approximate personalized PageRank) seeded with the fused mass.
	DiffusionAlpha float64

	// DiffusionEpsilon is the push tolerance of that diffusion: residual below
	// it stops being propagated.
	DiffusionEpsilon float64

	// HubDamping is τ, the exponent of the hub-suppression term that stops
	// high-degree nodes from absorbing the diffused mass.
	HubDamping float64

	// EdgeKindWeights scales the diffusion weight of each edge kind, so a call
	// carries more relevance than a plain type reference.
	EdgeKindWeights map[string]float64

	// MaxGraphNodes caps how many diffusion-reached nodes a search answer
	// carries. The seeds are not counted against it: they are what the query
	// matched, and a search that drops its own hits to fit a budget is lying.
	MaxGraphNodes int
}

// DefaultParams returns the shipped tuning of the search pipeline.
func DefaultParams() Params {
	return Params{
		DenseWeight:      0.5,
		DenseTemp:        0.05,
		LexTemp:          2.0,
		ExactNameBoost:   0.2,
		DiffusionAlpha:   0.15,
		DiffusionEpsilon: 1e-4,
		HubDamping:       0.25,
		EdgeKindWeights: map[string]float64{
			"calls":      1.0,
			"implements": 0.8,
			"uses":       0.6,
			"returns":    0.6,
		},
		MaxGraphNodes: 50,
	}
}
