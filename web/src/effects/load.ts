import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort } from '../domain/ports';

export function createLoadEffect(port: GraphSourcePort): Effect<AppState, Event> {
  return (event, _getState, dispatch) => {
    if (event.type !== 'GraphRequested') return;
    port.load().then(
      (graph) => dispatch({ type: 'GraphLoaded', graph }),
      (err) => dispatch({ type: 'GraphLoadFailed', error: String(err) })
    );
  };
}
