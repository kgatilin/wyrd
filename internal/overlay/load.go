package overlay

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

const packageOverlayFileName = "overlay.yaml"

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

// LoadComposed reads the root archai.yaml at path and composes it with
// package-local overlay fragments named .arch/overlay.yaml below the
// same project root. The root file remains the place for module-wide
// settings such as layers and layer rules, while package fragments can
// declare local configs and aggregates without forcing the root file to
// know every type.
//
// Package-local type references may be written as plain type names:
//
//	configs:
//	  - Options
//	aggregates:
//	  daemon_state:
//	    root: State
//
// For a fragment stored at internal/serve/.arch/overlay.yaml, those are
// expanded to github.com/example/app/internal/serve.Options and
// github.com/example/app/internal/serve.State using the module from the
// root config.
func LoadComposed(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("overlay: resolving project root for %s: %w", path, err)
	}

	fragments, err := findPackageOverlayFragments(root)
	if err != nil {
		return nil, err
	}

	var errs []error
	for _, fragmentPath := range fragments {
		fragment, err := Load(fragmentPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pkgPath, err := packagePathForFragment(root, fragmentPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := mergeFragment(cfg, fragment, pkgPath, fragmentPath); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

func findPackageOverlayFragments(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		switch name {
		case ".git", ".worktrees", "bin", "vendor":
			return filepath.SkipDir
		case ".arch":
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			if rel == ".arch" || strings.HasPrefix(rel, ".arch/targets") || strings.HasPrefix(rel, ".arch/.worktree") {
				return filepath.SkipDir
			}
			fragmentPath := filepath.Join(path, packageOverlayFileName)
			if _, statErr := os.Stat(fragmentPath); statErr == nil {
				out = append(out, fragmentPath)
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("overlay: stat %s: %w", fragmentPath, statErr)
			}
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func packagePathForFragment(root, fragmentPath string) (string, error) {
	pkgDir := filepath.Dir(filepath.Dir(fragmentPath))
	rel, err := filepath.Rel(root, pkgDir)
	if err != nil {
		return "", fmt.Errorf("overlay: resolving package path for %s: %w", fragmentPath, err)
	}
	if rel == "." {
		return "", nil
	}
	return filepath.ToSlash(rel), nil
}

func mergeFragment(dst, fragment *Config, pkgPath, source string) error {
	if fragment.Module != "" && dst.Module != "" && fragment.Module != dst.Module {
		return fmt.Errorf("overlay: fragment %s declares module %q, root declares %q", source, fragment.Module, dst.Module)
	}
	if dst.Layers == nil {
		dst.Layers = make(map[string][]string)
	}
	if dst.LayerRules == nil {
		dst.LayerRules = make(map[string][]string)
	}
	mergeStringSlices(dst.Layers, fragment.Layers)
	mergeStringSlices(dst.LayerRules, fragment.LayerRules)

	if dst.Aggregates == nil {
		dst.Aggregates = make(map[string]Aggregate)
	}
	var errs []error
	for name, agg := range fragment.Aggregates {
		if _, exists := dst.Aggregates[name]; exists {
			errs = append(errs, fmt.Errorf("overlay: fragment %s duplicates aggregate %q", source, name))
			continue
		}
		agg.Root = qualifyFragmentTypeRef(dst.Module, pkgPath, agg.Root)
		dst.Aggregates[name] = agg
	}

	seenConfigs := make(map[string]struct{}, len(dst.Configs)+len(fragment.Configs))
	for _, ref := range dst.Configs {
		seenConfigs[ref] = struct{}{}
	}
	for _, ref := range fragment.Configs {
		qualified := qualifyFragmentTypeRef(dst.Module, pkgPath, ref)
		if _, ok := seenConfigs[qualified]; ok {
			continue
		}
		dst.Configs = append(dst.Configs, qualified)
		seenConfigs[qualified] = struct{}{}
	}
	return errors.Join(errs...)
}

func mergeStringSlices(dst, src map[string][]string) {
	if len(src) == 0 {
		return
	}
	for key, values := range src {
		seen := make(map[string]struct{}, len(dst[key])+len(values))
		for _, existing := range dst[key] {
			seen[existing] = struct{}{}
		}
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			dst[key] = append(dst[key], value)
			seen[value] = struct{}{}
		}
	}
}

func qualifyFragmentTypeRef(module, pkgPath, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || module == "" {
		return ref
	}
	if strings.HasPrefix(ref, module+"/") {
		return ref
	}
	if strings.HasPrefix(ref, ".") {
		return modulePackage(module, pkgPath) + ref
	}
	if !strings.Contains(ref, ".") {
		return modulePackage(module, pkgPath) + "." + ref
	}
	dot := strings.LastIndex(ref, ".")
	if dot > 0 && strings.Contains(ref[:dot], "/") {
		firstSegment := strings.SplitN(ref[:dot], "/", 2)[0]
		if strings.Contains(firstSegment, ".") {
			return ref
		}
		return module + "/" + ref
	}
	return ref
}

func modulePackage(module, pkgPath string) string {
	if pkgPath == "" {
		return module
	}
	return module + "/" + pkgPath
}
