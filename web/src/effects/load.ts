import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort, NavigationPort } from '../domain/ports';
import type { UIGraph } from '../types';

export function createLoadEffect(port: GraphSourcePort, navigation?: NavigationPort): Effect<AppState, Event> {
  return (event, getState, dispatch) => {
    if (event.type !== 'GraphRequested' && event.type !== 'WorktreeChanged') return;
    const worktree = event.type === 'WorktreeChanged' ? event.name : event.worktree;
    if (event.type === 'WorktreeChanged') {
      navigation?.focusWorktree(event.name);
    }
    const previousGraph = getState().graph;
    const previousFingerprint = previousGraph ? graphFingerprint(previousGraph) : null;
    port.load(worktree).then(
      (graph) => {
        if (previousFingerprint != null && graphFingerprint(graph) === previousFingerprint) {
          dispatch({ type: 'GraphUnchanged' });
          return;
        }
        dispatch({ type: 'GraphLoaded', graph });
      },
      (err) => dispatch({ type: 'GraphLoadFailed', error: String(err) })
    );
  };
}

function graphFingerprint(graph: UIGraph): string {
  return JSON.stringify(graph);
}
