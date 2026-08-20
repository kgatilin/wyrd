import type { ArchReport } from '../domain/archReport';

/**
 * Read the architecture review report for a worktree against the review base.
 *
 * The daemon builds it from scratch per request — both package models, an
 * archmotif graph over each, the model diff and the git hunks — and sends it
 * back without an ETag. Whoever calls this is the cache.
 */
export async function fetchArchReport(worktree: string, baseRef: string): Promise<ArchReport> {
  const res = await fetch(archReportURL(worktree, baseRef));
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
