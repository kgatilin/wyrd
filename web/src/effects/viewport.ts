import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';

/**
 * Routes navigation scroll through the ViewportPort. `ChangeActivated` /
 * `TreeFocusRequested` re-lay out (they may expand a component), so their scroll
 * is DEFERRED to the next `LayoutComputed` — landing on the final geometry. A bare
 * `ScrollToComponentRequested` scrolls immediately. `ZoomFitRequested` → fit zoom.
 *
 * Gestures that put one package in focus (selecting a card, expanding it, or
 * picking it in the tree) additionally `fit` it: the zoom is chosen so the whole
 * card is visible and it lands in the middle of the viewport.
 */
export function createViewportEffect(port: ViewportPort): Effect<AppState, Event> {
  let pending: { id: string; fit: boolean } | null = null;
  return (event, getState, dispatch) => {
    const state = getState();
    const laid = state.geometry.laid;

    switch (event.type) {
      case 'LayoutComputed': {
        if (!pending || !state.geometry.laid) return;
        const { id, fit } = pending;
        pending = null;
        if (fit) {
          const zoom = port.focusComponent(id, state.geometry.laid);
          if (zoom != null) dispatch({ type: 'ZoomChanged', zoom });
        } else {
          port.scrollToComponent(id, state.geometry.laid);
        }
        return;
      }
      case 'LayoutFailed':
        pending = null;
        return;
      case 'ChangeActivated':
        // Stepping through the change list: centre it, but keep the reviewer's
        // zoom so consecutive changes don't rescale the canvas under them.
        pending = { id: event.change.cmp, fit: false };
        return;
      case 'TreeFocusRequested':
        pending = { id: event.target.componentId, fit: true };
        return;
      // Picking a hit out of an answer expands its package — land on it once
      // the relayout settles, the same way the review tree does.
      case 'AskHitActivated':
        if (event.hit.inGraph) pending = { id: event.hit.packageId, fit: true };
        return;
      // Selecting a card focuses and expands it (see focusedPackageView) — bring
      // the whole expanded card into view, centred.
      case 'ComponentSelected':
        if (state.ui.focusId === event.id) pending = { id: event.id, fit: true };
        return;
      // Expanding a card (or flipping it to its sequence view) resizes and
      // shifts the whole graph — centre on it once the relayout lands. The
      // reducer has already run, so state reflects the post-toggle expansion.
      case 'ComponentToggled':
        if (state.ui.expanded.has(event.id)) pending = { id: event.id, fit: true };
        return;
      case 'ComponentSeqToggled':
        if (state.ui.seqMode.has(event.id) && state.ui.expanded.has(event.id)) {
          pending = { id: event.id, fit: true };
        }
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
