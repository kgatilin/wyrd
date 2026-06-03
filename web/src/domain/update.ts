import type { AppState } from './state';
import type { Event } from './events';
import { addInternalsOfExpanded, initialExpanded } from './derive';

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
    default:
      return state;
  }
}

function loadGeometrySlice(state: AppState, event: Event): AppState {
  switch (event.type) {
    case 'GraphRequested':
      return { ...state, load: { status: 'loading', error: null } };
    case 'GraphLoaded': {
      const graph = event.graph;
      const expanded = new Set(initialExpanded(graph));
      const internalExpanded = addInternalsOfExpanded(graph, expanded, new Set());
      const leftTab = graph.pr != null ? 'changes' : state.ui.leftTab;
      return {
        ...state,
        graph,
        load: { status: 'ready', error: null },
        ui: { ...state.ui, expanded, internalExpanded, leftTab },
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
