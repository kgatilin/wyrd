import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';
import { createViewportEffect } from './viewport';

const laid: UIGraph = {
  schema: 'wyrd.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 0, y: 0, w: 10, h: 10 }],
  edges: [],
  comments: [],
};
const withLaid = (): AppState => ({ ...initialState, geometry: { laid, status: 'ready', error: null } });
const focusedOn = (id: string) => (): AppState => ({
  ...withLaid(),
  ui: { ...initialState.ui, focusId: id, expanded: new Set([id]) },
});

function makePort(overrides: Partial<ViewportPort> = {}): ViewportPort {
  return {
    scrollToComponent: vi.fn(),
    focusComponent: vi.fn().mockReturnValue(null),
    fitZoom: vi.fn(),
    ...overrides,
  };
}

describe('createViewportEffect', () => {
  it('defers ChangeActivated scroll to the next LayoutComputed', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledTimes(1);
  });

  it('ChangeActivated centres without rescaling the canvas', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.focusComponent).not.toHaveBeenCalled();
  });

  it('defers TreeFocusRequested fit to the next LayoutComputed', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'TreeFocusRequested', target: { componentId: 'a' } }, withLaid, vi.fn());
    expect(port.focusComponent).not.toHaveBeenCalled();
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.focusComponent).toHaveBeenCalledWith('a', laid);
  });

  it('fits the selected component into view once its layout lands', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'ComponentSelected', id: 'a' }, focusedOn('a'), vi.fn());
    effect({ type: 'LayoutComputed', laid }, focusedOn('a'), vi.fn());
    expect(port.focusComponent).toHaveBeenCalledWith('a', laid);
  });

  it('does not fit when selecting clears the focus instead of setting it', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'ComponentSelected', id: 'a' }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.focusComponent).not.toHaveBeenCalled();
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('applies the zoom a fit asks for', () => {
    const port = makePort({ focusComponent: vi.fn().mockReturnValue(0.7) });
    const effect = createViewportEffect(port);
    const dispatch = vi.fn();
    effect({ type: 'ComponentToggled', id: 'a' }, focusedOn('a'), vi.fn());
    effect({ type: 'LayoutComputed', laid }, focusedOn('a'), dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledWith({ type: 'ZoomChanged', zoom: 0.7 });
  });

  it('does not dispatch a zoom when the fit kept the current one', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    const dispatch = vi.fn();
    effect({ type: 'ComponentToggled', id: 'a' }, focusedOn('a'), vi.fn());
    effect({ type: 'LayoutComputed', laid }, focusedOn('a'), dispatch as (e: Event) => void);
    expect(dispatch).not.toHaveBeenCalled();
  });

  it('ScrollToComponentRequested scrolls immediately', () => {
    const port = makePort();
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('does nothing before layout exists', () => {
    const port = makePort();
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, () => initialState, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('clears a pending scroll on LayoutFailed (no scroll against stale geometry)', () => {
    const port = makePort();
    const effect = createViewportEffect(port);
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    effect({ type: 'LayoutFailed', error: 'boom' }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('on ZoomFitRequested, dispatches ZoomChanged with the fit zoom', () => {
    const port = makePort({ fitZoom: vi.fn().mockReturnValue(0.5) });
    const dispatch = vi.fn();
    createViewportEffect(port)({ type: 'ZoomFitRequested' }, withLaid, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledWith({ type: 'ZoomChanged', zoom: 0.5 });
  });
});
