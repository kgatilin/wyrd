// Package web exposes the built review UI assets for the archai CLI.
package web

import (
	"embed"
	"io/fs"
)

// distFS contains the built React review UI. Keep the pattern explicit so a
// local generated archgraph.json is not accidentally embedded into releases.
//
//go:embed dist/index.html dist/assets/* dist/archgraph.sample.json
var distFS embed.FS

// ReviewUI returns an fs.FS rooted at the built review UI dist directory.
func ReviewUI() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
