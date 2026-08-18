import type { GitDiff } from '../domain/gitDiff';

/**
 * Read the file-level diff of a worktree against the review base.
 *
 * The daemon recomputes this from scratch on every call — about ten git
 * invocations, including a full `git diff` whose patches are the bulk of
 * the payload — and sends it back without an ETag, so nothing between here
 * and the repository caches it. Whoever calls this is the cache.
 */
export async function fetchGitDiff(worktree: string, baseRef: string): Promise<GitDiff> {
  const res = await fetch(gitDiffURL(worktree, baseRef));
  if (!res.ok) {
    const message = await res.text();
    throw new Error(message.trim() || `HTTP ${res.status}`);
  }
  return (await res.json()) as GitDiff;
}

function gitDiffURL(worktree: string, baseRef: string): string {
  const query = baseRef ? `?base=${encodeURIComponent(baseRef)}` : '';
  if (worktree) return `/w/${encodeURIComponent(worktree)}/api/gitdiff${query}`;
  return `${currentWorktreePrefix()}/api/gitdiff${query}`;
}

function currentWorktreePrefix(): string {
  if (typeof window === 'undefined') return '';
  const match = window.location.pathname.match(/^\/w\/[^/]+/);
  return match ? match[0] : '';
}
