import { afterEach, describe, expect, it, vi } from 'vitest';
import { callLens, fetchStatus, isLensPending } from './lens';

function okJson(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('readiness', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    window.history.pushState(null, '', '/');
  });

  it('reads the status endpoint rather than the tool surface', async () => {
    // The `status` MCP tool renders prose for an agent; a poll on a timer
    // needs fields, and they come from a route of their own.
    const asked: string[] = [];
    vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
      asked.push(String(input));
      return okJson({ ready: true, packages: 3 });
    });

    const got = await fetchStatus({ worktree: 'feature-x' });

    expect(asked).toEqual(['/w/feature-x/api/mcp/status']);
    expect(got).toEqual({ ready: true, packages: 3 });
  });

  it('recognises a cold daemon answering from the status endpoint', async () => {
    vi.stubGlobal('fetch', async () => okJson({ ready: false, status: 'loading', phase: 'parsing' }));
    expect(isLensPending(await fetchStatus())).toBe(true);
  });

  it('still refuses to read a lens result that is prose', async () => {
    // Every other tool renders text now. A caller asking for one through the
    // JSON transport should be told, not handed a parse error.
    vi.stubGlobal('fetch', async () =>
      okJson({ content: [{ type: 'text', text: 'search "x"  ·  2 hits\n' }] })
    );
    await expect(callLens('search', { query: 'x' }, {})).rejects.toThrow('returned text, not JSON');
  });
});
