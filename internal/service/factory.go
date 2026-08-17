package service

// Option configures the Service with optional adapters.
type Option func(*Service)

// WithYAML adds YAML reader/writer support to the service.
func WithYAML(reader ModelReader, writer ModelWriter) Option {
	return func(s *Service) {
		s.yamlReader = reader
		s.yamlWriter = writer
	}
}

// NewService creates a new diagram service with the given adapters.
// Parameters:
//   - goReader: reads package models from Go source code
//   - d2Reader: reads package models from D2 diagram files (for split operation)
//   - d2Writer: writes package models as D2 diagram files
func NewService(goReader, d2Reader ModelReader, d2Writer ModelWriter, opts ...Option) *Service {
	s := &Service{
		goReader: goReader,
		d2Reader: d2Reader,
		d2Writer: d2Writer,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
