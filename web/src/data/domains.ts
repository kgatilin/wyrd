import type { ArchMotifScopeInput, RawLatentDomains } from '../domain/archMotifDomains';
import { domainsQueryForScope } from '../domain/archMotifDomains';

/**
 * The domains partition, from the daemon's own endpoint rather than through the
 * MCP tool surface next door in `lens.ts`.
 *
 * The tool endpoint clamps every result at 256 KiB, which is there to protect
 * an agent's context window and the NATS bridge behind it. This grid needs the
 * whole partition of every analysed symbol — both sides of it — and on a real
 * repository that is larger than the clamp however tightly it is encoded, so
 * asking for it through the agent transport comes back as a refusal. A browser
 * fetching the data for the page it is rendering is neither of those callers,
 * so it reads the partition directly.
 */
export async function fetchDomains(
  scope: ArchMotifScopeInput,
  options: { worktree?: string } = {}
): Promise<RawLatentDomains> {
  const query = new URLSearchParams(domainsQueryForScope(scope));
  const res = await fetch(`${apiPrefix(options.worktree)}/api/archmotif/domains?${query}`);
  if (!res.ok) {
    throw new Error(await errorText(res));
  }
  return (await res.json()) as RawLatentDomains;
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

/**
 * The endpoint answers a failed analysis with the reason as plain text — an
 * empty scope, a missing review base, too few embedded symbols — so the text is
 * the message, and the status line is only the fallback.
 */
async function errorText(res: Response): Promise<string> {
  try {
    const body = (await res.text()).trim();
    if (body) return body;
  } catch {
    // fall through to the status line
  }
  return `domains failed (${res.status})`;
}
