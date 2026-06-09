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

function withTwoComponentGraph(): AppState {
  const graph: UIGraph = {
    schema: 'archai.uigraph/v0',
    boundedContexts: [{ id: 'bc1', name: 'Core' }],
    components: [
      {
        id: 'a',
        name: 'A',
        tech: '',
        desc: '',
        bc: 'bc1',
        internals: [
          { id: 'a.Public', kind: 'class', name: 'Public', exported: true, members: [] },
          { id: 'a.private', kind: 'class', name: 'private', exported: false, members: [] },
        ],
        ports: [],
      },
      {
        id: 'b',
        name: 'B',
        tech: '',
        desc: '',
        bc: 'bc1',
        internals: [{ id: 'b.i', kind: 'class', name: 'Bi', members: [] }],
        ports: [],
      },
    ],
    edges: [],
    comments: [],
  };
  return { ...initialState, graph };
}

describe('update — focus slice', () => {
  it('ComponentSelected enters focused package view; selecting the focused one clears focus', () => {
    let s = update(withTwoComponentGraph(), { type: 'ComponentSelected', id: 'a' });
    expect(s.ui.focusId).toBe('a');
    expect([...s.ui.expanded]).toEqual(['a']);
    expect([...s.ui.internalExpanded].sort()).toEqual(['a.Public', 'a.private']);
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

  it('TreeFocusRequested focuses the whole package and expands all its internals', () => {
    const s = update(withTwoComponentGraph(), { type: 'TreeFocusRequested', target: { componentId: 'a', internalId: 'a.Public' } });
    expect(s.ui.focusId).toBe('a');
    expect(s.ui.expanded.has('a')).toBe(true);
    expect(s.ui.expanded.has('b')).toBe(false);
    expect([...s.ui.internalExpanded].sort()).toEqual(['a.Public', 'a.private']);
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('returns the same state object for unknown events', () => {
    const s = withGraph();
    expect(update(s, { type: 'ZoomFitRequested' })).toBe(s);
  });
});

describe('update — graph loading slice', () => {
  it('auto GraphRequested keeps the current ready graph state unchanged', () => {
    const s = { ...withGraph(), load: { status: 'ready' as const, error: null } };
    expect(update(s, { type: 'GraphRequested', source: 'auto' })).toBe(s);
  });

  it('manual GraphRequested still enters loading state', () => {
    const s = { ...withGraph(), load: { status: 'ready' as const, error: null } };
    const next = update(s, { type: 'GraphRequested', source: 'manual' });
    expect(next).not.toBe(s);
    expect(next.load).toEqual({ status: 'loading', error: null });
  });

  it('GraphUnchanged is a no-op when already ready', () => {
    const s = { ...withGraph(), load: { status: 'ready' as const, error: null } };
    expect(update(s, { type: 'GraphUnchanged' })).toBe(s);
  });

  it('GraphUnchanged clears loading without replacing graph state', () => {
    const s = { ...withGraph(), load: { status: 'loading' as const, error: null } };
    const next = update(s, { type: 'GraphUnchanged' });
    expect(next.graph).toBe(s.graph);
    expect(next.load).toEqual({ status: 'ready', error: null });
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
    expect(s.ui.rightCollapsed).toBe(false);
  });
  it('ZoomChanged sets the zoom level', () => {
    expect(update(withGraph(), { type: 'ZoomChanged', zoom: 0.5 }).ui.zoom).toBe(0.5);
  });

  it('ReviewViewChanged adopts the view default scope and grouping', () => {
    const reviewGraph: UIGraph = {
      ...withGraph().graph!,
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'all_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
        { id: 'runtime', title: 'Runtime', defaultScope: 'everything', groupBy: 'layer', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'directory', title: 'Directory', groups: [{ id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 }] },
        { id: 'layer', title: 'Layer', groups: [{ id: 'layer:api', title: 'API', componentIds: ['a'], componentCount: 1 }] },
      ],
    };
    const s = update(
      { ...withGraph(), graph: reviewGraph, ui: { ...initialState.ui, reviewGroupingId: 'directory', focusId: 'a', activeChangeId: 'c' } },
      { type: 'ReviewViewChanged', id: 'runtime' }
    );
    expect(s.ui.reviewScopeId).toBe('everything');
    expect(s.ui.reviewGroupingId).toBe('layer');
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('ReviewViewChanged uses saved scope and grouping defaults for that view', () => {
    const reviewGraph: UIGraph = {
      ...withGraph().graph!,
      reviewScopes: [
        { id: 'top_level_public_api', title: 'Top-level Public API' },
        { id: 'all_public_api', title: 'All Public API' },
        { id: 'everything', title: 'Everything' },
      ],
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'top_level_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
        { id: 'runtime', title: 'Runtime', defaultScope: 'everything', groupBy: 'layer', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'directory', title: 'Directory', groups: [{ id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 }] },
        { id: 'layer', title: 'Layer', groups: [{ id: 'layer:api', title: 'API', componentIds: ['a'], componentCount: 1 }] },
        { id: 'package_owner', title: 'Package Owner', groups: [{ id: 'owner:platform', title: 'Platform', componentIds: ['a'], componentCount: 1 }] },
      ],
    };
    const s = update(
      {
        ...withGraph(),
        graph: reviewGraph,
        ui: {
          ...initialState.ui,
          reviewGroupingId: 'directory',
          reviewDefaults: {
            reviewViewId: 'api',
            scopeByView: { runtime: 'all_public_api' },
            groupingByView: { runtime: 'package_owner' },
          },
        },
      },
      { type: 'ReviewViewChanged', id: 'runtime' }
    );
    expect(s.ui.reviewScopeId).toBe('all_public_api');
    expect(s.ui.reviewGroupingId).toBe('package_owner');
    expect(s.ui.reviewDefaults.reviewViewId).toBe('runtime');
  });

  it('ReviewDefaultsLoaded lets the view default override stale review slice grouping', () => {
    const reviewGraph: UIGraph = {
      ...withGraph().graph!,
      reviewViews: [
        { id: 'runtime', title: 'Runtime', defaultScope: 'everything', groupBy: 'configured_groups', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'review_view', title: 'Review View', groups: [{ id: 'review_view:runtime', title: 'Runtime', componentIds: ['a'], componentCount: 1 }] },
        { id: 'configured_groups', title: 'Categories', groups: [{ id: 'configured_groups:plugins', title: 'Plugins', componentIds: ['a'], componentCount: 1 }] },
      ],
    };
    const loaded = update(initialState, { type: 'GraphLoaded', graph: reviewGraph });
    const s = update(loaded, {
      type: 'ReviewDefaultsLoaded',
      key: 'repo-defaults',
      defaults: {
        reviewViewId: 'runtime',
        groupingByView: { runtime: 'review_view' },
      },
    });
    expect(s.ui.reviewGroupingId).toBe('configured_groups');
  });

  it('ReviewGroupingChanged switches grouping and clears focus', () => {
    const s = update(
      { ...withGraph(), ui: { ...initialState.ui, reviewGroupingId: 'directory', focusId: 'a', activeChangeId: 'c' } },
      { type: 'ReviewGroupingChanged', id: 'layer' }
    );
    expect(s.ui.reviewGroupingId).toBe('layer');
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('review filter events update filters and clear focus', () => {
    let s = update(
      { ...withGraph(), ui: { ...initialState.ui, focusId: 'a', activeChangeId: 'c' } },
      { type: 'ReviewImpactModeChanged', mode: 'changed_only' }
    );
    expect(s.ui.reviewImpactMode).toBe('changed_only');
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();

    s = update(
      { ...withGraph(), ui: { ...initialState.ui, focusId: 'a', activeChangeId: 'c' } },
      { type: 'ReviewChangeFilterChanged', filter: 'added' }
    );
    expect(s.ui.reviewChangeFilter).toBe('added');
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('UnchangedNeighborsToggled flips neighbor visibility and clears focus', () => {
    let s = update(
      { ...withGraph(), ui: { ...initialState.ui, focusId: 'a', activeChangeId: 'c' } },
      { type: 'UnchangedNeighborsToggled' }
    );
    expect(s.ui.hideUnchangedNeighbors).toBe(true);
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
    s = update(s, { type: 'UnchangedNeighborsToggled' });
    expect(s.ui.hideUnchangedNeighbors).toBe(false);
  });

  it('ChangedDetailsOnlyToggled flips package detail filtering and clears focus', () => {
    let s = update(
      { ...withGraph(), ui: { ...initialState.ui, focusId: 'a', activeChangeId: 'c' } },
      { type: 'ChangedDetailsOnlyToggled' }
    );
    expect(s.ui.changedDetailsOnly).toBe(false);
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
    s = update(s, { type: 'ChangedDetailsOnlyToggled' });
    expect(s.ui.changedDetailsOnly).toBe(true);
  });

  it('GroupLabelsToggled flips group label visibility', () => {
    let s = update(withGraph(), { type: 'GroupLabelsToggled' });
    expect(s.ui.showGroupLabels).toBe(false);
    s = update(s, { type: 'GroupLabelsToggled' });
    expect(s.ui.showGroupLabels).toBe(true);
  });

  it('CardDensityChanged switches card presentation density', () => {
    const s = update(withGraph(), { type: 'CardDensityChanged', density: 'compact' });
    expect(s.ui.cardDensity).toBe('compact');
  });

  it('InlineSignaturesToggled flips inline signature visibility', () => {
    let s = update(withGraph(), { type: 'InlineSignaturesToggled' });
    expect(s.ui.showInlineSignatures).toBe(false);
    s = update(s, { type: 'InlineSignaturesToggled' });
    expect(s.ui.showInlineSignatures).toBe(true);
  });

  it('review default load applies persisted valid view, scope, and grouping', () => {
    const reviewGraph: UIGraph = {
      ...withGraph().graph!,
      reviewScopes: [
        { id: 'top_level_public_api', title: 'Top-level Public API' },
        { id: 'all_public_api', title: 'All Public API' },
      ],
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'top_level_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
        { id: 'runtime', title: 'Runtime', defaultScope: 'top_level_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'directory', title: 'Directory', groups: [{ id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 }] },
        { id: 'package_owner', title: 'Package Owner', groups: [{ id: 'owner:platform', title: 'Platform', componentIds: ['a'], componentCount: 1 }] },
      ],
      defaultReviewView: 'api',
      defaultReviewScope: 'top_level_public_api',
      defaultGrouping: 'directory',
    };
    const loaded = update(initialState, { type: 'GraphLoaded', graph: reviewGraph });
    const s = update(loaded, {
      type: 'ReviewDefaultsLoaded',
      key: 'repo-defaults',
      defaults: {
        reviewViewId: 'runtime',
        scopeByView: { runtime: 'all_public_api' },
        groupingByView: { runtime: 'package_owner' },
      },
    });
    expect(s.ui.reviewDefaultsKey).toBe('repo-defaults');
    expect(s.ui.reviewViewId).toBe('runtime');
    expect(s.ui.reviewScopeId).toBe('all_public_api');
    expect(s.ui.reviewGroupingId).toBe('package_owner');
    expect(s.ui.focusId).toBeNull();
    expect(s.ui.activeChangeId).toBeNull();
  });

  it('layout pin events load, merge, and reset pinned positions', () => {
    let s = update(withGraph(), {
      type: 'LayoutPinsLoaded',
      scopeKey: 'scope-a',
      pins: { a: { x: 10, y: 20 } },
    });
    expect(s.ui.layoutPinScopeKey).toBe('scope-a');
    expect(s.ui.layoutPins).toEqual({ a: { x: 10, y: 20 } });

    s = update(s, { type: 'ComponentLayoutPinned', id: 'b', x: 30.4, y: 40.6 });
    expect(s.ui.layoutPins).toEqual({ a: { x: 10, y: 20 }, b: { x: 30, y: 41 } });

    s = update(s, { type: 'ComponentsLayoutPinned', pins: { c: { x: 50, y: 60 } } });
    expect(s.ui.layoutPins.c).toEqual({ x: 50, y: 60 });

    s = update(s, { type: 'LayoutPinReset', id: 'b' });
    expect(s.ui.layoutPins).not.toHaveProperty('b');
    expect(s.ui.layoutPins.a).toEqual({ x: 10, y: 20 });

    s = update(s, { type: 'ComponentsLayoutPinned', pins: { b: { x: 30, y: 40 }, d: { x: 70, y: 80 } } });
    s = update(s, { type: 'LayoutGroupPinsReset', componentIds: ['a', 'd'] });
    expect(s.ui.layoutPins).not.toHaveProperty('a');
    expect(s.ui.layoutPins).not.toHaveProperty('d');
    expect(s.ui.layoutPins.b).toEqual({ x: 30, y: 40 });

    s = update(s, { type: 'LayoutPinsReset' });
    expect(s.ui.layoutPins).toEqual({});

    s = update(s, { type: 'ComponentsLayoutPinned', pins: { a: { x: 10, y: 20 } } });
    s = update(s, { type: 'LayoutRepoPinsReset' });
    expect(s.ui.layoutPins).toEqual({});
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

  it('GraphLoaded initializes review view, scope, and grouping defaults', () => {
    const reviewGraph: UIGraph = {
      ...graph,
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'all_public_api', groupBy: 'layer', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'directory', title: 'Directory', groups: [{ id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 }] },
        { id: 'layer', title: 'Layer', groups: [{ id: 'layer:api', title: 'API', componentIds: ['a'], componentCount: 1 }] },
      ],
      defaultReviewView: 'api',
      defaultReviewScope: 'top_level_public_api',
      defaultGrouping: 'directory',
    };
    const s = update(initialState, { type: 'GraphLoaded', graph: reviewGraph });
    expect(s.ui.reviewViewId).toBe('api');
    expect(s.ui.reviewScopeId).toBe('top_level_public_api');
    expect(s.ui.reviewGroupingId).toBe('layer');
  });

  it('GraphLoaded preserves valid review selections during refresh', () => {
    const reviewGraph: UIGraph = {
      ...graph,
      reviewScopes: [
        { id: 'all_public_api', title: 'All Public API' },
        { id: 'everything', title: 'Everything' },
      ],
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'all_public_api', groupBy: 'directory', componentIds: ['a'], componentCount: 1 },
        { id: 'runtime', title: 'Runtime', defaultScope: 'everything', groupBy: 'layer', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'directory', title: 'Directory', groups: [{ id: 'directory:root', title: 'Root', componentIds: ['a'], componentCount: 1 }] },
        { id: 'layer', title: 'Layer', groups: [{ id: 'layer:runtime', title: 'Runtime', componentIds: ['a'], componentCount: 1 }] },
      ],
      defaultReviewView: 'api',
      defaultReviewScope: 'all_public_api',
      defaultGrouping: 'directory',
    };
    const s = update(
      {
        ...initialState,
        graph: reviewGraph,
        ui: {
          ...initialState.ui,
          reviewViewId: 'runtime',
          reviewScopeId: 'everything',
          reviewGroupingId: 'layer',
        },
      },
      { type: 'GraphLoaded', graph: reviewGraph }
    );
    expect(s.ui.reviewViewId).toBe('runtime');
    expect(s.ui.reviewScopeId).toBe('everything');
    expect(s.ui.reviewGroupingId).toBe('layer');
  });

  it('GraphLoaded applies review view initial expansion policy', () => {
    const reviewGraph: UIGraph = {
      ...graph,
      reviewViews: [
        { id: 'api', title: 'API', defaultScope: 'everything', defaultExpansion: 'collapsed', groupBy: 'layer', componentIds: ['a'], componentCount: 1 },
      ],
      reviewGroupings: [
        { id: 'layer', title: 'Layer', groups: [{ id: 'layer:api', title: 'API', componentIds: ['a'], componentCount: 1 }] },
      ],
      defaultReviewView: 'api',
      defaultGrouping: 'layer',
    };
    const s = update(initialState, { type: 'GraphLoaded', graph: reviewGraph });
    expect([...s.ui.expanded]).toEqual([]);
    expect([...s.ui.internalExpanded]).toEqual([]);
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

describe('update — comments slice', () => {
  it('CommentStarted opens a pending comment at the anchor', () => {
    const s = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 10, y: 20 } });
    expect(s.pendingComment).toEqual({ target: { type: 'component', id: 'a' }, x: 10, y: 20 });
  });

  it('CommentSubmitted appends a deterministically-id\'d marker, clears pending, activates it', () => {
    const started = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 10, y: 20 } });
    const s = update(started, { type: 'CommentSubmitted', text: 'hi' });
    expect(s.pendingComment).toBeNull();
    expect(s.markers).toHaveLength(1);
    expect(s.markers[0]).toMatchObject({ id: 'm-1', n: 1, x: 10, y: 12, body: 'hi', target: { type: 'component', id: 'a' } });
    expect(s.ui.activeMarkerId).toBe('m-1');
  });

  it('CommentSubmitted is a no-op when there is no pending comment', () => {
    const s = withGraph();
    expect(update(s, { type: 'CommentSubmitted', text: 'hi' })).toBe(s);
  });

  it('CommentCancelled clears pending', () => {
    const started = update(withGraph(), { type: 'CommentStarted', target: { type: 'component', id: 'a' }, anchor: { x: 1, y: 2 } });
    expect(update(started, { type: 'CommentCancelled' }).pendingComment).toBeNull();
  });

  it('MarkerActivated sets the active marker', () => {
    expect(update(withGraph(), { type: 'MarkerActivated', id: 'm-7' }).ui.activeMarkerId).toBe('m-7');
  });

  it('MarkersSeeded replaces the markers array', () => {
    const markers = [{ id: 'seed-0', n: 1, x: 1, y: 2, target: { type: 'component', id: 'a' }, body: 'b', author: '@you', when: '2m' }];
    const s = update(withGraph(), { type: 'MarkersSeeded', markers });
    expect(s.markers).toBe(markers);
  });
});
