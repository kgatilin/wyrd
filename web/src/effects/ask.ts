import type { Effect } from '../runtime/store';
import type { AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { SearchPort } from '../domain/ports';

/**
 * Runs a submitted question against the daemon's retrieval index. The answer
 * is dispatched with the query it answers, so the reducer can drop a late
 * response for a question the reviewer has already replaced.
 */
export function createAskEffect(port: SearchPort): Effect<AppState, Event> {
  return (event, getState, dispatch) => {
    if (event.type !== 'AskSubmitted') return;
    const query = event.query.trim();
    if (!query) return;
    const state = getState();
    const worktree =
      state.graph?.repo?.activeWorktree ??
      state.graph?.worktrees?.find((worktree) => worktree.current)?.name ??
      undefined;
    port.search(query, { k: state.ask.k, worktree }).then(
      (answer) => dispatch({ type: 'AskResultsLoaded', query, hits: answer.hits, dense: answer.dense }),
      (err) => dispatch({ type: 'AskFailed', query, error: String(err instanceof Error ? err.message : err) })
    );
  };
}
