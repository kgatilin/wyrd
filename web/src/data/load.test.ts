import { afterEach, describe, expect, it, vi } from 'vitest';
import type { UIGraph } from '../types';
import { loadGraph } from './load';

const graph: UIGraph = {
  schema: 'wyrd.uigraph/v0',
  boundedContexts: [],
  components: [],
  edges: [],
  comments: [],
};

function okGraph(): Response {
  return new Response(JSON.stringify(graph), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('loadGraph', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    window.history.pushState(null, '', '/');
  });

  it('tries the live daemon endpoint before static export fallbacks', async () => {
    const fetchMock = vi.fn(async () => okGraph());
    vi.stubGlobal('fetch', fetchMock);

    await loadGraph();

    expect(fetchMock).toHaveBeenCalledWith('/api/uigraph', undefined);
  });

  it('uses the current /w/<worktree> prefix when the app is scoped there', async () => {
    window.history.pushState(null, '', '/w/beta/');
    const fetchMock = vi.fn(async () => okGraph());
    vi.stubGlobal('fetch', fetchMock);

    await loadGraph();

    expect(fetchMock).toHaveBeenCalledWith('/w/beta/api/uigraph', undefined);
  });

  it('loads an explicit worktree without falling back to unrelated static JSON', async () => {
    const fetchMock = vi.fn(async () => okGraph());
    vi.stubGlobal('fetch', fetchMock);

    await loadGraph('feature');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith('/w/feature/api/uigraph', undefined);
  });

  it('asks the daemon to re-read the worktree on a fresh load', async () => {
    const fetchMock = vi.fn(async () => okGraph());
    vi.stubGlobal('fetch', fetchMock);

    await loadGraph('feature', true);

    expect(fetchMock).toHaveBeenCalledWith('/w/feature/api/uigraph', {
      headers: { 'Cache-Control': 'no-cache' },
    });
  });
});
