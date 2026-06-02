import { describe, it, expect } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from './state';
import { update } from './update';

function withGraph(): AppState {
  const graph: UIGraph = {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      { id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [{ id: 'a.i', kind: 'class', name: 'Ai', members: [] }], ports: [] },
    ],
    edges: [],
    comments: [],
  };
  return { ...initialState, graph };
}

describe('update — focus slice', () => {
  it('ComponentSelected sets focus; selecting the focused one clears it', () => {
    let s = update(withGraph(), { type: 'ComponentSelected', id: 'a' });
    expect(s.ui.focusId).toBe('a');
    s = update(s, { type: 'ComponentSelected', id: 'a' });
    expect(s.ui.focusId).toBeNull();
  });

  it('FocusCleared clears focus', () => {
    const s = update({ ...withGraph(), ui: { ...initialState.ui, focusId: 'a' } }, { type: 'FocusCleared' });
    expect(s.ui.focusId).toBeNull();
  });

  it('CanvasCleared clears focus, activeMarkerId, and pendingComment', () => {
    const base: AppState = {
      ...withGraph(),
      ui: { ...initialState.ui, focusId: 'a', activeMarkerId: 'm-1' },
      pendingComment: { x: 0, y: 0, target: { type: 'component', id: 'a' } },
    };
    const s = update(base, { type: 'CanvasCleared' });
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeMarkerId).toBeNull();
    expect(s.pendingComment).toBeNull();
  });

  it('ChangeActivated sets active change, focuses the component, and expands when drilling in', () => {
    const s = update(withGraph(), {
      type: 'ChangeActivated',
      change: { id: 'mem-x', kind: 'added', name: 'x', where: '', cmp: 'a', internal: 'a.i' },
    });
    expect(s.ui.activeChangeId).toBe('mem-x');
    expect(s.ui.focusId).toBe('a');
    expect(s.ui.expanded.has('a')).toBe(true);
  });

  it('TreeFocusRequested focuses and expands when drilling into an internal', () => {
    const s = update(withGraph(), { type: 'TreeFocusRequested', target: { componentId: 'a', internalId: 'a.i' } });
    expect(s.ui.focusId).toBe('a');
    expect(s.ui.expanded.has('a')).toBe(true);
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('returns the same state object for unknown events', () => {
    const s = withGraph();
    expect(update(s, { type: 'ZoomFitRequested' })).toBe(s);
  });
});
