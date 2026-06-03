import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { UIGraph } from '../types';
import { seedMarkers } from '../domain/derive';

/**
 * Seeds comment markers from graph.comments ONCE per loaded graph — on the first
 * `LayoutComputed` after a graph loads, so positions use real laid geometry and
 * user-added comments persist across re-layouts (navigation now re-lays out).
 */
export function createCommentsSeedEffect(): Effect<AppState, Event> {
  let seededGraph: UIGraph | null = null;
  return (event, getState, dispatch) => {
    if (event.type !== 'LayoutComputed') return;
    const state = getState();
    if (!state.graph || state.graph === seededGraph) return;
    seededGraph = state.graph;
    dispatch({ type: 'MarkersSeeded', markers: seedMarkers(state.graph, state.geometry.laid) });
  };
}
