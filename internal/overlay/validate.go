package overlay

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// Validate checks that cfg is internally consistent and agrees with
// the go.mod at goModPath.
//
// Specifically it enforces:
//   - cfg.Module is non-empty and matches the module directive in go.mod.
//   - Each layer has at least one package glob, and every glob looks
//     syntactically reasonable (non-empty, no whitespace, no absolute
//     paths, at most one trailing "...").
//   - LayerRules references only known layer names (both keys and
//     values).
//   - Every Aggregate has a non-empty, well-formed fully-qualified
//     Root type name (must contain a '.' separating package path from
//     type name).
//   - Every entry in Configs is a well-formed fully-qualified type name.
//
// Errors are joined (errors.Join) so callers can see every problem at
// once rather than discovering them one edit at a time.
func Validate(cfg *Config, goModPath string) error {
	if cfg == nil {
		return errors.New("overlay: nil config")
	}

	var errs []error

	if strings.TrimSpace(cfg.Module) == "" {
		errs = append(errs, errors.New("overlay: module is required"))
	} else if goModPath != "" {
		declared, err := readGoModModule(goModPath)
		if err != nil {
			errs = append(errs, err)
		} else if declared != cfg.Module {
			errs = append(errs, fmt.Errorf(
				"overlay: module mismatch: archai.yaml declares %q but %s declares %q",
				cfg.Module, goModPath, declared))
		}
	}

	if len(cfg.Layers) == 0 {
		errs = append(errs, errors.New("overlay: at least one layer is required"))
	}
	for _, name := range sortedKeys(cfg.Layers) {
		globs := cfg.Layers[name]
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("overlay: layer name must not be empty"))
			continue
		}
		if len(globs) == 0 {
			errs = append(errs, fmt.Errorf("overlay: layer %q has no package globs", name))
			continue
		}
		for _, g := range globs {
			if err := validateGlob(name, g); err != nil {
				errs = append(errs, err)
			}
		}
	}

	for _, layer := range sortedKeys(cfg.LayerRules) {
		allowed := cfg.LayerRules[layer]
		if _, ok := cfg.Layers[layer]; !ok {
			errs = append(errs, fmt.Errorf(
				"overlay: layer_rules references unknown layer %q", layer))
		}
		for _, target := range allowed {
			if strings.TrimSpace(target) == "" {
				errs = append(errs, fmt.Errorf(
					"overlay: layer_rules for %q contains empty target", layer))
				continue
			}
			if _, ok := cfg.Layers[target]; !ok {
				errs = append(errs, fmt.Errorf(
					"overlay: layer_rules for %q allows unknown layer %q",
					layer, target))
			}
		}
	}

	for _, name := range sortedKeys(cfg.Aggregates) {
		agg := cfg.Aggregates[name]
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("overlay: aggregate name must not be empty"))
			continue
		}
		if err := validateTypeRef(
			fmt.Sprintf("aggregate %q root", name), agg.Root); err != nil {
			errs = append(errs, err)
		}
	}

	for i, ref := range cfg.Configs {
		if err := validateTypeRef(
			fmt.Sprintf("configs[%d]", i), ref); err != nil {
			errs = append(errs, err)
		}
	}

	if err := validateServeConfig(cfg.Serve); err != nil {
		errs = append(errs, err)
	}

	// Track which BC each aggregate appears in so we can flag any
	// aggregate that appears in more than one bounded context.
	aggToBC := make(map[string]string)

	for _, name := range sortedKeys(cfg.BoundedContexts) {
		bc := cfg.BoundedContexts[name]
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("overlay: bounded_contexts: name must not be empty"))
			continue
		}
		for _, aggName := range bc.Aggregates {
			if strings.TrimSpace(aggName) == "" {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q has empty aggregate reference", name))
				continue
			}
			if _, ok := cfg.Aggregates[aggName]; !ok {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q references unknown aggregate %q",
					name, aggName))
			}
			if other, dup := aggToBC[aggName]; dup {
				if other != name {
					errs = append(errs, fmt.Errorf(
						"overlay: aggregate %q appears in multiple bounded_contexts (%q and %q); each aggregate may belong to at most one context",
						aggName, other, name))
				}
			} else {
				aggToBC[aggName] = name
			}
		}
		for _, ref := range bc.Upstream {
			if strings.TrimSpace(ref) == "" {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q has empty upstream reference", name))
				continue
			}
			if ref == name {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q references itself as upstream", name))
				continue
			}
			if _, ok := cfg.BoundedContexts[ref]; !ok {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q upstream references unknown context %q",
					name, ref))
			}
		}
		for _, ref := range bc.Downstream {
			if strings.TrimSpace(ref) == "" {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q has empty downstream reference", name))
				continue
			}
			if ref == name {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q references itself as downstream", name))
				continue
			}
			if _, ok := cfg.BoundedContexts[ref]; !ok {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q downstream references unknown context %q",
					name, ref))
			}
		}
		if bc.Relationship != "" {
			if !isAllowedRelationship(bc.Relationship) {
				errs = append(errs, fmt.Errorf(
					"overlay: bounded_contexts %q has unknown relationship %q (allowed: %s)",
					name, bc.Relationship,
					strings.Join(BoundedContextRelationships, ", ")))
			}
		}
	}

	for _, name := range sortedKeys(cfg.Adapters) {
		ad := cfg.Adapters[name]
		if strings.TrimSpace(name) == "" {
			errs = append(errs, errors.New("overlay: adapters: name must not be empty"))
			continue
		}
		if strings.TrimSpace(ad.Direction) == "" {
			errs = append(errs, fmt.Errorf(
				"overlay: adapters %q has empty direction (allowed: %s)",
				name, strings.Join(AdapterDirections, ", ")))
		} else if !isAllowedDirection(ad.Direction) {
			errs = append(errs, fmt.Errorf(
				"overlay: adapters %q has unknown direction %q (allowed: %s)",
				name, ad.Direction, strings.Join(AdapterDirections, ", ")))
		}
		for _, glob := range ad.Packages {
			if err := validateGlob("adapter "+name, glob); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// isAllowedDirection reports whether d is in the closed set of allowed
// adapter direction qualifiers.
func isAllowedDirection(d string) bool {
	for _, allowed := range AdapterDirections {
		if d == allowed {
			return true
		}
	}
	return false
}

// validateServeConfig checks the optional `serve:` block. Empty values
// are valid (they fall through to flag defaults at the daemon). When
// present, HTTPAddr must parse as a "host:port" pair with a numeric
// port in [0, 65535].
func validateServeConfig(cfg ServeConfig) error {
	addr := cfg.HTTPAddr
	if addr == "" {
		return nil
	}
	if addr != strings.TrimSpace(addr) {
		return fmt.Errorf(
			"overlay: serve.http_addr %q has leading/trailing whitespace", addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf(
			"overlay: serve.http_addr %q is not a valid host:port: %w", addr, err)
	}
	if strings.ContainsAny(host, " \t\n") {
		return fmt.Errorf(
			"overlay: serve.http_addr %q host contains whitespace", addr)
	}
	if port == "" {
		return fmt.Errorf(
			"overlay: serve.http_addr %q has empty port", addr)
	}
	n, perr := strconv.Atoi(port)
	if perr != nil {
		return fmt.Errorf(
			"overlay: serve.http_addr %q has non-numeric port %q", addr, port)
	}
	if n < 0 || n > 65535 {
		return fmt.Errorf(
			"overlay: serve.http_addr %q port %d is out of range [0, 65535]", addr, n)
	}
	return nil
}

// isAllowedRelationship reports whether r is in the closed set of
// recognised context-map relationship qualifiers.
func isAllowedRelationship(r string) bool {
	for _, allowed := range BoundedContextRelationships {
		if r == allowed {
			return true
		}
	}
	return false
}

// validateGlob checks a package glob for obvious syntactic problems.
// It deliberately does not check the filesystem — that's a concern
// for downstream consumers that actually load packages.
func validateGlob(layer, glob string) error {
	if strings.TrimSpace(glob) == "" {
		return fmt.Errorf("overlay: layer %q has empty package glob", layer)
	}
	if glob != strings.TrimSpace(glob) {
		return fmt.Errorf(
			"overlay: layer %q glob %q has leading/trailing whitespace",
			layer, glob)
	}
	if strings.ContainsAny(glob, " \t\n") {
		return fmt.Errorf(
			"overlay: layer %q glob %q contains whitespace", layer, glob)
	}
	if strings.HasPrefix(glob, "/") {
		return fmt.Errorf(
			"overlay: layer %q glob %q must be a relative package path",
			layer, glob)
	}
	// Only a single trailing "..." is allowed; interior "..." is invalid.
	if idx := strings.Index(glob, "..."); idx >= 0 && idx != len(glob)-3 {
		return fmt.Errorf(
			"overlay: layer %q glob %q: \"...\" may only appear at the end",
			layer, glob)
	}
	return nil
}

// validateTypeRef checks that s looks like a fully-qualified Go type
// reference (package path + "." + type name). It does not resolve the
// type; that's left to downstream analysis.
func validateTypeRef(context, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("overlay: %s is empty", context)
	}
	if s != strings.TrimSpace(s) {
		return fmt.Errorf(
			"overlay: %s %q has leading/trailing whitespace", context, s)
	}
	dot := strings.LastIndex(s, ".")
	if dot <= 0 || dot == len(s)-1 {
		return fmt.Errorf(
			"overlay: %s %q is not a fully-qualified type reference "+
				"(expected \"pkg/path.TypeName\")", context, s)
	}
	pkg, name := s[:dot], s[dot+1:]
	if strings.ContainsAny(pkg, " \t\n") || strings.ContainsAny(name, " \t\n./") {
		return fmt.Errorf(
			"overlay: %s %q is not a well-formed type reference", context, s)
	}
	return nil
}

// readGoModModule reads the module directive from the go.mod file at
// path. It uses golang.org/x/mod/modfile (already a transitive
// dependency via golang.org/x/tools) for a forgiving, spec-correct
// parse.
func readGoModModule(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("overlay: reading %s: %w", path, err)
	}
	f, err := modfile.Parse(filepath.Base(path), data, nil)
	if err != nil {
		return "", fmt.Errorf("overlay: parsing %s: %w", path, err)
	}
	if f.Module == nil || f.Module.Mod.Path == "" {
		return "", fmt.Errorf("overlay: %s has no module directive", path)
	}
	return f.Module.Mod.Path, nil
}

// sortedKeys returns the keys of m in lexical order so validation
// errors appear deterministically regardless of map iteration order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
