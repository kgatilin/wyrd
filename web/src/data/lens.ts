/**
 * The MCP tool endpoint the daemon already exposes to its thin client
 * (`POST /w/<worktree>/api/mcp/tools/call`), used from the review UI. The
 * analysis lenses are the daemon's, not the browser's: re-deriving a
 * clustering client-side would be a second implementation of the same maths,
 * so the UI calls the same tool an agent calls.
 */

/** The MCP `ToolResult` envelope, verbatim off the wire. */
interface ToolResult {
  content?: { type?: string; text?: string }[];
  isError?: boolean;
}

/**
 * A lens answer the daemon returned instead of the payload because it is not
 * ready yet. `loading` means the model is still being parsed; `indexing` means
 * the dense-embedding pass is still running. Both are states to wait through,
 * not failures — the daemon returns them precisely so a caller need not block.
 */
export interface LensPending {
  status: 'loading' | 'indexing';
  phase?: string;
  embedded?: number;
  embeddable?: number;
  message?: string;
}

/** A tool-level failure the daemon reported (`isError`), not a transport one. */
export class LensError extends Error {}

/**
 * Call one MCP tool and return its parsed payload. Handlers answer with a
 * single JSON text block, so the envelope is unwrapped here and nowhere else.
 */
export async function callLens(
  name: string,
  args: Record<string, unknown>,
  options: { worktree?: string } = {}
): Promise<unknown> {
  const res = await fetch(`${apiPrefix(options.worktree)}/api/mcp/tools/call`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ name, arguments: args }),
  });
  if (!res.ok) {
    throw new Error(await errorText(res, name));
  }
  const result = (await res.json()) as ToolResult;
  const text = result?.content?.[0]?.text ?? '';
  if (result?.isError) {
    throw new LensError(text || `${name} failed`);
  }
  if (text === '') return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    // A lens that renders for a reader rather than a parser. Nothing in the UI
    // asks for one, so say which tool did it instead of guessing at the text.
    throw new Error(`${name} returned text, not JSON`);
  }
}

/** True when a payload is the daemon saying "not ready yet" rather than data. */
export function isLensPending(payload: unknown): payload is LensPending {
  if (payload == null || typeof payload !== 'object') return false;
  const status = (payload as { status?: unknown }).status;
  return status === 'loading' || status === 'indexing';
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

async function errorText(res: Response, name: string): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body?.error) return body.error;
  } catch {
    // fall through to the status line
  }
  return `${name} failed (${res.status})`;
}
