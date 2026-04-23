package overlay

import (
	"bytes"
	"fmt"
	"os"

	yamlv3 "gopkg.in/yaml.v3"
)

// Load reads the archai.yaml file at path, parses it into a Config,
// and returns it. Errors are wrapped with the source path so callers
// can report them clearly.
//
// Load does not validate semantic consistency (e.g. module matches
// go.mod, layer globs are well-formed); use Validate for that.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("overlay: reading %s: %w", path, err)
	}

	var cfg Config
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("overlay: parsing %s: %w", path, err)
	}
	return &cfg, nil
}
