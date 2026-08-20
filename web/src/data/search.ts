import type { RawAskHit } from '../domain/ask';

export interface SearchResponse {
  hits: RawAskHit[];
  dense: boolean;
}

/** One node of the daemon's answer, in its wire shape (snake_case). */
interface RawSearchHit {
  id: string;
  kind: string;
  package?: string;
  name?: string;
  file?: string;
  line?: number;
  doc?: string;
  score?: number;
  /** The query text matched this node; the rest is the region around it. */
  seed?: boolean;
  text_score?: number;
}

/**
 * Ask the daemon's retrieval index (POST /api/search) — the same operation the
 * MCP `search` tool runs, over the worktree the review is showing.
 *
 * The answer is the query's own hits *and* the community the diffusion grew
 * around them, and both are passed on: the canvas draws the whole answer, with
 * the seeds marked. A question about code is a question about wiring, and the
 * neighbours are what make the hits mean something.
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
  const data = (await res.json()) as { hits?: RawSearchHit[]; dense?: boolean };
  return { hits: (data.hits ?? []).map(toAskHit), dense: !!data.dense };
}

function toAskHit(hit: RawSearchHit): RawAskHit {
  return {
    node_id: hit.id,
    kind: hit.kind,
    package: hit.package,
    name: hit.name,
    file: hit.file,
    line: hit.line,
    doc: hit.doc,
    // A seed's rank is its text mass; a neighbour's is the diffusion mass it
    // collected. Both order within this one answer and nothing else.
    score: hit.seed ? hit.text_score ?? hit.score : hit.score,
    seed: !!hit.seed,
  };
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
