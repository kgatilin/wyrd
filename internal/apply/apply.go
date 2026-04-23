// Package apply mutates a target architecture model by replaying a structured
// diff (see internal/diff) against it. It is the complement of diff.Compute:
// given a diff produced from (current, target), Apply(diff, current, target)
// returns an updated target that matches current — i.e. applying the diff to
// target makes the drift go away.
//
// Apply is a pure transformation: it does not touch the filesystem. The CLI
// wraps it with YAML read/write to update .arch/targets/<id>/ on disk.
package apply

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/diff"
	"github.com/kgatilin/archai/internal/domain"
)

// Apply replays diff d against targetModels, using currentModels as the
// authoritative source for any symbol payload required when growing the
// target. It returns a new slice of PackageModels; inputs are not mutated.
//
// The diff is interpreted per diff.Compute's semantics — namely it describes
// how the CURRENT code must change to match the target — and Apply reverses
// each change against the target so that, after apply, the target snapshot
// matches the current code.
//
//   - OpAdd    — symbol exists in target but not in current; REMOVE it from
//     the target snapshot.
//   - OpRemove — symbol exists in current but not in target; ADD it to the
//     target snapshot (copied from currentModels).
//   - OpChange — symbol exists on both sides but differs; REPLACE the target
//     version with the current version.
//
// Unknown Op or Kind values are reported as errors. Dependencies are keyed
// by the same "<from>-><to>:<kind>" string diff.Compute emits.
func Apply(d *diff.Diff, currentModels, targetModels []domain.PackageModel) ([]domain.PackageModel, error) {
	if d == nil || len(d.Changes) == 0 {
		return cloneModels(targetModels), nil
	}

	curIdx := indexPackages(currentModels)
	tgtIdx := indexMutableModels(targetModels)

	for _, change := range d.Changes {
		if err := applyChange(change, curIdx, tgtIdx); err != nil {
			return nil, fmt.Errorf("apply %s %s %q: %w", change.Op, change.Kind, change.Path, err)
		}
	}

	// Collect packages in sorted order for deterministic output.
	out := make([]domain.PackageModel, 0, len(tgtIdx))
	keys := make([]string, 0, len(tgtIdx))
	for k := range tgtIdx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, *tgtIdx[k])
	}
	return out, nil
}

// applyChange dispatches a single Change to the right mutator. It looks up
// the package-level target PackageModel (creating one on OpAdd/OpChange when
// needed) and delegates to a kind-specific helper.
func applyChange(c diff.Change, curIdx map[string]domain.PackageModel, tgtIdx map[string]*domain.PackageModel) error {
	switch c.Op {
	case diff.OpAdd, diff.OpRemove, diff.OpChange:
		// ok
	default:
		return fmt.Errorf("unknown op %q", c.Op)
	}

	// Package-level adds/removes are handled specially.
	if c.Kind == diff.KindPackage {
		return applyPackage(c, curIdx, tgtIdx)
	}

	pkgPath, symbolPath, err := splitPath(c.Kind, c.Path)
	if err != nil {
		return err
	}

	// For OpRemove / OpChange we need the current-side source of truth
	// (the symbol's new content) to copy into the target.
	var curPkg *domain.PackageModel
	if c.Op == diff.OpRemove || c.Op == diff.OpChange {
		p, ok := curIdx[pkgPath]
		if !ok {
			return fmt.Errorf("current model has no package %q", pkgPath)
		}
		pp := p // capture
		curPkg = &pp
	}

	// Ensure target package exists for OpRemove/OpChange (we may need to
	// add a symbol into it). For OpAdd we must already have the package,
	// because OpAdd semantically means "remove from target".
	tgtPkg, ok := tgtIdx[pkgPath]
	if !ok {
		if c.Op == diff.OpAdd {
			return fmt.Errorf("target model has no package %q", pkgPath)
		}
		// Create empty target package mirroring the current one's metadata.
		tgtPkg = &domain.PackageModel{Path: pkgPath}
		if curPkg != nil {
			tgtPkg.Name = curPkg.Name
			tgtPkg.Layer = curPkg.Layer
			tgtPkg.Aggregate = curPkg.Aggregate
		}
		tgtIdx[pkgPath] = tgtPkg
	}

	switch c.Kind {
	case diff.KindInterface:
		return applyInterface(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindStruct:
		return applyStruct(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindFunction:
		return applyFunction(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindTypeDef:
		return applyTypeDef(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindConst:
		return applyConst(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindVar:
		return applyVar(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindError:
		return applyError(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindDep:
		return applyDep(c.Op, tgtPkg, curPkg, symbolPath)
	case diff.KindMethod, diff.KindField, diff.KindLayerRule:
		// Methods/fields are embedded inside their owning struct/interface and
		// are surfaced by Compute as a structural change on the parent symbol
		// today. Layer-rule diffs are reported but not applyable to a frozen
		// target — skip with an informative error rather than silently no-op.
		return fmt.Errorf("kind %q is not yet supported by apply", c.Kind)
	default:
		return fmt.Errorf("unknown kind %q", c.Kind)
	}
}

// applyPackage handles OpAdd / OpRemove / OpChange on an entire package.
// OpAdd (target has it, current doesn't) => drop from target.
// OpRemove (current has it, target doesn't) => copy current package into target.
func applyPackage(c diff.Change, curIdx map[string]domain.PackageModel, tgtIdx map[string]*domain.PackageModel) error {
	switch c.Op {
	case diff.OpRemove:
		src, ok := curIdx[c.Path]
		if !ok {
			return fmt.Errorf("current model has no package %q", c.Path)
		}
		cp := src
		tgtIdx[c.Path] = &cp
		return nil
	case diff.OpAdd:
		if _, ok := tgtIdx[c.Path]; !ok {
			return fmt.Errorf("target model has no package %q", c.Path)
		}
		delete(tgtIdx, c.Path)
		return nil
	case diff.OpChange:
		src, ok := curIdx[c.Path]
		if !ok {
			return fmt.Errorf("current model has no package %q", c.Path)
		}
		cp := src
		tgtIdx[c.Path] = &cp
		return nil
	}
	return fmt.Errorf("unknown op %q", c.Op)
}

// --- per-kind helpers ---

func applyInterface(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		// target lacks symbol; add from current.
		src := findInterface(cur, name)
		if src == nil {
			return fmt.Errorf("current missing interface %q", name)
		}
		tgt.Interfaces = append(tgt.Interfaces, *src)
	case diff.OpAdd:
		// target has symbol current lacks; drop it from target.
		idx := indexInterface(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing interface %q", name)
		}
		tgt.Interfaces = append(tgt.Interfaces[:idx], tgt.Interfaces[idx+1:]...)
	case diff.OpChange:
		src := findInterface(cur, name)
		if src == nil {
			return fmt.Errorf("current missing interface %q", name)
		}
		idx := indexInterface(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing interface %q", name)
		}
		tgt.Interfaces[idx] = *src
	}
	return nil
}

func applyStruct(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		src := findStruct(cur, name)
		if src == nil {
			return fmt.Errorf("current missing struct %q", name)
		}
		tgt.Structs = append(tgt.Structs, *src)
	case diff.OpAdd:
		idx := indexStruct(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing struct %q", name)
		}
		tgt.Structs = append(tgt.Structs[:idx], tgt.Structs[idx+1:]...)
	case diff.OpChange:
		src := findStruct(cur, name)
		if src == nil {
			return fmt.Errorf("current missing struct %q", name)
		}
		idx := indexStruct(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing struct %q", name)
		}
		tgt.Structs[idx] = *src
	}
	return nil
}

func applyFunction(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		src := findFunction(cur, name)
		if src == nil {
			return fmt.Errorf("current missing function %q", name)
		}
		tgt.Functions = append(tgt.Functions, *src)
	case diff.OpAdd:
		idx := indexFunction(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing function %q", name)
		}
		tgt.Functions = append(tgt.Functions[:idx], tgt.Functions[idx+1:]...)
	case diff.OpChange:
		src := findFunction(cur, name)
		if src == nil {
			return fmt.Errorf("current missing function %q", name)
		}
		idx := indexFunction(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing function %q", name)
		}
		tgt.Functions[idx] = *src
	}
	return nil
}

func applyTypeDef(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		src := findTypeDef(cur, name)
		if src == nil {
			return fmt.Errorf("current missing typedef %q", name)
		}
		tgt.TypeDefs = append(tgt.TypeDefs, *src)
	case diff.OpAdd:
		idx := indexTypeDef(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing typedef %q", name)
		}
		tgt.TypeDefs = append(tgt.TypeDefs[:idx], tgt.TypeDefs[idx+1:]...)
	case diff.OpChange:
		src := findTypeDef(cur, name)
		if src == nil {
			return fmt.Errorf("current missing typedef %q", name)
		}
		idx := indexTypeDef(tgt, name)
		if idx < 0 {
			return fmt.Errorf("target missing typedef %q", name)
		}
		tgt.TypeDefs[idx] = *src
	}
	return nil
}

func applyConst(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		var src *domain.ConstDef
		for i := range cur.Constants {
			if cur.Constants[i].Name == name {
				src = &cur.Constants[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing const %q", name)
		}
		tgt.Constants = append(tgt.Constants, *src)
	case diff.OpAdd:
		idx := -1
		for i := range tgt.Constants {
			if tgt.Constants[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing const %q", name)
		}
		tgt.Constants = append(tgt.Constants[:idx], tgt.Constants[idx+1:]...)
	case diff.OpChange:
		var src *domain.ConstDef
		for i := range cur.Constants {
			if cur.Constants[i].Name == name {
				src = &cur.Constants[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing const %q", name)
		}
		idx := -1
		for i := range tgt.Constants {
			if tgt.Constants[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing const %q", name)
		}
		tgt.Constants[idx] = *src
	}
	return nil
}

func applyVar(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		var src *domain.VarDef
		for i := range cur.Variables {
			if cur.Variables[i].Name == name {
				src = &cur.Variables[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing var %q", name)
		}
		tgt.Variables = append(tgt.Variables, *src)
	case diff.OpAdd:
		idx := -1
		for i := range tgt.Variables {
			if tgt.Variables[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing var %q", name)
		}
		tgt.Variables = append(tgt.Variables[:idx], tgt.Variables[idx+1:]...)
	case diff.OpChange:
		var src *domain.VarDef
		for i := range cur.Variables {
			if cur.Variables[i].Name == name {
				src = &cur.Variables[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing var %q", name)
		}
		idx := -1
		for i := range tgt.Variables {
			if tgt.Variables[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing var %q", name)
		}
		tgt.Variables[idx] = *src
	}
	return nil
}

func applyError(op diff.Op, tgt, cur *domain.PackageModel, name string) error {
	switch op {
	case diff.OpRemove:
		var src *domain.ErrorDef
		for i := range cur.Errors {
			if cur.Errors[i].Name == name {
				src = &cur.Errors[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing error %q", name)
		}
		tgt.Errors = append(tgt.Errors, *src)
	case diff.OpAdd:
		idx := -1
		for i := range tgt.Errors {
			if tgt.Errors[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing error %q", name)
		}
		tgt.Errors = append(tgt.Errors[:idx], tgt.Errors[idx+1:]...)
	case diff.OpChange:
		var src *domain.ErrorDef
		for i := range cur.Errors {
			if cur.Errors[i].Name == name {
				src = &cur.Errors[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing error %q", name)
		}
		idx := -1
		for i := range tgt.Errors {
			if tgt.Errors[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing error %q", name)
		}
		tgt.Errors[idx] = *src
	}
	return nil
}

// applyDep looks up the dependency by the Compute key "<from>-><to>:<kind>".
func applyDep(op diff.Op, tgt, cur *domain.PackageModel, key string) error {
	depKey := func(d domain.Dependency) string {
		return d.From.String() + "->" + d.To.String() + ":" + string(d.Kind)
	}
	switch op {
	case diff.OpRemove:
		var src *domain.Dependency
		for i := range cur.Dependencies {
			if depKey(cur.Dependencies[i]) == key {
				src = &cur.Dependencies[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing dependency %q", key)
		}
		tgt.Dependencies = append(tgt.Dependencies, *src)
	case diff.OpAdd:
		idx := -1
		for i := range tgt.Dependencies {
			if depKey(tgt.Dependencies[i]) == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing dependency %q", key)
		}
		tgt.Dependencies = append(tgt.Dependencies[:idx], tgt.Dependencies[idx+1:]...)
	case diff.OpChange:
		var src *domain.Dependency
		for i := range cur.Dependencies {
			if depKey(cur.Dependencies[i]) == key {
				src = &cur.Dependencies[i]
				break
			}
		}
		if src == nil {
			return fmt.Errorf("current missing dependency %q", key)
		}
		idx := -1
		for i := range tgt.Dependencies {
			if depKey(tgt.Dependencies[i]) == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("target missing dependency %q", key)
		}
		tgt.Dependencies[idx] = *src
	}
	return nil
}

// --- lookup helpers ---

func findInterface(p *domain.PackageModel, name string) *domain.InterfaceDef {
	if p == nil {
		return nil
	}
	for i := range p.Interfaces {
		if p.Interfaces[i].Name == name {
			return &p.Interfaces[i]
		}
	}
	return nil
}

func indexInterface(p *domain.PackageModel, name string) int {
	if p == nil {
		return -1
	}
	for i := range p.Interfaces {
		if p.Interfaces[i].Name == name {
			return i
		}
	}
	return -1
}

func findStruct(p *domain.PackageModel, name string) *domain.StructDef {
	if p == nil {
		return nil
	}
	for i := range p.Structs {
		if p.Structs[i].Name == name {
			return &p.Structs[i]
		}
	}
	return nil
}

func indexStruct(p *domain.PackageModel, name string) int {
	if p == nil {
		return -1
	}
	for i := range p.Structs {
		if p.Structs[i].Name == name {
			return i
		}
	}
	return -1
}

func findFunction(p *domain.PackageModel, name string) *domain.FunctionDef {
	if p == nil {
		return nil
	}
	for i := range p.Functions {
		if p.Functions[i].Name == name {
			return &p.Functions[i]
		}
	}
	return nil
}

func indexFunction(p *domain.PackageModel, name string) int {
	if p == nil {
		return -1
	}
	for i := range p.Functions {
		if p.Functions[i].Name == name {
			return i
		}
	}
	return -1
}

func findTypeDef(p *domain.PackageModel, name string) *domain.TypeDef {
	if p == nil {
		return nil
	}
	for i := range p.TypeDefs {
		if p.TypeDefs[i].Name == name {
			return &p.TypeDefs[i]
		}
	}
	return nil
}

func indexTypeDef(p *domain.PackageModel, name string) int {
	if p == nil {
		return -1
	}
	for i := range p.TypeDefs {
		if p.TypeDefs[i].Name == name {
			return i
		}
	}
	return -1
}

// --- path parsing ---

// splitPath splits a diff.Change.Path into (package path, symbol-or-dep key).
// For dependency changes the separator is '#'; for everything else the last
// '.' after the package path is the separator.
func splitPath(kind diff.Kind, path string) (pkg, sym string, err error) {
	switch kind {
	case diff.KindDep:
		i := strings.Index(path, "#")
		if i < 0 {
			return "", "", fmt.Errorf("malformed dep path %q (missing '#')", path)
		}
		return path[:i], path[i+1:], nil
	default:
		i := strings.LastIndex(path, ".")
		if i < 0 {
			return "", "", fmt.Errorf("malformed path %q (missing '.')", path)
		}
		return path[:i], path[i+1:], nil
	}
}

// --- indexing / cloning ---

func indexPackages(pkgs []domain.PackageModel) map[string]domain.PackageModel {
	out := make(map[string]domain.PackageModel, len(pkgs))
	for _, p := range pkgs {
		out[p.Path] = p
	}
	return out
}

// indexMutableModels returns a map of path -> *PackageModel containing deep-
// copied models so mutations don't leak back into the caller's slice.
func indexMutableModels(pkgs []domain.PackageModel) map[string]*domain.PackageModel {
	out := make(map[string]*domain.PackageModel, len(pkgs))
	for _, p := range pkgs {
		cp := clonePackage(p)
		out[p.Path] = &cp
	}
	return out
}

func cloneModels(pkgs []domain.PackageModel) []domain.PackageModel {
	out := make([]domain.PackageModel, len(pkgs))
	for i, p := range pkgs {
		out[i] = clonePackage(p)
	}
	return out
}

// clonePackage returns a shallow-deep copy of p: slices are reallocated so
// element mutations don't leak, but the elements themselves are copied by
// value (their nested slices are not deep-copied because Apply replaces
// elements whole rather than mutating their interiors).
func clonePackage(p domain.PackageModel) domain.PackageModel {
	cp := p
	cp.Interfaces = append([]domain.InterfaceDef(nil), p.Interfaces...)
	cp.Structs = append([]domain.StructDef(nil), p.Structs...)
	cp.Functions = append([]domain.FunctionDef(nil), p.Functions...)
	cp.TypeDefs = append([]domain.TypeDef(nil), p.TypeDefs...)
	cp.Constants = append([]domain.ConstDef(nil), p.Constants...)
	cp.Variables = append([]domain.VarDef(nil), p.Variables...)
	cp.Errors = append([]domain.ErrorDef(nil), p.Errors...)
	cp.Dependencies = append([]domain.Dependency(nil), p.Dependencies...)
	cp.Implementations = append([]domain.Implementation(nil), p.Implementations...)
	return cp
}
