// Package types defines shared types for the retrieval system.
// These types are used by both the retrieval service and its adapters
// to avoid import cycles.
package types

// Scored represents a search result with its score.
type Scored struct {
	ID    string
	Score float32
}
