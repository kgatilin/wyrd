import type { SymbolDefinition } from '../domain/symbolDefinition';

/** One node of the daemon's retrieval graph, in its wire shape (snake_case). */
interface RawSymbolDefinition {
  node_id?: string;
  kind?: string;
  package?: string;
  name?: string;
  file?: string;
  line?: number;
  signature?: string;
  doc?: string;
  body?: string;
}

/**
 * Read one symbol's declaration from the daemon (GET /api/node/{id}) — the
 * same node the MCP `get_node` tool returns, body read from the span the
 * indexer recorded.
 *
 * A node the graph does not have comes back as null rather than as an error:
 * a field and an interface method are spans inside their type, not nodes of
 * their own, and "no such node" is a state the caller acts on (it asks for the
 * declaring type next), not a failure to report.
 */
export async function fetchSymbolDefinition(
  nodeId: string,
  worktree: string
): Promise<SymbolDefinition | null> {
  const res = await fetch(nodeURL(nodeId, worktree));
  if (res.status === 404) return null;
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  const raw = (await res.json()) as RawSymbolDefinition | null;
  if (!raw?.node_id) return null;
  return {
    nodeId: raw.node_id,
    kind: raw.kind ?? '',
    packageName: raw.package ?? '',
    name: raw.name ?? '',
    file: raw.file ?? '',
    line: raw.line ?? 0,
    signature: raw.signature?.trim() || undefined,
    // The indexer keeps the doc comment's own trailing newline; a block with
    // one is a blank line the reader has to look past.
    doc: raw.doc?.trimEnd() || undefined,
    body: raw.body || undefined,
  };
}

function nodeURL(nodeId: string, worktree: string): string {
  // A node id carries the package path, so it is full of slashes. They are
  // escaped rather than passed through: the daemon reads the id as everything
  // after the prefix and decodes it, while an unescaped `#` or `?` in a
  // generic parameter would be read as a fragment or a query.
  const id = encodeURIComponent(nodeId);
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/node/${id}`;
  return `${currentWorktreePrefix()}/api/node/${id}`;
}

function currentWorktreePrefix(): string {
  if (typeof window === 'undefined') return '';
  const match = window.location.pathname.match(/^\/w\/[^/]+/);
  return match ? match[0] : '';
}

async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body?.error) return body.error;
  } catch {
    // fall through to the status line
  }
  return `definition read failed (${res.status})`;
}
