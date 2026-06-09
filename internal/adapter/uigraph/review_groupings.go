package uigraph

import (
	"path"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/domain"
	"github.com/kgatilin/archai/internal/overlay"
)

const (
	groupingReviewView       = "review_view"
	groupingConfiguredGroups = "configured_groups"
	groupingLayer            = "layer"
	groupingDirectory        = "directory"
	groupingPackageOwner     = "package_owner"
)

func buildReviewGroupings(
	models []domain.PackageModel,
	cfg *overlay.Config,
	views []ReviewView,
	contexts []BoundedContext,
) []ReviewGrouping {
	return []ReviewGrouping{
		{
			ID:     groupingReviewView,
			Title:  "Review View",
			Groups: groupsFromReviewViews(views),
		},
		{
			ID:     groupingConfiguredGroups,
			Title:  configuredGroupsTitle(cfg),
			Groups: groupsFromConfiguredCategories(models, cfg, contexts),
		},
		{
			ID:    groupingLayer,
			Title: "Layer",
			Groups: groupsFromModels(models, func(m domain.PackageModel) (string, string) {
				layer := strings.TrimSpace(m.Layer)
				if layer == "" {
					layer = "unlayered"
				}
				return "layer:" + layer, titleFromID(layer)
			}),
		},
		{
			ID:    groupingDirectory,
			Title: "Directory",
			Groups: groupsFromModels(models, func(m domain.PackageModel) (string, string) {
				dir := topPackageSegment(m.Path)
				return "directory:" + dir, titleFromID(dir)
			}),
		},
		{
			ID:     groupingPackageOwner,
			Title:  "Package Owner",
			Groups: groupsFromPackageOwners(models, cfg),
		},
	}
}

func defaultGroupingForView(view ReviewView, groupings []ReviewGrouping) string {
	groupBy := normalizeReviewGroupBy(view.GroupBy)
	if hasGrouping(groupings, groupBy) {
		return groupBy
	}
	if hasGrouping(groupings, groupingDirectory) {
		return groupingDirectory
	}
	if len(groupings) > 0 {
		return groupings[0].ID
	}
	return ""
}

func hasGrouping(groupings []ReviewGrouping, id string) bool {
	if id == "" {
		return false
	}
	for _, grouping := range groupings {
		if grouping.ID == id {
			return true
		}
	}
	return false
}

func normalizeReviewGroupBy(id string) string {
	switch strings.TrimSpace(id) {
	case "categories", "category", "review_groups", "review_group":
		return groupingConfiguredGroups
	default:
		return strings.TrimSpace(id)
	}
}

func hasConfiguredReviewGroups(cfg *overlay.Config) bool {
	return cfg != nil && len(cfg.ReviewGroups) > 0
}

func configuredGroupsTitle(cfg *overlay.Config) string {
	if hasConfiguredReviewGroups(cfg) {
		return "Categories"
	}
	return "Configured Groups"
}

func groupsFromReviewViews(views []ReviewView) []ReviewGroup {
	groups := make([]ReviewGroup, 0, len(views))
	for _, view := range views {
		ids := append([]string(nil), view.ComponentIDs...)
		sort.Strings(ids)
		groups = append(groups, ReviewGroup{
			ID:             "review_view:" + view.ID,
			Title:          view.Title,
			ComponentIDs:   ids,
			ComponentCount: len(ids),
		})
	}
	return groups
}

func groupsFromConfiguredCategories(
	models []domain.PackageModel,
	cfg *overlay.Config,
	contexts []BoundedContext,
) []ReviewGroup {
	if hasConfiguredReviewGroups(cfg) {
		return groupsFromReviewGroups(models, cfg)
	}
	return groupsFromConfiguredContexts(models, cfg, contexts)
}

func groupsFromReviewGroups(models []domain.PackageModel, cfg *overlay.Config) []ReviewGroup {
	type acc struct {
		title string
		ids   []string
	}

	groupIDs := sortedMapKeys(cfg.ReviewGroups)
	byID := make(map[string]acc, len(groupIDs)+1)
	for _, model := range models {
		matchedID := ""
		matchedTitle := ""
		for _, groupID := range groupIDs {
			group := cfg.ReviewGroups[groupID]
			if !selectorMatches(group.Packages, model.Path) {
				continue
			}
			matchedID = "configured_groups:" + groupID
			matchedTitle = strings.TrimSpace(group.Title)
			if matchedTitle == "" {
				matchedTitle = titleFromID(groupID)
			}
			break
		}
		if matchedID == "" {
			matchedID = "configured_groups:uncategorized"
			matchedTitle = "Uncategorized"
		}

		group := byID[matchedID]
		group.title = matchedTitle
		group.ids = append(group.ids, model.Path)
		byID[matchedID] = group
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	groups := make([]ReviewGroup, 0, len(ids))
	for _, id := range ids {
		group := byID[id]
		sort.Strings(group.ids)
		groups = append(groups, ReviewGroup{
			ID:             id,
			Title:          group.title,
			ComponentIDs:   group.ids,
			ComponentCount: len(group.ids),
		})
	}
	return groups
}

func groupsFromConfiguredContexts(
	models []domain.PackageModel,
	cfg *overlay.Config,
	contexts []BoundedContext,
) []ReviewGroup {
	titleByID := make(map[string]string, len(contexts))
	for _, context := range contexts {
		titleByID[context.ID] = context.Name
	}
	return groupsFromModels(models, func(m domain.PackageModel) (string, string) {
		id := resolveBC(m, cfg)
		title := titleByID[id]
		if title == "" {
			title = titleFromID(id)
		}
		return "configured_groups:" + id, title
	})
}

func groupsFromPackageOwners(models []domain.PackageModel, cfg *overlay.Config) []ReviewGroup {
	if cfg == nil || len(cfg.PackageOwners) == 0 {
		return groupsFromModels(models, func(domain.PackageModel) (string, string) {
			return "package_owner:unowned", "Unowned"
		})
	}

	ownerIDs := sortedMapKeys(cfg.PackageOwners)
	return groupsFromModels(models, func(m domain.PackageModel) (string, string) {
		for _, ownerID := range ownerIDs {
			owner := cfg.PackageOwners[ownerID]
			if !selectorMatches(owner.Packages, m.Path) {
				continue
			}
			title := owner.Name
			if title == "" {
				title = titleFromID(ownerID)
			}
			return "package_owner:" + ownerID, title
		}
		return "package_owner:unowned", "Unowned"
	})
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func groupsFromModels(
	models []domain.PackageModel,
	classify func(domain.PackageModel) (id, title string),
) []ReviewGroup {
	type acc struct {
		title string
		ids   []string
	}
	byID := make(map[string]acc)
	for _, model := range models {
		id, title := classify(model)
		id = strings.TrimSpace(id)
		if id == "" {
			id = "all"
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = titleFromID(id)
		}
		group := byID[id]
		group.title = title
		group.ids = append(group.ids, model.Path)
		byID[id] = group
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	groups := make([]ReviewGroup, 0, len(ids))
	for _, id := range ids {
		group := byID[id]
		sort.Strings(group.ids)
		groups = append(groups, ReviewGroup{
			ID:             id,
			Title:          group.title,
			ComponentIDs:   group.ids,
			ComponentCount: len(group.ids),
		})
	}
	return groups
}

func topPackageSegment(pkg string) string {
	pkg = normalizePackagePath(pkg)
	if pkg == "" {
		return "root"
	}
	if slash := strings.IndexByte(pkg, '/'); slash >= 0 {
		return pkg[:slash]
	}
	dir := path.Dir(pkg)
	if dir == "." || dir == "" {
		return pkg
	}
	return dir
}
