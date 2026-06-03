import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';

/**
 * Routes navigation scroll through the ViewportPort. `ChangeActivated` /
 * `TreeFocusRequested` re-lay out (they may expand a component), so their scroll
 * is DEFERRED to the next `LayoutComputed` — landing on the final geometry. A bare
 * `ScrollToComponentRequested` scrolls immediately. `ZoomFitRequested` → fit zoom.
 */
export function createViewportEffect(port: ViewportPort): Effect<AppState, Event> {
  let pendingScrollId: string | null = null;
  return (event, getState, dispatch) => {
    const state = getState();
    const laid = state.geometry.laid;

    switch (event.type) {
      case 'LayoutComputed':
        if (pendingScrollId && state.geometry.laid) {
          port.scrollToComponent(pendingScrollId, state.geometry.laid);
          pendingScrollId = null;
        }
        return;
      case 'LayoutFailed':
        pendingScrollId = null;
        return;
      case 'ChangeActivated':
        pendingScrollId = event.change.cmp;
        return;
      case 'TreeFocusRequested':
        pendingScrollId = event.target.componentId;
        return;
      case 'ScrollToComponentRequested':
        if (laid) port.scrollToComponent(event.id, laid);
        return;
      case 'ZoomFitRequested': {
        if (!laid) return;
        const z = port.fitZoom(laid);
        if (z != null) dispatch({ type: 'ZoomChanged', zoom: z });
        return;
      }
      default:
        return;
    }
  };
}
