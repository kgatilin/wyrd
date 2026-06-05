import type { NavigationPort } from '../domain/ports';

/** NavigationPort backed by browser history. */
export function createBrowserNavigation(): NavigationPort {
  return {
    focusWorktree(name: string) {
      if (typeof window === 'undefined' || name === '') return;
      const next = worktreeReviewURL(name, window.location.search, window.location.hash);
      const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
      if (current !== next) {
        window.history.pushState(null, '', next);
      }
    },
  };
}

export function worktreeReviewURL(name: string, search = '', hash = ''): string {
  return `/w/${encodeURIComponent(name)}/review/${search}${hash}`;
}
