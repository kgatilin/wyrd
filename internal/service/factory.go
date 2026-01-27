package service

// NewService creates a new diagram service with the given adapters.
// Parameters:
//   - goReader: reads package models from Go source code
//   - d2Reader: reads package models from D2 diagram files (for split operation)
//   - d2Writer: writes package models as D2 diagram files
func NewService(goReader, d2Reader ModelReader, d2Writer ModelWriter) *Service {
	return &Service{
		goReader: goReader,
		d2Reader: d2Reader,
		d2Writer: d2Writer,
	}
}
