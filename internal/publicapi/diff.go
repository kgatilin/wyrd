package publicapi

import (
	"sort"
	"strings"
)

const DiffSchema = "archai.public-surface-diff/v0"

type Diff struct {
	Schema  string       `json:"schema"`
	Changes []DiffChange `json:"changes"`
}

type DiffChange struct {
	Op       string `json:"op"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Package  string `json:"package,omitempty"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name,omitempty"`
	Before   string `json:"before,omitempty"`
	After    string `json:"after,omitempty"`
}

func Compare(current, base Surface) Diff {
	diff := Diff{Schema: DiffSchema}
	currentItems := surfaceItems(current)
	baseItems := surfaceItems(base)

	ids := make(map[string]struct{}, len(currentItems)+len(baseItems))
	for id := range currentItems {
		ids[id] = struct{}{}
	}
	for id := range baseItems {
		ids[id] = struct{}{}
	}

	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)

	for _, id := range ordered {
		cur, hasCurrent := currentItems[id]
		old, hasBase := baseItems[id]
		switch {
		case hasCurrent && !hasBase:
			diff.Changes = append(diff.Changes, cur.change("added", "", cur.fingerprint))
		case !hasCurrent && hasBase:
			diff.Changes = append(diff.Changes, old.change("removed", old.fingerprint, ""))
		case hasCurrent && hasBase && cur.fingerprint != old.fingerprint:
			diff.Changes = append(diff.Changes, cur.change("changed", old.fingerprint, cur.fingerprint))
		}
	}

	return diff
}

func (d Diff) IsEmpty() bool {
	return len(d.Changes) == 0
}

type surfaceItem struct {
	id          string
	kind        string
	pkg         string
	parentID    string
	name        string
	fingerprint string
}

func (item surfaceItem) change(op, before, after string) DiffChange {
	return DiffChange{
		Op:       op,
		Kind:     item.kind,
		ID:       item.id,
		Package:  item.pkg,
		ParentID: item.parentID,
		Name:     item.name,
		Before:   before,
		After:    after,
	}
}

func surfaceItems(surface Surface) map[string]surfaceItem {
	items := make(map[string]surfaceItem)
	for _, pkg := range surface.Packages {
		items["pkg:"+pkg.Path] = surfaceItem{
			id:          "pkg:" + pkg.Path,
			kind:        "package",
			pkg:         pkg.Path,
			name:        pkg.Name,
			fingerprint: pkg.Name,
		}
		for _, symbol := range pkg.Symbols {
			items["symbol:"+symbol.ID] = surfaceItem{
				id:          symbol.ID,
				kind:        "symbol:" + symbol.Kind,
				pkg:         pkg.Path,
				name:        symbol.Name,
				fingerprint: symbolFingerprint(symbol),
			}
			for _, member := range symbol.Members {
				items["member:"+member.ID] = surfaceItem{
					id:          member.ID,
					kind:        "member:" + member.Kind,
					pkg:         pkg.Path,
					parentID:    symbol.ID,
					name:        member.Name,
					fingerprint: memberFingerprint(member),
				}
			}
		}
	}
	for _, dep := range surface.PackageDeps {
		items["dep:"+dep.ID] = surfaceItem{
			id:          dep.ID,
			kind:        "package_dependency",
			pkg:         dep.FromPackage,
			name:        dep.FromPackage + " -> " + dep.ToPackage,
			fingerprint: strings.Join(dep.Kinds, "|"),
		}
	}
	return items
}

func symbolFingerprint(symbol Symbol) string {
	return strings.Join([]string{symbol.Kind, symbol.Signature}, "\x00")
}

func memberFingerprint(member Member) string {
	return strings.Join([]string{member.Kind, member.Signature}, "\x00")
}
