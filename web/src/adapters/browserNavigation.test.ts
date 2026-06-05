import { afterEach, describe, expect, it } from 'vitest';
import { createBrowserNavigation, worktreeReviewURL } from './browserNavigation';

describe('worktreeReviewURL', () => {
  it('builds the review URL for a worktree and preserves query/hash', () => {
    expect(worktreeReviewURL('feature one', '?base=main', '#changes')).toBe('/w/feature%20one/review/?base=main#changes');
  });
});

describe('createBrowserNavigation', () => {
  afterEach(() => {
    window.history.pushState(null, '', '/');
  });

  it('pushes the worktree-scoped review URL', () => {
    window.history.pushState(null, '', '/w/old/review/?base=main#changes');

    createBrowserNavigation().focusWorktree('new');

    expect(window.location.pathname).toBe('/w/new/review/');
    expect(window.location.search).toBe('?base=main');
    expect(window.location.hash).toBe('#changes');
  });
});
