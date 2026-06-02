import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { LayoutPort } from '../domain/ports';
import { toInteraction } from '../domain/derive';

const LAYOUT_TRIGGERS: ReadonlySet<Event['type']> = new Set([
  'GraphLoaded',
  'ComponentToggled',
  'InternalWideToggled',
  'ComponentAllWideSet',
]);

export function createLayoutEffect(port: LayoutPort): Effect<AppState, Event> {
  let seq = 0;
  return (event, getState, dispatch) => {
    if (!LAYOUT_TRIGGERS.has(event.type)) return;
    const state = getState();
    if (!state.graph) return;
    const mySeq = ++seq;
    port.compute(state.graph, toInteraction(state.ui)).then(
      (laid) => { if (mySeq === seq) dispatch({ type: 'LayoutComputed', laid }); },
      (err) => { if (mySeq === seq) dispatch({ type: 'LayoutFailed', error: String(err) }); }
    );
  };
}
