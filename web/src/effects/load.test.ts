import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState } from '../domain/state';
import type { Event } from '../domain/events';
import type { GraphSourcePort, NavigationPort } from '../domain/ports';
import { createLoadEffect } from './load';

const graph: UIGraph = { schema: 'archai.uigraph/v0', boundedContexts: [], components: [], edges: [], comments: [] };
const flush = () => new Promise((r) => setTimeout(r));

describe('createLoadEffect', () => {
  it('on GraphRequested, loads and dispatches GraphLoaded', async () => {
    const port: GraphSourcePort = { load: () => Promise.resolve(graph) };
    const dispatch = vi.fn();
    const effect = createLoadEffect(port);
    effect({ type: 'GraphRequested' }, () => initialState, dispatch as (e: Event) => void);
    await flush();
    expect(dispatch).toHaveBeenCalledWith({ type: 'GraphLoaded', graph });
  });

  it('on load failure, dispatches GraphLoadFailed', async () => {
    const port: GraphSourcePort = { load: () => Promise.reject(new Error('nope')) };
    const dispatch = vi.fn();
    createLoadEffect(port)({ type: 'GraphRequested' }, () => initialState, dispatch as (e: Event) => void);
    await flush();
    expect(dispatch).toHaveBeenCalledWith({ type: 'GraphLoadFailed', error: 'Error: nope' });
  });

  it('on WorktreeChanged, loads that worktree and dispatches GraphLoaded', async () => {
    const load = vi.fn(() => Promise.resolve(graph));
    const port: GraphSourcePort = { load };
    const navigation: NavigationPort = { focusWorktree: vi.fn() };
    const dispatch = vi.fn();
    createLoadEffect(port, navigation)({ type: 'WorktreeChanged', name: 'feature' }, () => initialState, dispatch as (e: Event) => void);
    await flush();
    expect(navigation.focusWorktree).toHaveBeenCalledWith('feature');
    expect(load).toHaveBeenCalledWith('feature');
    expect(dispatch).toHaveBeenCalledWith({ type: 'GraphLoaded', graph });
  });

  it('ignores unrelated events', () => {
    const port: GraphSourcePort = { load: vi.fn() };
    createLoadEffect(port)({ type: 'ThemeToggled' }, () => initialState, vi.fn());
    expect(port.load).not.toHaveBeenCalled();
  });
});
