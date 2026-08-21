package domain

// Module represents the root context for a Go module.
// It provides the module path from go.mod and contains all parsed packages.
type Module struct {
	// Path is the module path from go.mod, e.g., "github.com/kgatilin/wyrd".
	Path string

	// Packages is the list of packages parsed from this module.
	Packages []PackageModel
}

// FindPackage returns the package with the given path, or nil if not found.
func (m Module) FindPackage(path string) *PackageModel {
	for i := range m.Packages {
		if m.Packages[i].Path == path {
			return &m.Packages[i]
		}
	}
	return nil
}

// PackageCount returns the number of packages in the module.
func (m Module) PackageCount() int {
	return len(m.Packages)
}
