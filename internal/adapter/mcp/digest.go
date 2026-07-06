package mcp

import "github.com/kgatilin/archai/internal/domain"

// Package-digest sizing. A full domain.PackageModel dump embeds every
// symbol's call edges, the package-level dependency list, and source spans —
// bulk that pushed a single get_package past 1 MB on large packages. The
// digest keeps only the symbol *surface* (signatures, synopses, locations);
// bodies come from get_node/read_file and the dependency graph from
// expand/search_graph.
const (
	// digestDefaultLimit caps how many symbols one get_package page returns.
	// Chosen so a typical package returns its whole surface in one call
	// while a god-package paginates instead of blowing the budget.
	digestDefaultLimit = 400

	// digestDocBytes clips each symbol's synopsis (first doc line).
	digestDocBytes = 200

	// digestMaxMembers caps the method/field list rendered per symbol.
	digestMaxMembers = 50
)

// packageDigest is the LLM-facing summary get_package returns in place of a
// full PackageModel. Counts always describe the whole package; Symbols is the
// requested page (empty in index mode). Bodies and edges are reachable
// per-symbol via get_node.
type packageDigest struct {
	Path            string         `json:"path"`
	Name            string         `json:"name"`
	Layer           string         `json:"layer,omitempty"`
	Aggregate       string         `json:"aggregate,omitempty"`
	Counts          symbolCounts   `json:"counts"`
	SourceFiles     []string       `json:"source_files,omitempty"`
	Symbols         []symbolDigest `json:"symbols,omitempty"`
	Implementations []implDigest   `json:"implementations,omitempty"`
	Pagination      *pageInfo      `json:"pagination,omitempty"`
	Hint            string         `json:"hint,omitempty"`
}

// symbolCounts is the per-kind census of a package, independent of paging.
type symbolCounts struct {
	Interfaces int `json:"interfaces"`
	Structs    int `json:"structs"`
	Functions  int `json:"functions"`
	TypeDefs   int `json:"typedefs"`
	Constants  int `json:"constants"`
	Variables  int `json:"variables"`
	Errors     int `json:"errors"`
	Total      int `json:"total"`
}

// symbolDigest is a compact, body-free summary of one package-level symbol.
type symbolDigest struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // interface|struct|func|type|const|var|error
	Signature  string   `json:"signature,omitempty"`
	Doc        string   `json:"doc,omitempty"` // first line only, clipped
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	Exported   bool     `json:"exported"`
	Stereotype string   `json:"stereotype,omitempty"`
	Methods    []string `json:"methods,omitempty"`     // interface/struct method signatures
	Fields     []string `json:"fields,omitempty"`      // struct fields as "name Type"
	Underlying string   `json:"underlying,omitempty"`  // typedef underlying type
	EnumValues []string `json:"enum_values,omitempty"` // typedef associated constants
	Value      string   `json:"value,omitempty"`       // const literal value
	Message    string   `json:"message,omitempty"`     // sentinel error message
}

// implDigest is a compact "Concrete implements Interface" record.
type implDigest struct {
	Concrete  string `json:"concrete"`
	Interface string `json:"interface"`
	Pointer   bool   `json:"pointer,omitempty"`
}

// pageInfo describes which slice of a package's symbols a digest carries.
type pageInfo struct {
	Offset    int  `json:"offset"`
	Limit     int  `json:"limit"`
	Total     int  `json:"total"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`
	NextOffset int `json:"next_offset,omitempty"`
}

// countSymbols returns the per-kind census for a package.
func countSymbols(m domain.PackageModel) symbolCounts {
	c := symbolCounts{
		Interfaces: len(m.Interfaces),
		Structs:    len(m.Structs),
		Functions:  len(m.Functions),
		TypeDefs:   len(m.TypeDefs),
		Constants:  len(m.Constants),
		Variables:  len(m.Variables),
		Errors:     len(m.Errors),
	}
	c.Total = c.Interfaces + c.Structs + c.Functions + c.TypeDefs + c.Constants + c.Variables + c.Errors
	return c
}

// buildPackageIndex returns a symbol-free digest: package metadata and the
// per-kind census, but no symbol list. This is what extract emits per package
// when no explicit paths are requested — an index to drill into, not a dump.
func buildPackageIndex(m domain.PackageModel) packageDigest {
	return packageDigest{
		Path:      m.Path,
		Name:      m.Name,
		Layer:     m.Layer,
		Aggregate: m.Aggregate,
		Counts:    countSymbols(m),
	}
}

// buildPackageDigest projects a PackageModel into a bounded digest. kinds
// optionally filters which symbol kinds are included (empty = all). offset/
// limit page the flattened symbol list (limit<=0 uses digestDefaultLimit).
func buildPackageDigest(m domain.PackageModel, kinds []string, offset, limit int) packageDigest {
	if limit <= 0 {
		limit = digestDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	want := kindFilter(kinds)

	all := flattenSymbols(m, want)
	total := len(all)

	// Page from offset, bounded by BOTH the symbol-count limit and the soft
	// byte budget — a page of 400 method-heavy structs and one of 400 bare
	// consts should not weigh the same. Always emit at least one symbol so a
	// single oversized symbol can't stall pagination.
	page := []symbolDigest{}
	bytes := 0
	i := offset
	for ; i < len(all) && len(page) < limit; i++ {
		sz := estimateSymbolBytes(all[i])
		if len(page) > 0 && bytes+sz > softResultBytes {
			break
		}
		page = append(page, all[i])
		bytes += sz
	}
	truncated := i < len(all) // symbols remain past this page

	dg := packageDigest{
		Path:        m.Path,
		Name:        m.Name,
		Layer:       m.Layer,
		Aggregate:   m.Aggregate,
		Counts:      countSymbols(m),
		SourceFiles: m.SourceFiles(),
		Symbols:     page,
		Pagination: &pageInfo{
			Offset:    offset,
			Limit:     limit,
			Total:     total,
			Returned:  len(page),
			Truncated: truncated,
		},
	}
	if truncated {
		dg.Pagination.NextOffset = offset + len(page)
		dg.Hint = "More symbols than one page: re-call with `offset` = next_offset, narrow with `kinds`, or read a specific symbol with get_node."
	}
	// Implementations are compact and architecturally load-bearing; include
	// them only on the first page so paged calls stay lean.
	if offset == 0 && len(m.Implementations) > 0 {
		for _, impl := range m.Implementations {
			dg.Implementations = append(dg.Implementations, implDigest{
				Concrete:  symbolRefName(impl.Concrete),
				Interface: symbolRefName(impl.Interface),
				Pointer:   impl.IsPointer,
			})
		}
	}
	return dg
}

// kindFilter turns a kinds argument into a lookup set; nil means "all kinds".
func kindFilter(kinds []string) map[string]bool {
	if len(kinds) == 0 {
		return nil
	}
	set := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		set[k] = true
	}
	return set
}

// flattenSymbols concatenates a package's symbols into a single ordered slice
// of digests (interfaces, structs, functions, typedefs, constants, variables,
// errors), honoring the kind filter. The stable order makes offset paging
// deterministic.
func flattenSymbols(m domain.PackageModel, want map[string]bool) []symbolDigest {
	out := make([]symbolDigest, 0, countSymbols(m).Total)

	if want == nil || want["interface"] {
		for _, s := range m.Interfaces {
			out = append(out, symbolDigest{
				Name: s.Name, Kind: "interface", Exported: s.IsExported,
				Doc: firstLine(s.Doc, digestDocBytes), File: s.SourceFile,
				Line: s.Span.StartLine, Stereotype: s.Stereotype.String(),
				Methods: methodSignatures(s.Methods),
			})
		}
	}
	if want == nil || want["struct"] {
		for _, s := range m.Structs {
			out = append(out, symbolDigest{
				Name: s.Name, Kind: "struct", Exported: s.IsExported,
				Doc: firstLine(s.Doc, digestDocBytes), File: s.SourceFile,
				Line: s.Span.StartLine, Stereotype: s.Stereotype.String(),
				Fields: fieldSignatures(s.Fields), Methods: methodSignatures(s.Methods),
			})
		}
	}
	if want == nil || want["func"] {
		for _, f := range m.Functions {
			out = append(out, symbolDigest{
				Name: f.Name, Kind: "func", Exported: f.IsExported,
				Signature: f.Signature(), Doc: firstLine(f.Doc, digestDocBytes),
				File: f.SourceFile, Line: f.Span.StartLine, Stereotype: f.Stereotype.String(),
			})
		}
	}
	if want == nil || want["type"] {
		for _, t := range m.TypeDefs {
			out = append(out, symbolDigest{
				Name: t.Name, Kind: "type", Exported: t.IsExported,
				Underlying: t.UnderlyingType.String(), Doc: firstLine(t.Doc, digestDocBytes),
				File: t.SourceFile, Line: t.Span.StartLine, Stereotype: t.Stereotype.String(),
				EnumValues: capMembers(t.Constants, digestMaxMembers),
			})
		}
	}
	if want == nil || want["const"] {
		for _, c := range m.Constants {
			out = append(out, symbolDigest{
				Name: c.Name, Kind: "const", Exported: c.IsExported,
				Signature: c.Type.String(), Value: c.Value,
				Doc: firstLine(c.Doc, digestDocBytes), File: c.SourceFile, Line: c.Span.StartLine,
			})
		}
	}
	if want == nil || want["var"] {
		for _, v := range m.Variables {
			out = append(out, symbolDigest{
				Name: v.Name, Kind: "var", Exported: v.IsExported,
				Signature: v.Type.String(), Doc: firstLine(v.Doc, digestDocBytes),
				File: v.SourceFile, Line: v.Span.StartLine,
			})
		}
	}
	if want == nil || want["error"] {
		for _, e := range m.Errors {
			out = append(out, symbolDigest{
				Name: e.Name, Kind: "error", Exported: e.IsExported,
				Message: e.Message, Doc: firstLine(e.Doc, digestDocBytes),
				File: e.SourceFile, Line: e.Span.StartLine,
			})
		}
	}
	return out
}

// methodSignatures renders a method set as capped signature strings.
func methodSignatures(methods []domain.MethodDef) []string {
	if len(methods) == 0 {
		return nil
	}
	sigs := make([]string, 0, len(methods))
	for _, m := range methods {
		sigs = append(sigs, m.Signature())
	}
	return capMembers(sigs, digestMaxMembers)
}

// fieldSignatures renders struct fields as capped "name Type" strings.
func fieldSignatures(fields []domain.FieldDef) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.Name+" "+f.Type.String())
	}
	return capMembers(out, digestMaxMembers)
}

// estimateSymbolBytes approximates a symbolDigest's serialized JSON size
// (field text plus a fixed per-field/per-element overhead). It only needs to
// be monotonic and roughly proportional for byte-aware paging — the hard
// clamp in Dispatch is the real ceiling.
func estimateSymbolBytes(s symbolDigest) int {
	// ~90 bytes covers the repeated JSON key names ("name","kind","signature",
	// "doc","file","line","exported","stereotype",...) and punctuation per
	// symbol; tuned so the summed estimate tracks the real compact size and
	// the soft byte budget meaningfully bounds a page.
	n := len(s.Name) + len(s.Kind) + len(s.Signature) + len(s.Doc) +
		len(s.File) + len(s.Underlying) + len(s.Value) + len(s.Message) +
		len(s.Stereotype) + 90
	for _, m := range s.Methods {
		n += len(m) + 8
	}
	for _, f := range s.Fields {
		n += len(f) + 8
	}
	for _, e := range s.EnumValues {
		n += len(e) + 8
	}
	return n
}

// symbolRefName renders a SymbolRef as "package.Symbol" (or just the symbol
// name when the package is empty/local).
func symbolRefName(ref domain.SymbolRef) string {
	if ref.Package == "" {
		return ref.Symbol
	}
	return ref.Package + "." + ref.Symbol
}
