package diff

import (
	"reflect"
	"sort"

	"github.com/kgatilin/archai/internal/domain"
)

// Compute produces a structured Diff describing how the current model must
// change to match the target model.
//
//   - OpAdd    — symbol exists in target but not in current (After populated).
//   - OpRemove — symbol exists in current but not in target (Before populated).
//   - OpChange — symbol exists on both sides but differs (Before+After).
//
// Matching is strictly name-based: packages by Path, symbols by Name within
// a package, methods by (receiver, Name). Rename detection is intentionally
// left out — a rename surfaces as OpRemove + OpAdd.
func Compute(current, target []domain.PackageModel) *Diff {
	d := &Diff{}

	curByPath := indexPackages(current)
	tgtByPath := indexPackages(target)

	// Stable ordering: union of package paths, sorted.
	paths := make([]string, 0, len(curByPath)+len(tgtByPath))
	seen := make(map[string]struct{}, len(curByPath)+len(tgtByPath))
	for p := range curByPath {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
	}
	for p := range tgtByPath {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		cur, hasCur := curByPath[p]
		tgt, hasTgt := tgtByPath[p]
		switch {
		case !hasCur && hasTgt:
			d.Changes = append(d.Changes, Change{
				Op:    OpAdd,
				Kind:  KindPackage,
				Path:  p,
				After: packageSummary(tgt),
			})
		case hasCur && !hasTgt:
			d.Changes = append(d.Changes, Change{
				Op:     OpRemove,
				Kind:   KindPackage,
				Path:   p,
				Before: packageSummary(cur),
			})
		default:
			d.Changes = append(d.Changes, diffPackage(cur, tgt)...)
		}
	}

	return d
}

func indexPackages(pkgs []domain.PackageModel) map[string]domain.PackageModel {
	out := make(map[string]domain.PackageModel, len(pkgs))
	for _, p := range pkgs {
		out[p.Path] = p
	}
	return out
}

// diffPackage compares two matched packages and emits Changes for each
// differing child symbol.
func diffPackage(cur, tgt domain.PackageModel) []Change {
	var changes []Change
	changes = append(changes, diffInterfaces(cur.Path, cur.Interfaces, tgt.Interfaces)...)
	changes = append(changes, diffStructs(cur.Path, cur.Structs, tgt.Structs)...)
	changes = append(changes, diffFunctions(cur.Path, cur.Functions, tgt.Functions)...)
	changes = append(changes, diffTypeDefs(cur.Path, cur.TypeDefs, tgt.TypeDefs)...)
	changes = append(changes, diffConsts(cur.Path, cur.Constants, tgt.Constants)...)
	changes = append(changes, diffVars(cur.Path, cur.Variables, tgt.Variables)...)
	changes = append(changes, diffErrors(cur.Path, cur.Errors, tgt.Errors)...)
	changes = append(changes, diffDependencies(cur.Path, cur.Dependencies, tgt.Dependencies)...)
	return changes
}

// --- per-kind comparisons ---

func diffInterfaces(pkg string, cur, tgt []domain.InterfaceDef) []Change {
	curIdx := make(map[string]domain.InterfaceDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.InterfaceDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindInterface, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindInterface, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindInterface, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffStructs(pkg string, cur, tgt []domain.StructDef) []Change {
	curIdx := make(map[string]domain.StructDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.StructDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindStruct, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindStruct, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindStruct, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffFunctions(pkg string, cur, tgt []domain.FunctionDef) []Change {
	curIdx := make(map[string]domain.FunctionDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.FunctionDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindFunction, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindFunction, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindFunction, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffTypeDefs(pkg string, cur, tgt []domain.TypeDef) []Change {
	curIdx := make(map[string]domain.TypeDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.TypeDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindTypeDef, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindTypeDef, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindTypeDef, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffConsts(pkg string, cur, tgt []domain.ConstDef) []Change {
	curIdx := make(map[string]domain.ConstDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.ConstDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindConst, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindConst, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindConst, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffVars(pkg string, cur, tgt []domain.VarDef) []Change {
	curIdx := make(map[string]domain.VarDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.VarDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindVar, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindVar, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindVar, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

func diffErrors(pkg string, cur, tgt []domain.ErrorDef) []Change {
	curIdx := make(map[string]domain.ErrorDef, len(cur))
	for _, v := range cur {
		curIdx[v.Name] = v
	}
	tgtIdx := make(map[string]domain.ErrorDef, len(tgt))
	for _, v := range tgt {
		tgtIdx[v.Name] = v
	}
	names := unionNames(curIdx, tgtIdx)

	var out []Change
	for _, n := range names {
		c, hasC := curIdx[n]
		t, hasT := tgtIdx[n]
		path := pkg + "." + n
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindError, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindError, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindError, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

// diffDependencies compares dependency lists. Since Dependency does not have
// a natural name, we use a string key built from From/To/Kind.
func diffDependencies(pkg string, cur, tgt []domain.Dependency) []Change {
	depKey := func(d domain.Dependency) string {
		return d.From.String() + "->" + d.To.String() + ":" + string(d.Kind)
	}
	curIdx := make(map[string]domain.Dependency, len(cur))
	for _, v := range cur {
		curIdx[depKey(v)] = v
	}
	tgtIdx := make(map[string]domain.Dependency, len(tgt))
	for _, v := range tgt {
		tgtIdx[depKey(v)] = v
	}

	keys := make([]string, 0, len(curIdx)+len(tgtIdx))
	seen := make(map[string]struct{}, len(curIdx)+len(tgtIdx))
	for k := range curIdx {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	for k := range tgtIdx {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var out []Change
	for _, k := range keys {
		c, hasC := curIdx[k]
		t, hasT := tgtIdx[k]
		path := pkg + "#" + k
		switch {
		case !hasC && hasT:
			out = append(out, Change{Op: OpAdd, Kind: KindDep, Path: path, After: t})
		case hasC && !hasT:
			out = append(out, Change{Op: OpRemove, Kind: KindDep, Path: path, Before: c})
		default:
			if !reflect.DeepEqual(c, t) {
				out = append(out, Change{Op: OpChange, Kind: KindDep, Path: path, Before: c, After: t})
			}
		}
	}
	return out
}

// --- helpers ---

// unionNames returns the sorted union of keys from two maps.
func unionNames[V any](a, b map[string]V) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	names := make([]string, 0, len(a)+len(b))
	for k := range a {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			names = append(names, k)
		}
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}

// packageSummary produces a compact, marshal-friendly snapshot of a package
// used for the Before/After payload on add/remove-of-whole-package changes.
func packageSummary(p domain.PackageModel) map[string]any {
	return map[string]any{
		"path":         p.Path,
		"name":         p.Name,
		"interfaces":   symbolNames(p.Interfaces, func(v domain.InterfaceDef) string { return v.Name }),
		"structs":      symbolNames(p.Structs, func(v domain.StructDef) string { return v.Name }),
		"functions":    symbolNames(p.Functions, func(v domain.FunctionDef) string { return v.Name }),
		"typedefs":     symbolNames(p.TypeDefs, func(v domain.TypeDef) string { return v.Name }),
		"constants":    symbolNames(p.Constants, func(v domain.ConstDef) string { return v.Name }),
		"variables":    symbolNames(p.Variables, func(v domain.VarDef) string { return v.Name }),
		"errors":       symbolNames(p.Errors, func(v domain.ErrorDef) string { return v.Name }),
		"dependencies": len(p.Dependencies),
	}
}

func symbolNames[T any](xs []T, name func(T) string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, name(x))
	}
	sort.Strings(out)
	return out
}
