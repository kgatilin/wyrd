package archmotif

// TrophicVerdict names an incoherence F0 — the figure trophic.Analyze reports
// over a graph built by this adapter. The thresholds live here, next to the
// graph they are read off, so the MCP lens and the review report cannot call
// the same number by two different words.
//
// The vocabulary is the lens's, snake_cased like every other id crossing the
// tool boundary; a surface writing for a reader opens the underscores up.
func TrophicVerdict(f0 float64) string {
	switch {
	case f0 < 0.05:
		return "layered"
	case f0 < 0.25:
		return "mostly_layered"
	case f0 < 0.45:
		return "partially_layered"
	default:
		return "tangled"
	}
}
