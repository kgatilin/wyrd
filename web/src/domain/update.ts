import type { AppState } from './state';
import type { Event } from './events';
import { addInternalsOfExpanded } from './derive';

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

export function update(state: AppState, event: Event): AppState {
  let next = focusSlice(state, event);
  next = expansionSlice(next, event);
  return next;
}
