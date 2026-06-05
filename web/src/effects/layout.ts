import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { LayoutPort } from '../domain/ports';
import { selectReviewGraph, toInteraction } from '../domain/derive';

const LAYOUT_TRIGGERS: ReadonlySet<Event['type']> = new Set([
  'GraphLoaded',
  'ComponentToggled',
  'InternalWideToggled',
  'ComponentAllWideSet',
  'ChangeActivated',
  'TreeFocusRequested',
  'ReviewViewChanged',
  'ReviewScopeChanged',
  'ReviewGroupingChanged',
  'ReviewImpactModeChanged',
  'ReviewChangeFilterChanged',
  'UnchangedNeighborsToggled',
  'ChangedDetailsOnlyToggled',
  'CardDensityChanged',
  'InlineSignaturesToggled',
]);

export function createLayoutEffect(port: LayoutPort): Effect<AppState, Event> {
  let seq = 0;
  return (event, getState, dispatch) => {
    if (!LAYOUT_TRIGGERS.has(event.type)) return;
    const state = getState();
    if (!state.graph) return;
    const graph = selectReviewGraph(
      state.graph,
      state.ui.reviewViewId,
      state.ui.reviewScopeId,
      state.ui.reviewGroupingId,
      {
        impactMode: state.ui.reviewImpactMode,
        changeFilter: state.ui.reviewChangeFilter,
        hideUnchangedNeighbors: state.ui.hideUnchangedNeighbors,
        changedDetailsOnly: state.ui.changedDetailsOnly,
      }
    );
    const mySeq = ++seq;
    port.compute(graph, toInteraction(state.ui)).then(
      (laid) => { if (mySeq === seq) dispatch({ type: 'LayoutComputed', laid }); },
      (err) => { if (mySeq === seq) dispatch({ type: 'LayoutFailed', error: String(err) }); }
    );
  };
}
