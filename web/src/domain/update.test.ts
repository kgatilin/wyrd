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

describe('update — expansion slice', () => {
  it('ComponentToggled expands and auto-expands the component internals', () => {
    const s = update(withGraph(), { type: 'ComponentToggled', id: 'a' });
    expect(s.ui.expanded.has('a')).toBe(true);
    expect(s.ui.internalExpanded.has('a.i')).toBe(true);
  });

  it('ComponentToggled collapses an expanded component (internalExpanded is add-only)', () => {
    const opened = update(withGraph(), { type: 'ComponentToggled', id: 'a' });
    const closed = update(opened, { type: 'ComponentToggled', id: 'a' });
    expect(closed.ui.expanded.has('a')).toBe(false);
    expect(closed.ui.internalExpanded.has('a.i')).toBe(true); // not removed, matching current behaviour
  });

  it('InternalWideToggled toggles one internal in fit-width mode', () => {
    let s = update(withGraph(), { type: 'InternalWideToggled', id: 'a.i' });
    expect(s.ui.internalWide.has('a.i')).toBe(true);
    s = update(s, { type: 'InternalWideToggled', id: 'a.i' });
    expect(s.ui.internalWide.has('a.i')).toBe(false);
  });

  it('ComponentAllWideSet adds/removes every internal of a component', () => {
    let s = update(withGraph(), { type: 'ComponentAllWideSet', id: 'a', wide: true });
    expect(s.ui.internalWide.has('a.i')).toBe(true);
    s = update(s, { type: 'ComponentAllWideSet', id: 'a', wide: false });
    expect(s.ui.internalWide.has('a.i')).toBe(false);
  });
});
