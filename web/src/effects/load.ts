import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort, NavigationPort } from '../domain/ports';

export function createLoadEffect(port: GraphSourcePort, navigation?: NavigationPort): Effect<AppState, Event> {
  return (event, _getState, dispatch) => {
    if (event.type !== 'GraphRequested' && event.type !== 'WorktreeChanged') return;
    const worktree = event.type === 'WorktreeChanged' ? event.name : event.worktree;
    if (event.type === 'WorktreeChanged') {
      navigation?.focusWorktree(event.name);
    }
    port.load(worktree).then(
      (graph) => dispatch({ type: 'GraphLoaded', graph }),
      (err) => dispatch({ type: 'GraphLoadFailed', error: String(err) })
    );
  };
}
