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

  it('re-lays out on ChangeActivated (drill-in navigation expands a component)', async () => {
    const laid = { ...graph };
    const port: LayoutPort = { compute: vi.fn().mockResolvedValue(laid) };
    const dispatch = vi.fn();
    createLayoutEffect(port)(
      { type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a', internal: 'a.i' } },
      () => stateWith(graph),
      dispatch as (e: Event) => void
    );
    await flush();
    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(dispatch).toHaveBeenCalledWith({ type: 'LayoutComputed', laid });
  });

  it('re-lays out on TreeFocusRequested (drill-in navigation expands a component)', async () => {
    const laid = { ...graph };
    const port: LayoutPort = { compute: vi.fn().mockResolvedValue(laid) };
    const dispatch = vi.fn();
    createLayoutEffect(port)(
      { type: 'TreeFocusRequested', target: { componentId: 'a', internalId: 'a.i' } },
      () => stateWith(graph),
      dispatch as (e: Event) => void
    );
    await flush();
    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(dispatch).toHaveBeenCalledWith({ type: 'LayoutComputed', laid });
  });

  it('re-lays out using the hide-unchanged-neighbors projection', async () => {
    const reviewGraph: UIGraph = {
      ...graph,
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [] },
        { id: 'b', name: 'B', tech: '', desc: '', bc: 'bc1', diff: 'added', internals: [], ports: [] },
      ],
      edges: [{ id: 'ab', from: 'a', to: 'b', fromPort: '', toPort: '', label: '' }],
    };
    const laid = { ...reviewGraph };
    const compute = vi.fn().mockResolvedValue(laid);
    const port: LayoutPort = { compute };
    const dispatch = vi.fn();
    createLayoutEffect(port)(
      { type: 'UnchangedNeighborsToggled' },
      () => ({ ...stateWith(reviewGraph), ui: { ...initialState.ui, hideUnchangedNeighbors: true } }),
      dispatch as (e: Event) => void
    );
    await flush();

    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(compute.mock.calls[0][0].components.map((c: { id: string }) => c.id)).toEqual(['b']);
  });

  it('re-lays out using the changed-details-only projection', async () => {
    const reviewGraph: UIGraph = {
      ...graph,
      pr: { title: 'Review', branch: 'feature', agent: 'archai', summary: '', stats: { added: 1, removed: 0, changed: 0, comments: 0 } },
      components: [
        {
          id: 'a',
          name: 'A',
          tech: '',
          desc: '',
          bc: 'bc1',
          internals: [
            { id: 'a.Changed', kind: 'class', name: 'Changed', diff: 'added', members: [] },
            { id: 'a.Unchanged', kind: 'class', name: 'Unchanged', members: [] },
          ],
          ports: [],
        },
      ],
      edges: [],
    };
    const laid = { ...reviewGraph };
    const compute = vi.fn().mockResolvedValue(laid);
    const port: LayoutPort = { compute };
    const dispatch = vi.fn();
    createLayoutEffect(port)(
      { type: 'ChangedDetailsOnlyToggled' },
      () => ({ ...stateWith(reviewGraph), ui: { ...initialState.ui, changedDetailsOnly: true } }),
      dispatch as (e: Event) => void
    );
    await flush();

    expect(port.compute).toHaveBeenCalledTimes(1);
    expect(compute.mock.calls[0][0].components[0].internals.map((i: { id: string }) => i.id)).toEqual(['a.Changed']);
  });
});
