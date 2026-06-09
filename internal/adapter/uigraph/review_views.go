package uigraph

import (
	"path"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

const (
	scopeTopLevelPublicAPI      = "top_level_public_api"
	scopeAllPublicAPI           = "all_public_api"
	scopeInternalImplementation = "internal_implementation"
	scopeEverything             = "everything"
)

func defaultReviewScopes() []ReviewScope {
	return []ReviewScope{
		{ID: scopeTopLevelPublicAPI, Title: "Top-level Public API"},
		{ID: scopeAllPublicAPI, Title: "All Public API"},
		{ID: scopeInternalImplementation, Title: "Internal Implementation"},
		{ID: scopeEverything, Title: "Everything"},
	}
}

func buildReviewViews(models []domain.PackageModel, cfg *overlay.Config) []ReviewView {
	if cfg != nil && len(cfg.ReviewViews) > 0 {
		names := make([]string, 0, len(cfg.ReviewViews))
		for name := range cfg.ReviewViews {
			names = append(names, name)
		}
		sort.Strings(names)

		out := make([]ReviewView, 0, len(names))
		for _, id := range names {
			def := cfg.ReviewViews[id]
			title := def.Title
			if title == "" {
				title = titleFromID(id)
			}
			scope := def.DefaultScope
			if scope == "" {
				scope = scopeAllPublicAPI
			}
			expansion := def.DefaultExpansion
			if expansion == "" {
				expansion = "changed"
			}
			groupBy := normalizeReviewGroupBy(def.GroupBy)
			if groupBy == "" && hasConfiguredReviewGroups(cfg) {
				groupBy = groupingConfiguredGroups
			}
			ids := selectComponentIDs(models, def.Packages)
			out = append(out, ReviewView{
				ID:               id,
				Title:            title,
				DefaultScope:     scope,
				DefaultExpansion: expansion,
				GroupBy:          groupBy,
				ComponentIDs:     ids,
				ComponentCount:   len(ids),
			})
		}
		return out
	}

	top := selectComponentIDs(models, overlay.PackageSelector{
		Include: []string{"*"},
		Exclude: []string{
			"internal",
			"internal/...",
			"test",
			"test/...",
			"tests",
			"tests/...",
			"tools",
			"tools/...",
		},
	})
	all := selectComponentIDs(models, overlay.PackageSelector{})

	groupBy := "directory"
	if hasConfiguredReviewGroups(cfg) {
		groupBy = groupingConfiguredGroups
	}

	return []ReviewView{
		{
			ID:               "top_level",
			Title:            "Top-level",
			DefaultScope:     scopeTopLevelPublicAPI,
			DefaultExpansion: "changed",
			GroupBy:          groupBy,
			ComponentIDs:     top,
			ComponentCount:   len(top),
		},
		{
			ID:               "all",
			Title:            "All",
			DefaultScope:     scopeAllPublicAPI,
			DefaultExpansion: "changed",
			GroupBy:          groupBy,
			ComponentIDs:     all,
			ComponentCount:   len(all),
		},
	}
}

func selectComponentIDs(models []domain.PackageModel, selector overlay.PackageSelector) []string {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if selectorMatches(selector, m.Path) {
			ids = append(ids, m.Path)
		}
	}
	sort.Strings(ids)
	return ids
}

func selectorMatches(selector overlay.PackageSelector, pkg string) bool {
	included := len(selector.Include) == 0
	for _, pattern := range selector.Include {
		if matchPackagePattern(pattern, pkg) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range selector.Exclude {
		if matchPackagePattern(pattern, pkg) {
			return false
		}
	}
	return true
}

func matchPackagePattern(pattern, pkg string) bool {
	pattern = normalizePackagePath(pattern)
	pkg = normalizePackagePath(pkg)
	if pattern == "" {
		return pkg == ""
	}
	patSegs := strings.Split(pattern, "/")
	pkgSegs := strings.Split(pkg, "/")
	return matchSegments(patSegs, pkgSegs)
}

func matchSegments(pattern, pkg []string) bool {
	for len(pattern) > 0 {
		head := pattern[0]
		pattern = pattern[1:]
		if head == "..." {
			return len(pattern) == 0
		}
		if len(pkg) == 0 {
			return false
		}
		switch head {
		case "*":
			pkg = pkg[1:]
		default:
			if head != pkg[0] {
				return false
			}
			pkg = pkg[1:]
		}
	}
	return len(pkg) == 0
}

func normalizePackagePath(p string) string {
	p = strings.TrimSpace(filepathSlash(p))
	p = strings.TrimPrefix(p, "./")
	if p == "." {
		return ""
	}
	return strings.Trim(p, "/")
}

func filepathSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func titleFromID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "Review View"
	}
	parts := strings.FieldsFunc(id, func(r rune) bool {
		return r == '_' || r == '-' || r == '/'
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	title := strings.Join(parts, " ")
	if title == "" {
		return path.Base(id)
	}
	return title
}
