# Worktree Architecture Review UI - Product Requirements

Date: 2026-06-04
Status: draft
Branch: `poc/arch-review-ui-worktree-review-doc`

## Product Goal

Archai should be a local review server for a large repository with many git
worktrees.

The user starts Archai once for the whole repo, opens the UI, picks a worktree,
and reviews what changed compared to `main`.

The default experience should answer:

- Did the top-level framework API change?
- Did any public package surface change?
- Did internal implementation architecture change?
- Which packages and groups are impacted?
- Which dependencies were added, removed, or changed?

The user should not have to export JSON manually, pass target files, or know how
the server reads `archai.yaml`.

## Core User Flow

```text
archai serve --repo /path/to/repo --ui
```

Then in the UI:

```text
Repo: myproject
Compare: feature-123 vs main
View: Framework API
Scope: Top-level Public API
Show: Changes + impacted neighbors
```

The user can switch worktrees from a picker and the screen updates to show that
worktree's diff against `main`.

## First-Screen Experience

The first screen is a review screen, not a full repository explorer.

By default it should show only:

- changed packages/components;
- changed public API elements;
- directly impacted dependency neighbors;
- a summary of additions, removals, and changes.

The user can expand outward into a wider architecture view when needed.

## Review Scopes

The UI needs a first-class scope switch:

```text
Scope:
[Top-level Public API] [All Public API] [Internal Implementation] [Everything]
```

### Top-level Public API

The highest-signal review mode.

This scope focuses on exported types, functions, interfaces, methods, fields,
constants, and config structs in the packages that form the framework-facing API.

For a framework project, this is the mode used to answer:

```text
Did this branch change the framework API that consumers depend on?
```

### All Public API

Shows exported surface across all packages in the selected review view,
including `internal/...` packages.

This matters because exported symbols inside internal packages are still
important architecture contracts inside the repo, but they are lower priority
than top-level framework API.

### Internal Implementation

Shows private implementation details, helper types, private functions, internal
dependencies, and package structure changes.

This is a drill-down mode, not the default.

### Everything

Debug mode. Useful for deep investigation, but not the normal review experience.

## Public API Source Of Truth

Public review scopes should be based on a server-side `PublicSurface`, not on UI
heuristics.

The surface should include:

- exported package-level functions;
- exported types, interfaces, structs, constants, variables, and sentinel errors;
- exported methods, fields, and enum-like constants that belong to exported
  types;
- public package dependency edges that are reachable through exported symbols.

The UI may filter already-resolved graph data for interaction speed, but the
meaning of "public" must come from the server projection. Private implementation
dependencies between otherwise visible packages should not appear in public API
review scopes.

## Review Views

A review view is a named perspective over the repository.

It decides:

- which packages are included;
- which packages are excluded;
- which scope is selected by default;
- how packages are grouped by default;
- whether the view should start collapsed or expanded.

The UI should present these as plain user-facing choices:

```text
View:
[Framework API] [Runtime] [Plugins] [Storage] [Internal] [All]
```

The product term should be `Review View`. The UI should not require the user to
think in terms of `archai.yaml`, overlays, layers, or bounded contexts.

## Package Selection

Review views need package selectors that work on large repos without listing
every package manually.

Selectors should support `include` and `exclude`.

`default_expansion` controls the initial package-card expansion for that view:
`changed` opens packages with local symbol changes, `collapsed` opens none,
`expanded` opens every visible package, and `auto` keeps the compact UI default.

Suggested pattern language:

- `*` matches one top-level package segment.
- `pkg/*` matches direct children under `pkg`.
- `pkg/...` matches `pkg` and every package below it.
- exact package paths are allowed.
- `exclude` always wins over `include`.

Example:

```yaml
review_views:
  framework_api:
    title: Framework API
    default_scope: top_level_public_api
    default_expansion: changed
    group_by: api_area
    packages:
      include:
        - "*"
      exclude:
        - "internal"
        - "internal/..."
        - "test"
        - "test/..."
        - "tests"
        - "tests/..."
        - "tools"
        - "tools/..."

  internal_runtime:
    title: Internal Runtime
    default_scope: all_public_api
    default_expansion: collapsed
    group_by: configured_groups
    packages:
      include:
        - "internal/..."
      exclude:
        - "internal/testing/..."
        - "internal/testdata/..."

  plugins:
    title: Plugins
    default_scope: all_public_api
    default_expansion: collapsed
    group_by: directory
    packages:
      include:
        - "internal/plugins/..."
        - "plugin/..."
      exclude:
        - "internal/plugins/*/testdata/..."
```

This allows a view such as "all top-level packages except tests/tools/internal"
without enumerating package names.

## Grouping

Grouping is a server-side concern.

The server reads configuration and returns already-resolved groups to the UI.
The UI should not parse the overlay and should not know why a package belongs to
a group.

The UI only needs a list of group choices:

```text
Group by:
[Review View] [Configured Groups] [Layer] [Directory] [Package Owner]
```

The server can derive those groups from config, package path, ownership data, or
future sources. The UI receives the finished structure.

This keeps the UI simple and lets the product vocabulary evolve without forcing
the frontend to understand every config concept.

Example server-side owner configuration:

```yaml
package_owners:
  platform:
    name: Platform API
    packages:
      include:
        - "*"
      exclude:
        - "internal/..."
        - "tools/..."

  runtime:
    name: Runtime Team
    packages:
      include:
        - "internal/runtime/..."

  plugins:
    name: Plugin Team
    packages:
      include:
        - "internal/plugins/..."
        - "plugin/..."
      exclude:
        - "internal/plugins/*/testdata/..."
```

`Package Owner` is still a grouping projection, not a UI-side config concept.
The server resolves these selectors into concrete package ids before returning
the graph.

## Large Repository Behavior

The product must work for repositories with hundreds or thousands of packages.

The UI should not try to render the entire repository by default.

Default view:

```text
changed elements + impacted neighbors
```

Progressive expansion:

1. Show changed package/component.
2. Expand to direct dependencies and dependents.
3. Expand to the containing group.
4. Expand to the whole review view.
5. Expand to the whole repository only on explicit request.

The UI should also provide filters:

- only additions;
- only removals;
- only changed signatures;
- only dependency changes;
- only policy/grouping violations;
- hide unchanged neighbors.

## Layout Customization

Automatic layout should be the default.

Manual layout is an escape hatch for review readability, not the primary way to
use the tool.

Needed interactions:

- drag a component/group to move it;
- mark moved items as pinned;
- save pinned positions;
- reset layout for one item, one group, one view, or the whole repo;
- keep new/unpinned nodes auto-laid out around pinned nodes.

Saved positions should be scoped by:

```text
repo + review view + scope + grouping
```

They should not be scoped to a single worktree by default. A good layout for
`Framework API / Top-level Public API` should remain useful across branches.

Worktree-specific temporary positioning can exist, but the default persisted
layout should be stable across review sessions.

## UI Customization

Current POC capabilities that should remain:

- dark/light theme;
- zoom;
- pan;
- fit to screen;
- expand/collapse components;
- expand/collapse internals;
- focus mode;
- changes/context tree tabs;
- comments and pins;
- automatic ELK layout.

Future customization:

- saved review view defaults;
- saved scope defaults per view;
- saved grouping defaults per view;
- pinned layout positions;
- reset layout;
- hide/show group labels;
- hide/show unchanged neighbors;
- compact vs detailed cards;
- show signatures inline vs in details panel.

## Non-Goals For The First Product Slice

- Manual JSON export as a required user step.
- Full repository graph as the default screen.
- Making the UI parse `archai.yaml` directly.
- Manual diagram editing as the primary workflow.
- Target lock/amend/import/export as a prerequisite for worktree review.

Targets can become a later workflow for "approved architecture shape", but the
basic product should first solve:

```text
Show me this worktree's architectural and API changes compared to main.
```

## MVP

1. Start one repo-level server with UI.
2. Discover git worktrees.
3. Let the user select a worktree.
4. Compare the selected worktree to `main`.
5. Support review scopes:
   - Top-level Public API;
   - All Public API;
   - Internal Implementation.
6. Support configurable review views with package include/exclude selectors.
7. Render changed elements plus impacted neighbors by default.
8. Let the user switch grouping.
9. Keep UI unaware of overlay internals.
10. Add manual layout pinning after the default review flow works.

## Product Principle

The UI is a review surface, not a repository dump.

Archai should guide the user from the highest-signal API changes into deeper
implementation details only when they choose to drill down.
