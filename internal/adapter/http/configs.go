package http

import (
	"fmt"
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

// configPageData is the page model for /configs. Configs flagged on
// the overlay (or structs carrying the "config" stereotype) are listed
// with a field table and a synthesized default-value example.
type configPageData struct {
	pageData

	HasOverlay bool
	Module     string

	Configs []configView
	Missing []string // overlay entries whose type could not be resolved
}

// configView is one config type surfaced in the catalog.
type configView struct {
	Package    string
	Name       string
	TypeHref   string // /types/{pkg}.{Name}
	Doc        string
	Fields     []configField
	Example    string // synthesized example instance (Go literal)
	FromSource string // "overlay" | "stereotype"
}

// configField is one row in a config type's field table.
type configField struct {
	Name       string
	Type       string
	Tag        string
	IsExported bool
	Default    string // stringified zero/default value
}

// handleConfigs renders /configs. The catalog is best-effort: absent
// overlay just shows the empty-state, missing overlay entries are
// listed under "Unresolved" so the user knows what to fix.
func (s *Server) handleConfigs(w nethttp.ResponseWriter, r *nethttp.Request) {
	state := s.stateFor(r)
	if state == nil {
		nethttp.Error(w, "no state available", nethttp.StatusServiceUnavailable)
		return
	}
	snap := state.Snapshot()
	data := configPageData{
		pageData: s.basePageData(r, "Configs", "/configs"),
	}

	if snap.Overlay != nil {
		data.HasOverlay = true
		data.Module = snap.Overlay.Module
	}

	configs, missing := collectConfigs(snap.Packages, snap.Overlay)
	data.Configs = configs
	data.Missing = missing

	s.renderPage(w, "configs.html", data)
}

// collectConfigs returns every type listed in overlay.Config.Configs
// by its fully-qualified name. The second return is the list of
// unresolved overlay entries — names that do not match any struct in
// the loaded packages. Surfacing them helps operators notice typos.
func collectConfigs(packages []domain.PackageModel, cfg *overlay.Config) ([]configView, []string) {
	module := ""
	var wanted map[string]struct{}
	if cfg != nil {
		module = cfg.Module
		if len(cfg.Configs) > 0 {
			wanted = make(map[string]struct{}, len(cfg.Configs))
			for _, fqn := range cfg.Configs {
				wanted[fqn] = struct{}{}
			}
		}
	}

	seen := make(map[string]bool)
	var out []configView

	addStruct := func(pkgPath string, s domain.StructDef, source string) {
		ref := typeRef{Package: pkgPath, Name: s.Name}
		key := ref.id()
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, configView{
			Package:    ref.Package,
			Name:       ref.Name,
			TypeHref:   "/types/" + key,
			Doc:        s.Doc,
			Fields:     buildConfigFields(s.Fields),
			Example:    synthesizeExample(s),
			FromSource: source,
		})
	}

	// Pass 1: explicit overlay entries.
	resolved := make(map[string]bool)
	if wanted != nil {
		for _, p := range packages {
			for _, s := range p.Structs {
				fqn := buildFQN(module, p.Path, s.Name)
				if _, ok := wanted[fqn]; ok {
					resolved[fqn] = true
					addStruct(p.Path, s, "overlay")
					continue
				}
				// Accept non-module-prefixed entries too (overlay files
				// sometimes use the internal path only).
				altFQN := p.Path + "." + s.Name
				if _, ok := wanted[altFQN]; ok {
					resolved[altFQN] = true
					addStruct(p.Path, s, "overlay")
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Name < out[j].Name
	})

	// Unresolved overlay entries.
	var missing []string
	if wanted != nil {
		for fqn := range wanted {
			if resolved[fqn] {
				continue
			}
			// Allow the non-module-prefixed alias to count as resolved.
			stripped := strings.TrimPrefix(fqn, module+"/")
			if stripped != fqn && resolved[stripped] {
				continue
			}
			missing = append(missing, fqn)
		}
		sort.Strings(missing)
	}

	return out, missing
}

// buildFQN assembles the fully-qualified name used in overlay.Configs.
// An empty module (no overlay or unspecified) falls back to just
// "{pkgPath}.{Name}".
func buildFQN(module, pkgPath, name string) string {
	if module == "" {
		return pkgPath + "." + name
	}
	if pkgPath == "" {
		return module + "." + name
	}
	return module + "/" + pkgPath + "." + name
}

// buildConfigFields renders each field with a default-value literal.
// The default mirrors synthesizeExample but is emitted per-field so
// the table can show each value inline.
func buildConfigFields(fields []domain.FieldDef) []configField {
	out := make([]configField, 0, len(fields))
	for _, f := range fields {
		out = append(out, configField{
			Name:       f.Name,
			Type:       f.Type.String(),
			Tag:        f.Tag,
			IsExported: f.IsExported,
			Default:    defaultLiteral(f.Type),
		})
	}
	return out
}

// synthesizeExample produces a Go-style composite literal for a struct
// using the zero value of every field. The output is formatted so the
// template can drop it straight into a <pre> block without additional
// escaping.
//
// Example output for a struct with a string + int + nested []string:
//
//	ExampleConfig{
//	    Host:   "",
//	    Port:   0,
//	    Allow:  []string{},
//	}
//
// The synthesizer is intentionally dumb: it does not consult struct
// tags for "default" hints (out of scope for M7d) and does not attempt
// to recurse into fields whose type is another named struct from the
// module — that would require a full type graph. Named struct fields
// render as "NamedType{}" which is both valid Go and clearly marked as
// a nested default.
func synthesizeExample(s domain.StructDef) string {
	if len(s.Fields) == 0 {
		return s.Name + "{}"
	}
	// Compute the widest field name so we can align the ":" column.
	maxName := 0
	for _, f := range s.Fields {
		if n := len(f.Name); n > maxName {
			maxName = n
		}
	}

	var b strings.Builder
	b.WriteString(s.Name)
	b.WriteString("{\n")
	for _, f := range s.Fields {
		b.WriteString("    ")
		b.WriteString(f.Name)
		b.WriteString(":")
		// Pad so values line up vertically.
		if pad := maxName - len(f.Name); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(" ")
		b.WriteString(defaultLiteral(f.Type))
		b.WriteString(",\n")
	}
	b.WriteString("}")
	return b.String()
}

// defaultLiteral returns a Go source literal for the zero value of t.
// The result is used by synthesizeExample AND by the per-field
// "default" column in the config table so both stay consistent.
//
// Rules:
//   - Pointer, slice, map, interface → "nil" (or "map[K]V{}" for maps
//     so the output is self-documenting).
//   - string → `""`
//   - bool → `false`
//   - numeric → `0`
//   - named types (non-builtin) → `TypeName{}`. We cannot know if the
//     type is a struct, interface, or scalar, but the brace form is
//     valid for structs and arrays; scalars will be corrected by the
//     reader in a follow-up.
func defaultLiteral(t domain.TypeRef) string {
	if t.IsPointer {
		return "nil"
	}
	if t.IsSlice {
		// Build the element type without pointer prefix; the outer
		// literal is "[]Elem{}".
		elem := t
		elem.IsSlice = false
		return "[]" + elem.String() + "{}"
	}
	if t.IsMap {
		key := "any"
		val := "any"
		if t.KeyType != nil {
			key = t.KeyType.String()
		}
		if t.ValueType != nil {
			val = t.ValueType.String()
		}
		return fmt.Sprintf("map[%s]%s{}", key, val)
	}
	if t.Package == "" {
		// Builtin or local type. Handle the common primitives; fall
		// through to "{}" for unknown locals (likely another struct).
		switch t.Name {
		case "string":
			return `""`
		case "bool":
			return "false"
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"byte", "rune",
			"float32", "float64",
			"complex64", "complex128":
			return "0"
		case "error":
			return "nil"
		case "any", "interface{}":
			return "nil"
		}
		return t.Name + "{}"
	}
	// External type: "pkg.Type{}" is the safest default.
	return t.Package + "." + t.Name + "{}"
}
