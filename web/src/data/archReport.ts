import type { ArchReport } from '../domain/archReport';

/**
 * Read the architecture review report for a worktree against the review base.
 *
 * The daemon keeps one built report per worktree and base, warmed when the
 * worktree's model finishes parsing and dropped on the same model change the
 * SSE stream announces. `fresh` asks it to rebuild instead: the daemon has no
 * event for a change that leaves the model alone — an edited comment, a base
 * branch that moved underneath — so the reviewer's own refresh is what reaches
 * past its cache. The response carries no ETag, so this client is still a cache
 * in its own right.
 */
export async function fetchArchReport(
  worktree: string,
  baseRef: string,
  fresh = false
): Promise<ArchReport> {
  const res = await fetch(archReportURL(worktree, baseRef), {
    headers: fresh ? { 'Cache-Control': 'no-cache' } : undefined,
  });
  if (!res.ok) {
    const message = await res.text();
    throw new Error(message.trim() || `HTTP ${res.status}`);
  }
  return (await res.json()) as ArchReport;
}

function archReportURL(worktree: string, baseRef: string): string {
  const query = baseRef ? `?base=${encodeURIComponent(baseRef)}` : '';
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/archmotif/report${query}`;
  return `${currentWorktreePrefix()}/api/archmotif/report${query}`;
}

function currentWorktreePrefix(): string {
  if (typeof window === 'undefined') return '';
  const match = window.location.pathname.match(/^\/w\/[^/]+/);
  return match ? match[0] : '';
}
