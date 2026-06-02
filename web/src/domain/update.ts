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

export function update(state: AppState, event: Event): AppState {
  return focusSlice(state, event);
}
