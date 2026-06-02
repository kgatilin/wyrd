import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';

const SCROLL_TRIGGERS: ReadonlySet<Event['type']> = new Set([
  'ChangeActivated',
  'TreeFocusRequested',
  'ScrollToComponentRequested',
]);

export function createViewportEffect(port: ViewportPort): Effect<AppState, Event> {
  return (event, getState, dispatch) => {
    const state = getState();
    const laid = state.geometry.laid;

    if (event.type === 'ZoomFitRequested') {
      if (!laid) return;
      const z = port.fitZoom(laid);
      if (z != null) dispatch({ type: 'ZoomChanged', zoom: z });
      return;
    }

    if (!SCROLL_TRIGGERS.has(event.type) || !laid) return;

    const id =
      event.type === 'ChangeActivated' ? event.change.cmp
      : event.type === 'TreeFocusRequested' ? event.target.componentId
      : event.type === 'ScrollToComponentRequested' ? event.id
      : null;

    if (id) port.scrollToComponent(id, laid);
  };
}
