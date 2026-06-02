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

describe('update — chrome + zoom slice', () => {
  it('ThemeToggled flips dark/light', () => {
    const s = update(withGraph(), { type: 'ThemeToggled' });
    expect(s.ui.theme).toBe('light');
  });
  it('LevelChanged sets the level', () => {
    expect(update(withGraph(), { type: 'LevelChanged', level: 1 }).ui.level).toBe(1);
  });
  it('LeftTabChanged / collapse toggles', () => {
    let s = update(withGraph(), { type: 'LeftTabChanged', tab: 'changes' });
    expect(s.ui.leftTab).toBe('changes');
    s = update(s, { type: 'LeftCollapsedToggled' });
    expect(s.ui.leftCollapsed).toBe(true);
    s = update(s, { type: 'RightCollapsedToggled' });
    expect(s.ui.rightCollapsed).toBe(true);
  });
  it('ZoomChanged sets the zoom level', () => {
    expect(update(withGraph(), { type: 'ZoomChanged', zoom: 0.5 }).ui.zoom).toBe(0.5);
  });
});

describe('update — load + geometry slice', () => {
  const graph = withGraph().graph!;

  it('GraphRequested sets load status to loading', () => {
    const s = update({ ...initialState, load: { status: 'ready', error: null } }, { type: 'GraphRequested' });
    expect(s.load.status).toBe('loading');
  });

  it('GraphLoaded stores the graph, marks ready, and seeds initial expansion', () => {
    const s = update(initialState, { type: 'GraphLoaded', graph });
    expect(s.graph).toBe(graph);
    expect(s.load.status).toBe('ready');
    expect(s.ui.expanded.has('a')).toBe(true); // initialExpanded → first component
  });

  it('GraphLoaded selects the changes tab when the graph carries a PR', () => {
    const prGraph = { ...graph, pr: { title: 't', branch: 'b', agent: 'x', summary: '', stats: { added: 0, removed: 0, changed: 0, comments: 0 } } };
    const s = update(initialState, { type: 'GraphLoaded', graph: prGraph });
    expect(s.ui.leftTab).toBe('changes');
  });

  it('GraphLoadFailed records the error', () => {
    const s = update(initialState, { type: 'GraphLoadFailed', error: 'boom' });
    expect(s.load).toEqual({ status: 'error', error: 'boom' });
  });

  it('LayoutComputed stores geometry and marks ready', () => {
    const s = update(initialState, { type: 'LayoutComputed', laid: graph });
    expect(s.geometry.laid).toBe(graph);
    expect(s.geometry.status).toBe('ready');
  });

  it('LayoutFailed keeps the last good laid graph and records the error', () => {
    const ready = update(initialState, { type: 'LayoutComputed', laid: graph });
    const failed = update(ready, { type: 'LayoutFailed', error: 'elk-died' });
    expect(failed.geometry.laid).toBe(graph); // last good preserved (no empty flash)
    expect(failed.geometry.status).toBe('error');
    expect(failed.geometry.error).toBe('elk-died');
  });
});
