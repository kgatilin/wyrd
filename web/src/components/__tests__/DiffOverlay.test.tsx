import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { DiffOverlay, useDiffSession } from '../DiffOverlay';
import type { GitDiff } from '../../domain/gitDiff';
import type { UIGraph } from '../../types';

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

/** Two packages, one calling the other, declared in the diffed files. */
const graph: UIGraph = {
  schema: 'archai.uigraph/v0',
  boundedContexts: [],
  comments: [],
  edges: [],
  components: [
    {
      id: 'internal/adapter/http',
      name: 'http',
      tech: 'Go',
      desc: '',
      bc: 'root',
      ports: [],
      internals: [
        {
          id: 'internal/adapter/http.Handle',
          kind: 'func',
          name: 'Handle',
          exported: true,
          sourceFile: 'api.go',
          members: [],
        },
      ],
    },
    {
      id: 'internal/serve',
      name: 'serve',
      tech: 'Go',
      desc: '',
      bc: 'root',
      ports: [],
      internals: [
        { id: 'internal/serve.Serve', kind: 'func', name: 'Serve', exported: true, sourceFile: 'serve.go', members: [] },
      ],
    },
  ],
  relations: [
    {
      id: 'r:calls',
      kind: 'calls',
      fromComponentId: 'internal/serve',
      fromInternalId: 'internal/serve.Serve',
      toComponentId: 'internal/adapter/http',
      toInternalId: 'internal/adapter/http.Handle',
      toLabel: 'Handle',
    },
  ],
};

/**
 * The overlay is a view over a session the app owns, so the tests drive it
 * the way the app does: through the hook, with `open` toggling the mount.
 */
function Harness({
  worktree = 'feature',
  baseRef = 'main',
  open = true,
  onClose = () => {},
}: {
  worktree?: string;
  baseRef?: string;
  open?: boolean;
  onClose?: () => void;
}) {
  const session = useDiffSession(worktree, baseRef, open);
  if (!open) return null;
  return (
    <DiffOverlay session={session} graph={graph} worktree={worktree} baseRef={baseRef} onClose={onClose} />
  );
}

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
    render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(fetchMock.mock.calls[0][0]).toBe('/w/feature/api/gitdiff?base=main');
  });

  it('names the working tree when the branch under review is the base itself', async () => {
    stubFetch({ ...diff, branch: 'main' });
    const { container } = render(<Harness worktree="archai" baseRef="main" onClose={() => {}} />);
    await waitFor(() =>
      expect(container.querySelector('.hf-diff-compare .branch')?.textContent).toBe('working tree')
    );
  });

  it('names the merge-base revision the diff starts from', async () => {
    stubFetch();
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    await waitFor(() => expect(container.querySelector('.hf-diff-compare')?.textContent).toContain('main@abc1234'));
  });

  it('sections the file list by package and shows per-group stats', async () => {
    stubFetch();
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    await screen.findByText('internal/adapter/git');

    const groups = [...container.querySelectorAll('.hf-diff-group-head .label')].map((n) => n.textContent);
    expect(groups).toEqual(['assets', 'internal/adapter/git', 'internal/adapter/http']);

    const httpGroup = screen.getByText('internal/adapter/http').closest('.hf-diff-group-head')!;
    expect(within(httpGroup as HTMLElement).getByText('+5')).toBeTruthy();
  });

  it('renders the first file by default and switches on click', async () => {
    stubFetch();
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
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
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
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
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={onClose} />);
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
    const { container } = render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    await screen.findByText('diff.go');

    fireEvent.click(screen.getByText('internal/adapter/git'));
    await waitFor(() => expect(screen.queryByText('diff.go')).toBeNull());
    expect(container.querySelector('.hf-diff-filehead')).toBeTruthy();
  });

  it('reports an empty diff rather than an empty pane', async () => {
    stubFetch({ ...diff, files: [], stats: { files: 0, insertions: 0, deletions: 0 } });
    render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    expect(await screen.findByText('No file changes against main.')).toBeTruthy();
  });

  it('surfaces a server error', async () => {
    vi.stubGlobal('fetch', vi.fn(async (_url: string) => new Response('not a git repository', { status: 500 })));
    render(<Harness worktree="feature" baseRef="main" onClose={() => {}} />);
    expect(await screen.findByText('not a git repository')).toBeTruthy();
  });

  it('keeps the diff and the reading position across a close and a reopen', async () => {
    const fetchMock = stubFetch();
    const { container, rerender } = render(<Harness />);
    await screen.findByText('api.go');
    fireEvent.click(screen.getByText('api.go'));
    await waitFor(() =>
      expect(container.querySelector('.hf-diff-filehead .path')?.textContent).toBe(
        'internal/adapter/http/api.go'
      )
    );

    rerender(<Harness open={false} />);
    expect(container.querySelector('.hf-diff-overlay')).toBeNull();
    rerender(<Harness />);

    // Back on the same file, with no second read of the working tree.
    expect(container.querySelector('.hf-diff-filehead .path')?.textContent).toBe(
      'internal/adapter/http/api.go'
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('re-reads when the reviewed worktree changes', async () => {
    const fetchMock = stubFetch();
    const { rerender } = render(<Harness worktree="feature" />);
    await screen.findByText('api.go');

    rerender(<Harness worktree="other" />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock.mock.calls[1][0]).toBe('/w/other/api/gitdiff?base=main');
  });

  it('re-reads on demand', async () => {
    const fetchMock = stubFetch();
    render(<Harness />);
    await screen.findByText('api.go');

    fireEvent.click(screen.getByText('Reload'));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it('opens the wiring of a symbol clicked in the patch', async () => {
    stubFetch();
    const { container } = render(<Harness />);
    await screen.findByText('api.go');
    fireEvent.click(screen.getByText('api.go'));
    await waitFor(() =>
      expect(container.querySelector('.hf-diff-filehead .path')?.textContent).toBe(
        'internal/adapter/http/api.go'
      )
    );

    // `Handle` is declared in this very file, so the patch marks it.
    const marked = container.querySelectorAll('.hf-diff-view .hf-code-sym[data-sym="Handle"]');
    expect(marked.length).toBeGreaterThan(0);
    // `mux` is not a symbol the graph knows — nothing to open, nothing marked.
    expect(container.querySelector('.hf-code-sym[data-sym="mux"]')).toBeNull();

    fireEvent.click(marked[0]);
    const panel = container.querySelector('.hf-symbol-overlay');
    expect(panel).toBeTruthy();
    expect(panel?.querySelector('.hf-symbol-title')?.textContent).toContain('Handle');
    // The caller lives in another package, which is the whole point.
    expect(within(panel as HTMLElement).getByText('Serve')).toBeTruthy();
    expect(panel?.querySelector('.hf-symbol-group.cross')).toBeTruthy();
  });

  it('lets Escape dismiss the wiring panel without closing the diff', async () => {
    stubFetch();
    const onClose = vi.fn();
    const { container } = render(<Harness onClose={onClose} />);
    await screen.findByText('api.go');
    fireEvent.click(screen.getByText('api.go'));
    await waitFor(() => expect(container.querySelector('.hf-code-sym[data-sym="Handle"]')).toBeTruthy());

    fireEvent.click(container.querySelector('.hf-code-sym[data-sym="Handle"]') as HTMLElement);
    expect(container.querySelector('.hf-symbol-overlay')).toBeTruthy();

    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(container.querySelector('.hf-symbol-overlay')).toBeNull());
    expect(onClose).not.toHaveBeenCalled();

    // With the panel gone the diff takes the keyboard back.
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('does not cache a failed read', async () => {
    const fetchMock = vi.fn(async (_url: string) => new Response('daemon is gone', { status: 500 }));
    vi.stubGlobal('fetch', fetchMock);
    const { rerender } = render(<Harness />);
    await screen.findByText('daemon is gone');

    rerender(<Harness open={false} />);
    rerender(<Harness />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });
});
