import type { AppState } from './state';
import type { Event } from './events';
import { addInternalsOfExpanded, initialExpanded, selectReviewGraph } from './derive';
import {
  defaultsWithGrouping,
  defaultsWithReviewView,
  defaultsWithScope,
  normalizeReviewDefaults,
  type ReviewDefaults,
} from './reviewDefaults';
import type { UIGraph } from '../types';

function expandComponent(state: AppState, id: string): AppState {
  if (!state.graph || state.ui.expanded.has(id)) return state;
  const expanded = new Set(state.ui.expanded);
  expanded.add(id);
  const internalExpanded = addInternalsOfExpanded(state.graph, expanded, state.ui.internalExpanded);
  return { ...state, ui: { ...state.ui, expanded, internalExpanded } };
}

function focusSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'ComponentSelected': {
      const focusId = state.ui.focusId === event.id ? null : event.id;
      return { ...state, ui: { ...state.ui, focusId } };
    }
    case 'FocusCleared':
      return { ...state, ui: { ...state.ui, focusId: null } };
    case 'CanvasCleared':
      return { ...state, ui: { ...state.ui, focusId: null, activeMarkerId: null }, pendingComment: null };
    case 'ChangeActivated': {
      const { change } = event;
      const drillIn = !!(change.internal || change.member || change.port);
      let next: AppState = { ...state, ui: { ...state.ui, activeChangeId: change.id, focusId: change.cmp } };
      if (drillIn) next = expandComponent(next, change.cmp);
      return next;
    }
    case 'TreeFocusRequested': {
      const { target } = event;
      const drillIn = !!(target.internalId || target.memberId);
      let next: AppState = { ...state, ui: { ...state.ui, activeChangeId: null, focusId: target.componentId } };
      if (drillIn) next = expandComponent(next, target.componentId);
      return next;
    }
    default:
      return state;
  }
}

function expansionSlice(state: AppState, event: Event): AppState {
  if (!state.graph) return state;
  switch (event.type) {
    case 'ComponentToggled': {
      const expanded = new Set(state.ui.expanded);
      if (expanded.has(event.id)) expanded.delete(event.id);
      else expanded.add(event.id);
      const internalExpanded = addInternalsOfExpanded(state.graph, expanded, state.ui.internalExpanded);
      return { ...state, ui: { ...state.ui, expanded, internalExpanded } };
    }
    case 'InternalWideToggled': {
      const internalWide = new Set(state.ui.internalWide);
      if (internalWide.has(event.id)) internalWide.delete(event.id);
      else internalWide.add(event.id);
      return { ...state, ui: { ...state.ui, internalWide } };
    }
    case 'ComponentAllWideSet': {
      const comp = state.graph.components.find((c) => c.id === event.id);
      if (!comp) return state;
      const internalWide = new Set(state.ui.internalWide);
      for (const internal of comp.internals) {
        if (event.wide) internalWide.add(internal.id);
        else internalWide.delete(internal.id);
      }
      return { ...state, ui: { ...state.ui, internalWide } };
    }
    default:
      return state;
  }
}

function chromeSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'ThemeToggled':
      return { ...state, ui: { ...state.ui, theme: state.ui.theme === 'dark' ? 'light' : 'dark' } };
    case 'LevelChanged':
      return { ...state, ui: { ...state.ui, level: event.level } };
    case 'LeftTabChanged':
      return { ...state, ui: { ...state.ui, leftTab: event.tab } };
    case 'LeftCollapsedToggled':
      return { ...state, ui: { ...state.ui, leftCollapsed: !state.ui.leftCollapsed } };
    case 'RightCollapsedToggled':
      return { ...state, ui: { ...state.ui, rightCollapsed: !state.ui.rightCollapsed } };
    case 'ZoomChanged':
      return { ...state, ui: { ...state.ui, zoom: event.zoom } };
    case 'ReviewViewChanged': {
      const view = state.graph?.reviewViews?.find((v) => v.id === event.id);
      const preferredScopeId = state.ui.reviewDefaults.scopeByView?.[event.id];
      const preferredGroupingId = state.ui.reviewDefaults.groupingByView?.[event.id];
      const reviewGroupingId = state.graph
        ? groupingForView(state.graph, event.id, preferredGroupingId, state.ui.reviewGroupingId)
        : state.ui.reviewGroupingId;
      const reviewScopeId = validReviewScope(state.graph, preferredScopeId) ? preferredScopeId : (view?.defaultScope ?? state.ui.reviewScopeId);
      const expansion = state.graph
        ? expandedForReviewSelection(
          state.graph,
          event.id,
          reviewScopeId,
          reviewGroupingId,
          state.ui.reviewImpactMode,
          state.ui.reviewChangeFilter,
          state.ui.hideUnchangedNeighbors,
          state.ui.changedDetailsOnly
        )
        : { expanded: state.ui.expanded, internalExpanded: state.ui.internalExpanded };
      return {
        ...state,
        ui: {
          ...state.ui,
          reviewViewId: event.id,
          reviewScopeId,
          reviewGroupingId,
          reviewDefaults: defaultsWithReviewView(state.ui.reviewDefaults, event.id),
          expanded: expansion.expanded,
          internalExpanded: expansion.internalExpanded,
          focusId: null,
          activeChangeId: null,
        },
      };
    }
    case 'ReviewScopeChanged':
      return {
        ...state,
        ui: {
          ...state.ui,
          reviewScopeId: event.id,
          reviewDefaults: defaultsWithScope(state.ui.reviewDefaults, state.ui.reviewViewId, event.id),
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'ReviewGroupingChanged':
      return {
        ...state,
        ui: {
          ...state.ui,
          reviewGroupingId: event.id,
          reviewDefaults: defaultsWithGrouping(state.ui.reviewDefaults, state.ui.reviewViewId, event.id),
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'ReviewImpactModeChanged':
      return {
        ...state,
        ui: {
          ...state.ui,
          reviewImpactMode: event.mode,
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'ReviewChangeFilterChanged':
      return {
        ...state,
        ui: {
          ...state.ui,
          reviewChangeFilter: event.filter,
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'UnchangedNeighborsToggled':
      return {
        ...state,
        ui: {
          ...state.ui,
          hideUnchangedNeighbors: !state.ui.hideUnchangedNeighbors,
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'ChangedDetailsOnlyToggled':
      return {
        ...state,
        ui: {
          ...state.ui,
          changedDetailsOnly: !state.ui.changedDetailsOnly,
          focusId: null,
          activeChangeId: null,
        },
      };
    case 'ReviewDefaultsLoaded':
      return applyReviewDefaults(state, event.key, event.defaults);
    case 'GroupLabelsToggled':
      return {
        ...state,
        ui: {
          ...state.ui,
          showGroupLabels: !state.ui.showGroupLabels,
        },
      };
    case 'CardDensityChanged':
      return {
        ...state,
        ui: {
          ...state.ui,
          cardDensity: event.density,
        },
      };
    case 'InlineSignaturesToggled':
      return {
        ...state,
        ui: {
          ...state.ui,
          showInlineSignatures: !state.ui.showInlineSignatures,
        },
      };
    case 'LayoutPinsLoaded':
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPinScopeKey: event.scopeKey,
          layoutPins: event.pins,
        },
      };
    case 'ComponentLayoutPinned':
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPins: {
            ...state.ui.layoutPins,
            [event.id]: { x: Math.round(event.x), y: Math.round(event.y) },
          },
        },
      };
    case 'ComponentsLayoutPinned':
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPins: {
            ...state.ui.layoutPins,
            ...event.pins,
          },
        },
      };
    case 'LayoutPinReset': {
      const { [event.id]: _removed, ...layoutPins } = state.ui.layoutPins;
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPins,
        },
      };
    }
    case 'LayoutGroupPinsReset': {
      const ids = new Set(event.componentIds);
      const layoutPins = Object.fromEntries(
        Object.entries(state.ui.layoutPins).filter(([id]) => !ids.has(id))
      );
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPins,
        },
      };
    }
    case 'LayoutPinsReset':
    case 'LayoutRepoPinsReset':
      return {
        ...state,
        ui: {
          ...state.ui,
          layoutPins: {},
        },
      };
    default:
      return state;
  }
}

function loadGeometrySlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'GraphRequested':
    case 'WorktreeChanged':
      return { ...state, load: { status: 'loading', error: null } };
    case 'GraphLoaded': {
      const graph = event.graph;
      const leftTab = graph.pr != null ? 'changes' : state.ui.leftTab;
      const reviewViewId = graph.defaultReviewView ?? graph.reviewViews?.[0]?.id ?? null;
      const reviewScopeId =
        graph.defaultReviewScope ??
        graph.reviewViews?.find((v) => v.id === reviewViewId)?.defaultScope ??
        graph.reviewScopes?.[0]?.id ??
        null;
      const reviewGroupingId = defaultGroupingForGraph(graph, reviewViewId, null);
      const reviewDefaults = seedReviewDefaults(reviewViewId, reviewScopeId, reviewGroupingId);
      const expansion = expandedForReviewSelection(
        graph,
        reviewViewId,
        reviewScopeId,
        reviewGroupingId,
        state.ui.reviewImpactMode,
        state.ui.reviewChangeFilter,
        state.ui.hideUnchangedNeighbors,
        state.ui.changedDetailsOnly
      );
      return {
        ...state,
        graph,
        load: { status: 'ready', error: null },
        ui: {
          ...state.ui,
          leftTab,
          reviewViewId,
          reviewScopeId,
          reviewGroupingId,
          reviewDefaultsKey: null,
          reviewDefaults,
          expanded: expansion.expanded,
          internalExpanded: expansion.internalExpanded,
        },
      };
    }
    case 'GraphLoadFailed':
      return { ...state, load: { status: 'error', error: event.error } };
    case 'LayoutComputed':
      return { ...state, geometry: { laid: event.laid, status: 'ready', error: null } };
    case 'LayoutFailed':
      return { ...state, geometry: { ...state.geometry, status: 'error', error: event.error } };
    default:
      return state;
  }
}

function commentsSlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'CommentStarted':
      return { ...state, pendingComment: { target: event.target, x: event.anchor.x, y: event.anchor.y } };
    case 'CommentSubmitted': {
      if (!state.pendingComment) return state;
      const n = state.markers.length + 1;
      const marker = {
        id: `m-${n}`,
        n,
        x: state.pendingComment.x,
        y: state.pendingComment.y - 8,
        target: state.pendingComment.target,
        body: event.text,
        author: '@you',
        when: 'just now',
      };
      return { ...state, markers: [...state.markers, marker], pendingComment: null, ui: { ...state.ui, activeMarkerId: marker.id } };
    }
    case 'CommentCancelled':
      return { ...state, pendingComment: null };
    case 'MarkerActivated':
      return { ...state, ui: { ...state.ui, activeMarkerId: event.id } };
    case 'MarkersSeeded':
      return { ...state, markers: event.markers };
    default:
      return state;
  }
}

export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  next = chromeSlice(next, event);
  next = loadGeometrySlice(next, event);
  next = commentsSlice(next, event);
  return next;
}

function applyReviewDefaults(state: AppState, key: string, defaults: ReviewDefaults): AppState {
  if (!state.graph) {
    return { ...state, ui: { ...state.ui, reviewDefaultsKey: key, reviewDefaults: defaults } };
  }
  const graph = state.graph;
  const normalized = normalizeReviewDefaults(defaults, graph);
  const reviewViewId = validReviewView(graph, normalized.reviewViewId)
    ? normalized.reviewViewId
    : state.ui.reviewViewId;
  const preferredScopeId = reviewViewId ? normalized.scopeByView?.[reviewViewId] : undefined;
  const preferredGroupingId = reviewViewId ? normalized.groupingByView?.[reviewViewId] : undefined;
  const reviewScopeId = validReviewScope(graph, preferredScopeId)
    ? preferredScopeId
    : state.ui.reviewScopeId;
  const reviewGroupingId = groupingForView(graph, reviewViewId, preferredGroupingId, state.ui.reviewGroupingId);

  const mergedDefaults = seedReviewDefaults(reviewViewId, reviewScopeId, reviewGroupingId, normalized);
  const expansion = expandedForReviewSelection(
    graph,
    reviewViewId,
    reviewScopeId,
    reviewGroupingId,
    state.ui.reviewImpactMode,
    state.ui.reviewChangeFilter,
    state.ui.hideUnchangedNeighbors,
    state.ui.changedDetailsOnly
  );

  return {
    ...state,
    ui: {
      ...state.ui,
      reviewViewId,
      reviewScopeId,
      reviewGroupingId,
      reviewDefaultsKey: key,
      reviewDefaults: mergedDefaults,
      expanded: expansion.expanded,
      internalExpanded: expansion.internalExpanded,
      focusId: null,
      activeChangeId: null,
    },
  };
}

function expandedForReviewSelection(
  graph: UIGraph,
  reviewViewId: string | null,
  reviewScopeId: string | null,
  reviewGroupingId: string | null,
  impactMode: AppState['ui']['reviewImpactMode'],
  changeFilter: AppState['ui']['reviewChangeFilter'],
  hideUnchangedNeighbors: boolean,
  changedDetailsOnly: boolean
): { expanded: Set<string>; internalExpanded: Set<string> } {
  const visible = selectReviewGraph(graph, reviewViewId, reviewScopeId, reviewGroupingId, {
    impactMode,
    changeFilter,
    hideUnchangedNeighbors,
    changedDetailsOnly,
  });
  const expanded = new Set(initialExpanded(visible, reviewViewDefaultExpansion(graph, reviewViewId)));
  const internalExpanded = addInternalsOfExpanded(visible, expanded, new Set());
  return { expanded, internalExpanded };
}

function reviewViewDefaultExpansion(graph: UIGraph, reviewViewId: string | null): string {
  if (!reviewViewId) return 'auto';
  return graph.reviewViews?.find((view) => view.id === reviewViewId)?.defaultExpansion ?? 'auto';
}

function seedReviewDefaults(
  reviewViewId: string | null,
  reviewScopeId: string | null,
  reviewGroupingId: string | null,
  base: ReviewDefaults = {}
): ReviewDefaults {
  let defaults = base;
  if (reviewViewId) defaults = defaultsWithReviewView(defaults, reviewViewId);
  if (reviewScopeId) defaults = defaultsWithScope(defaults, reviewViewId, reviewScopeId);
  if (reviewGroupingId) defaults = defaultsWithGrouping(defaults, reviewViewId, reviewGroupingId);
  return defaults;
}

function defaultGroupingForGraph(
  graph: UIGraph,
  reviewViewId: string | null,
  currentGroupingId: string | null
): string | null {
  const groupings = graph.reviewGroupings ?? [];
  if (groupings.length === 0) return null;
  const hasGrouping = (id: string | null | undefined) => !!id && groupings.some((grouping) => grouping.id === id);
  const view = reviewViewId ? graph.reviewViews?.find((v) => v.id === reviewViewId) : undefined;
  if (hasGrouping(view?.groupBy)) return view!.groupBy!;
  if (hasGrouping(currentGroupingId)) return currentGroupingId;
  if (hasGrouping(graph.defaultGrouping)) return graph.defaultGrouping!;
  return groupings[0].id;
}

function groupingForView(
  graph: UIGraph,
  reviewViewId: string | null,
  preferredGroupingId: string | null | undefined,
  currentGroupingId: string | null
): string | null {
  const groupings = graph.reviewGroupings ?? [];
  const hasGrouping = (id: string | null | undefined) => !!id && groupings.some((grouping) => grouping.id === id);
  if (hasGrouping(preferredGroupingId)) return preferredGroupingId!;
  return defaultGroupingForGraph(graph, reviewViewId, currentGroupingId);
}

function validReviewView(graph: UIGraph | null, id: string | null | undefined): id is string {
  return !!id && !!graph?.reviewViews?.some((view) => view.id === id);
}

function validReviewScope(graph: UIGraph | null, id: string | null | undefined): id is string {
  return !!id && !!graph?.reviewScopes?.some((scope) => scope.id === id);
}
