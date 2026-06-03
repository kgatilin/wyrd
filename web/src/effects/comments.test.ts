import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import { createCommentsSeedEffect } from './comments';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 10, y: 20, w: 100 }],
  edges: [],
  comments: [{ id: 'cm1', target: { type: 'component', id: 'a' }, body: 'hi' }],
};
const laid = { ...graph };
const stateReady = (): AppState => ({ ...initialState, graph, geometry: { laid, status: 'ready', error: null } });

describe('createCommentsSeedEffect', () => {
  it('on the first LayoutComputed for a graph, dispatches MarkersSeeded with seeded markers', () => {
    const dispatch = vi.fn();
    createCommentsSeedEffect()({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledTimes(1);
    const call = dispatch.mock.calls[0][0];
    expect(call.type).toBe('MarkersSeeded');
    expect(call.markers).toHaveLength(1);
    expect(call.markers[0]).toMatchObject({ id: 'seed-0', target: { type: 'component', id: 'a' } });
  });

  it('seeds only ONCE per graph (a later LayoutComputed for the same graph does not re-seed)', () => {
    const dispatch = vi.fn();
    const effect = createCommentsSeedEffect();
    effect({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    effect({ type: 'LayoutComputed', laid }, stateReady, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it('ignores non-LayoutComputed events and does nothing without a graph', () => {
    const dispatch = vi.fn();
    const effect = createCommentsSeedEffect();
    effect({ type: 'ThemeToggled' }, stateReady, dispatch as (e: Event) => void);
    effect({ type: 'LayoutComputed', laid }, () => initialState, dispatch as (e: Event) => void);
    expect(dispatch).not.toHaveBeenCalled();
  });
});
