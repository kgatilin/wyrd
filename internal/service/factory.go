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

// WithJavaReader registers a Java reader. It is invoked only on input
// paths whose subtree contains *.java files, so users with pure-Go
// projects pay no cost when the option is wired in unconditionally.
func WithJavaReader(reader ModelReader) Option {
	return func(s *Service) {
		s.langReaders = append(s.langReaders, languageReader{
			name:   "java",
			reader: reader,
			match:  matchSubtreeHasExt(".java"),
		})
	}
}

// WithLanguageReader is the generic form of WithJavaReader. It registers
// a reader scoped to paths for which match returns true. Pass nil for
// match to run the reader unconditionally.
//
// Order is preserved: readers added earlier run earlier; the Go reader
// passed to NewService always runs first.
func WithLanguageReader(name string, reader ModelReader, match func(path string) bool) Option {
	return func(s *Service) {
		s.langReaders = append(s.langReaders, languageReader{
			name: name, reader: reader, match: match,
		})
	}
}

// WithGoLanguageMatcher attaches a path predicate to the Go reader so it
// only runs on inputs whose subtree actually contains *.go files. By
// default the Go reader runs unconditionally (legacy behaviour preserved
// for `archai diagram generate`); pass matchSubtreeHasExt(".go") here to
// keep pure-Java projects from tripping over the Go loader's
// "directory prefix . does not contain main module" failure.
func WithGoLanguageMatcher(match func(path string) bool) Option {
	return func(s *Service) {
		for i := range s.langReaders {
			if s.langReaders[i].name == "go" {
				s.langReaders[i].match = match
				return
			}
		}
	}
}

// MatchSubtreeHasExt is the exported form of the internal helper used by
// WithJavaReader. Callers wiring a Service from outside this package use
// it to share matchSubtreeHasExt(".go") with WithGoLanguageMatcher
// without re-implementing the heuristic.
func MatchSubtreeHasExt(ext string) func(path string) bool {
	return matchSubtreeHasExt(ext)
}

// NewService creates a new diagram service with the given adapters.
// Parameters:
//   - goReader: reads package models from Go source code (always runs first;
//     wrapped as the leading entry in the language-reader chain)
//   - d2Reader: reads package models from D2 diagram files (for split operation)
//   - d2Writer: writes package models as D2 diagram files
//
// Additional language readers (e.g. Java via WithJavaReader) are layered
// on top through opts; they only run on paths whose subtree contains
// the language's source files.
func NewService(goReader, d2Reader ModelReader, d2Writer ModelWriter, opts ...Option) *Service {
	s := &Service{
		goReader: goReader,
		d2Reader: d2Reader,
		d2Writer: d2Writer,
		langReaders: []languageReader{
			{name: "go", reader: goReader, match: nil},
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
