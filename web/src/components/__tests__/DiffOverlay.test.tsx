import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DiffOverlay } from '../DiffOverlay';
import type { GitDiff } from '../../domain/gitDiff';

const diff: GitDiff = {
  schema: 'archai.gitdiff/1',
  worktree: 'feature',
  branch: 'feature',
  baseRef: 'main',
  baseRev: 'abc1234',
  stats: { files: 3, insertions: 9, deletions: 2 },
  files: [
    {
      path: 'internal/adapter/git/diff.go',
      status: 'A',
      insertions: 4,
      deletions: 0,
      patch: ['diff --git a/internal/adapter/git/diff.go b/internal/adapter/git/diff.go', '@@ -0,0 +1,2 @@', '+package git', '+'].join('\n'),
    },
    {
      path: 'internal/adapter/http/api.go',
      status: 'M',
      insertions: 5,
      deletions: 2,
      patch: ['@@ -3,3 +3,3 @@ func routes() {', ' 	mux := http.NewServeMux()', '-	mux.Handle("/old", old)', '+	mux.Handle("/new", next)'].join('\n'),
    },
    { path: 'assets/logo.png', status: 'M', insertions: 0, deletions: 0, binary: true },
  ],
};

function stubFetch(payload: GitDiff = diff) {
  const fetchMock = vi.fn(async (_url: string) => new Response(JSON.stringify(payload), { status: 200 }));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('DiffOverlay', () => {
  it('asks the daemon for the active worktree diff against the review base', async () => {
    const fetchMock = stubFetch();
    render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(fetchMock.mock.calls[0][0]).toBe('/w/feature/api/gitdiff?base=main');
  });

  it('names the working tree when the branch under review is the base itself', async () => {
    stubFetch({ ...diff, branch: 'main' });
    const { container } = render(<DiffOverlay worktree="archai" baseRef="main" onClose={() => {}} />);
    await waitFor(() =>
      expect(container.querySelector('.hf-diff-compare .branch')?.textContent).toBe('working tree')
    );
  });

  it('names the merge-base revision the diff starts from', async () => {
    stubFetch();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    await waitFor(() => expect(container.querySelector('.hf-diff-compare')?.textContent).toContain('main@abc1234'));
  });

  it('sections the file list by package and shows per-group stats', async () => {
    stubFetch();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    await screen.findByText('internal/adapter/git');

    const groups = [...container.querySelectorAll('.hf-diff-group-head .label')].map((n) => n.textContent);
    expect(groups).toEqual(['assets', 'internal/adapter/git', 'internal/adapter/http']);

    const httpGroup = screen.getByText('internal/adapter/http').closest('.hf-diff-group-head')!;
    expect(within(httpGroup as HTMLElement).getByText('+5')).toBeTruthy();
  });

  it('renders the first file by default and switches on click', async () => {
    stubFetch();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    // Groups sort alphabetically, so the binary asset is selected first.
    await screen.findByText('Binary file — no textual diff.');

    fireEvent.click(screen.getByText('diff.go'));
    await waitFor(() => expect(container.querySelector('.hf-diff-filehead .path')?.textContent).toBe(
      'internal/adapter/git/diff.go'
    ));
    expect(container.querySelector('.hf-diff-line.add .text')?.textContent).toBe('package git');
  });

  it('numbers both sides of a modification and keeps the marker out of the text', async () => {
    stubFetch();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    await screen.findByText('api.go');
    fireEvent.click(screen.getByText('api.go'));

    await waitFor(() => expect(container.querySelectorAll('.hf-diff-line.del').length).toBe(1));
    const deleted = container.querySelector('.hf-diff-line.del')!;
    expect(deleted.querySelector('.text')!.textContent).toBe('\tmux.Handle("/old", old)');
    // Old line 4 deleted, new line 4 added: each side numbers independently.
    expect([...deleted.querySelectorAll('.ln')].map((n) => n.textContent)).toEqual(['4', '']);
    const added = container.querySelector('.hf-diff-line.add')!;
    expect([...added.querySelectorAll('.ln')].map((n) => n.textContent)).toEqual(['', '4']);
  });

  it('walks files with j/k and closes on Escape', async () => {
    stubFetch();
    const onClose = vi.fn();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={onClose} />);
    await screen.findByText('diff.go');

    fireEvent.keyDown(window, { key: 'j' });
    await waitFor(() => expect(container.querySelector('.hf-diff-file.active .name')?.textContent).toBe('diff.go'));
    fireEvent.keyDown(window, { key: 'j' });
    await waitFor(() => expect(container.querySelector('.hf-diff-file.active .name')?.textContent).toBe('api.go'));
    fireEvent.keyDown(window, { key: 'k' });
    await waitFor(() => expect(container.querySelector('.hf-diff-file.active .name')?.textContent).toBe('diff.go'));

    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('collapses a section without losing the selection', async () => {
    stubFetch();
    const { container } = render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    await screen.findByText('diff.go');

    fireEvent.click(screen.getByText('internal/adapter/git'));
    await waitFor(() => expect(screen.queryByText('diff.go')).toBeNull());
    expect(container.querySelector('.hf-diff-filehead')).toBeTruthy();
  });

  it('reports an empty diff rather than an empty pane', async () => {
    stubFetch({ ...diff, files: [], stats: { files: 0, insertions: 0, deletions: 0 } });
    render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    expect(await screen.findByText('No file changes against main.')).toBeTruthy();
  });

  it('surfaces a server error', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string) => new Response('not a git repository', { status: 500 })));
    render(<DiffOverlay worktree="feature" baseRef="main" onClose={() => {}} />);
    expect(await screen.findByText('not a git repository')).toBeTruthy();
  });
});
