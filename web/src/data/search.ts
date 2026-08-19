import type { RawAskHit } from '../domain/ask';

export interface SearchResponse {
  hits: RawAskHit[];
  dense: boolean;
}

/**
 * Ask the daemon's retrieval index (POST /api/search) — the same hybrid
 * dense+BM25 search the MCP `search` tool runs, over the worktree the review
 * is showing.
 */
export async function searchSymbols(
  query: string,
  options: { k: number; worktree?: string }
): Promise<SearchResponse> {
  const res = await fetch(`${apiPrefix(options.worktree)}/api/search`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ query, k: options.k }),
  });
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  const data = (await res.json()) as { results?: RawAskHit[]; dense?: boolean };
  return { hits: data.results ?? [], dense: !!data.dense };
}

function apiPrefix(worktree?: string): string {
  if (worktree) return `/w/${encodeURIComponent(worktree)}`;
  return currentWorktreePrefix() ?? '';
}

function currentWorktreePrefix(): string | null {
  if (typeof window === 'undefined') return null;
  const match = window.location.pathname.match(/^\/w\/([^/]+)(?:\/|$)/);
  if (!match) return null;
  return `/w/${match[1]}`;
}

async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body?.error) return body.error;
  } catch {
    // fall through to the status line
  }
  return `search failed (${res.status})`;
}
