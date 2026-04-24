package yaml

import (
	"encoding/json"
	"fmt"

	"github.com/kgatilin/archai/internal/domain"
	yamlv3 "gopkg.in/yaml.v3"
)

// MarshalPackage serializes a single PackageModel to bytes in the
// requested format ("yaml" or "json"). The output matches the on-disk
// YAML schema produced by the ModelWriter (same field names and nesting),
// so callers can round-trip a dumped package back through the YAML
// reader.
//
// JSON output uses the same field names as YAML (snake_case where
// applicable) by routing the spec through a generic map, which keeps
// the two formats in lock-step without duplicate struct tags.
func MarshalPackage(model domain.PackageModel, format string) ([]byte, error) {
	spec := toSpec(model, false)
	yamlBytes, err := yamlv3.Marshal(&spec)
	if err != nil {
		return nil, fmt.Errorf("marshaling %s as yaml: %w", model.Path, err)
	}

	switch format {
	case "", "yaml":
		return yamlBytes, nil
	case "json":
		var generic any
		if err := yamlv3.Unmarshal(yamlBytes, &generic); err != nil {
			return nil, fmt.Errorf("re-parsing %s yaml for json conversion: %w", model.Path, err)
		}
		generic = normalizeForJSON(generic)
		out, err := json.MarshalIndent(generic, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling %s as json: %w", model.Path, err)
		}
		return append(out, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported format %q (use yaml or json)", format)
	}
}

// normalizeForJSON walks the yaml.v3-decoded tree and coerces any
// map[interface{}]interface{} (yaml's default) into map[string]interface{}
// so encoding/json can marshal it. yaml.v3 already returns
// map[string]interface{} in most cases, but we walk defensively to be
// safe across library versions.
func normalizeForJSON(v any) any {
	switch t := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeForJSON(val)
		}
		return out
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeForJSON(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeForJSON(val)
		}
		return t
	default:
		return v
	}
}
