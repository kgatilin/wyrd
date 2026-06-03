import { describe, it, expect, vi } from 'vitest';
import type { UIGraph } from '../types';
import { initialState, type AppState } from '../domain/state';
import type { Event } from '../domain/events';
import type { ViewportPort } from '../domain/ports';
import { createViewportEffect } from './viewport';

const laid: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  components: [{ id: 'a', name: 'A', tech: '', desc: '', bc: 'bc1', internals: [], ports: [], x: 0, y: 0, w: 10, h: 10 }],
  edges: [],
  comments: [],
};
const withLaid = (): AppState => ({ ...initialState, geometry: { laid, status: 'ready', error: null } });

describe('createViewportEffect', () => {
  it('defers ChangeActivated scroll to the next LayoutComputed', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledTimes(1);
  });

  it('defers TreeFocusRequested scroll to the next LayoutComputed', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    effect({ type: 'TreeFocusRequested', target: { componentId: 'a' } }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('ScrollToComponentRequested scrolls immediately', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, withLaid, vi.fn());
    expect(port.scrollToComponent).toHaveBeenCalledWith('a', laid);
  });

  it('does nothing before layout exists', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    createViewportEffect(port)({ type: 'ScrollToComponentRequested', id: 'a' }, () => initialState, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('clears a pending scroll on LayoutFailed (no scroll against stale geometry)', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn() };
    const effect = createViewportEffect(port);
    effect({ type: 'ChangeActivated', change: { id: 'c', kind: 'added', name: '', where: '', cmp: 'a' } }, withLaid, vi.fn());
    effect({ type: 'LayoutFailed', error: 'boom' }, withLaid, vi.fn());
    effect({ type: 'LayoutComputed', laid }, withLaid, vi.fn());
    expect(port.scrollToComponent).not.toHaveBeenCalled();
  });

  it('on ZoomFitRequested, dispatches ZoomChanged with the fit zoom', () => {
    const port: ViewportPort = { scrollToComponent: vi.fn(), fitZoom: vi.fn().mockReturnValue(0.5) };
    const dispatch = vi.fn();
    createViewportEffect(port)({ type: 'ZoomFitRequested' }, withLaid, dispatch as (e: Event) => void);
    expect(dispatch).toHaveBeenCalledWith({ type: 'ZoomChanged', zoom: 0.5 });
  });
});
