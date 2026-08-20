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
				expansion = "collapsed"
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
			DefaultExpansion: "collapsed",
			GroupBy:          groupBy,
			ComponentIDs:     top,
			ComponentCount:   len(top),
		},
		{
			ID:               "all",
			Title:            "All",
			DefaultScope:     scopeAllPublicAPI,
			DefaultExpansion: "collapsed",
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

// selectorMatches defers to overlay, which owns PackageSelector and its
// pattern syntax. The review report resolves packages through the same
// matcher, so a group means one thing on the canvas and in the report.
func selectorMatches(selector overlay.PackageSelector, pkg string) bool {
	return selector.Matches(pkg)
}

func normalizePackagePath(p string) string {
	return overlay.NormalizePackagePath(p)
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
