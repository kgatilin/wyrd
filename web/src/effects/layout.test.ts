import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { LayoutPort } from '../domain/ports';
import { createLayoutEffect } from './layout';

const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] }],
  edges: [],
  comments: [],
};
const stateWith = (graphIn: UIGraph | null): AppState => ({ ...initialState, graph: graphIn });
const flush = () => new Promise((r) => setTimeout(r));

describe('createLayoutEffect', () => {
  it('on a trigger event, computes layout and dispatches LayoutComputed', async () => {
    const laid = { ...graph };
    const port: LayoutPort = { compute: vi.fn().mockResolvedValue(laid) };
    const dispatch = vi.fn();
    createLayoutEffect(port)({ type: 'GraphLoaded', graph }, () => stateWith(graph), dispatch as (e: Event) => void);
    await flush();
    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(dispatch).toHaveBeenCalledWith({ type: 'LayoutComputed', laid });
  });

  it('does nothing when there is no graph', () => {
    const port: LayoutPort = { compute: vi.fn() };
    createLayoutEffect(port)({ type: 'GraphLoaded', graph }, () => stateWith(null), vi.fn());
    expect(port.compute).not.toHaveBeenCalled();
  });

  it('drops a stale result when a newer trigger superseded it (race guard)', async () => {
    let resolveFirst!: (g: UIGraph) => void;
    const first = new Promise<UIGraph>((r) => { resolveFirst = r; });
    const second = Promise.resolve({ ...graph, schema: 'second' });
    const compute = vi.fn().mockReturnValueOnce(first).mockReturnValueOnce(second);
    const port: LayoutPort = { compute };
    const dispatch = vi.fn();
    const effect = createLayoutEffect(port);
    const get = () => stateWith(graph);

    effect({ type: 'ComponentToggled', id: 'a' }, get, dispatch as (e: Event) => void); // seq 1
    effect({ type: 'ComponentToggled', id: 'a' }, get, dispatch as (e: Event) => void); // seq 2
    await flush();
    resolveFirst(graph); // stale seq-1 resolves last
    await flush();

    const laidDispatches = dispatch.mock.calls.filter((c) => c[0].type === 'LayoutComputed');
    expect(laidDispatches).toHaveLength(1);
    expect(laidDispatches[0][0].laid.schema).toBe('second'); // only the latest wins
  });
});
