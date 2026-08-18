import { describe, expect, it } from 'vitest';
import { fileLabel, groupFiles, parseHunks, statusLabel, type DiffFile } from './gitDiff';

const patch = [
  'diff --git a/internal/serve/state.go b/internal/serve/state.go',
  'index 1111111..2222222 100644',
  '--- a/internal/serve/state.go',
  '+++ b/internal/serve/state.go',
  '@@ -10,6 +10,7 @@ func (s *State) Load() error {',
  ' 	s.mu.Lock()',
  '-	old := s.packages',
  '+	next := s.packages',
  '+	s.dirty = true',
  ' 	return nil',
  '@@ -40,2 +41,2 @@ func (s *State) Close() error {',
  '-	return nil',
  '+	return s.bus.Close()',
  '',
].join('\n');

function file(overrides: Partial<DiffFile> & { path: string }): DiffFile {
  return { status: 'M', insertions: 0, deletions: 0, ...overrides };
}

describe('parseHunks', () => {
  it('drops the file preamble and keeps one entry per hunk', () => {
    const hunks = parseHunks(patch);
    expect(hunks).toHaveLength(2);
    expect(hunks[0].section).toBe('func (s *State) Load() error {');
    expect(hunks[0].lines.map((l) => l.kind)).toEqual(['context', 'del', 'add', 'add', 'context']);
  });

  it('numbers each side independently', () => {
    const [hunk] = parseHunks(patch);
    // Context at the top of the hunk is line 10 on both sides.
    expect(hunk.lines[0]).toMatchObject({ oldNumber: 10, newNumber: 10 });
    // The deletion consumes an old line only, the additions new lines only.
    expect(hunk.lines[1]).toMatchObject({ kind: 'del', oldNumber: 11 });
    expect(hunk.lines[1].newNumber).toBeUndefined();
    expect(hunk.lines[2]).toMatchObject({ kind: 'add', newNumber: 11 });
    expect(hunk.lines[2].oldNumber).toBeUndefined();
    expect(hunk.lines[3]).toMatchObject({ kind: 'add', newNumber: 12 });
    // ...so the trailing context is old 12 / new 13.
    expect(hunk.lines[4]).toMatchObject({ kind: 'context', oldNumber: 12, newNumber: 13 });
  });

  it('strips the marker but preserves indentation', () => {
    const [hunk] = parseHunks(patch);
    expect(hunk.lines[2].text).toBe('\tnext := s.packages');
  });

  it('reads a no-newline marker as metadata, not as content', () => {
    const hunks = parseHunks(['@@ -1 +1 @@', '-a', '\\ No newline at end of file', '+b'].join('\n'));
    expect(hunks[0].lines.map((l) => l.kind)).toEqual(['del', 'meta', 'add']);
    // The marker must not consume a line number.
    expect(hunks[0].lines[2]).toMatchObject({ newNumber: 1 });
  });

  it('returns nothing for an absent or empty patch', () => {
    expect(parseHunks(undefined)).toEqual([]);
    expect(parseHunks('')).toEqual([]);
    // Binary files have a header but no hunks.
    expect(parseHunks('diff --git a/x.png b/x.png\nBinary files differ\n')).toEqual([]);
  });
});

describe('groupFiles', () => {
  const files = [
    file({ path: 'internal/adapter/http/api.go', insertions: 10, deletions: 2 }),
    file({ path: 'internal/adapter/git/diff.go', insertions: 5, deletions: 0, status: 'A' }),
    file({ path: 'internal/serve/state.go', insertions: 1, deletions: 1 }),
    file({ path: 'README.md', insertions: 3, deletions: 3 }),
  ];

  it('sections by directory in package mode and sums the stats', () => {
    const groups = groupFiles(files, 'package');
    expect(groups.map((g) => g.key)).toEqual([
      '(root)',
      'internal/adapter/git',
      'internal/adapter/http',
      'internal/serve',
    ]);
    expect(groups[1]).toMatchObject({ insertions: 5, deletions: 0 });
  });

  it('collapses to top-level areas, keeping two segments under container roots', () => {
    const groups = groupFiles(files, 'toplevel');
    expect(groups.map((g) => g.key)).toEqual(['(root)', 'internal/adapter', 'internal/serve']);
    const adapters = groups[1];
    expect(adapters.files.map((f) => f.path)).toEqual([
      'internal/adapter/git/diff.go',
      'internal/adapter/http/api.go',
    ]);
    expect(adapters).toMatchObject({ insertions: 15, deletions: 2 });
  });

  it('puts everything in one section in flat mode', () => {
    const groups = groupFiles(files, 'flat');
    expect(groups).toHaveLength(1);
    expect(groups[0].files).toHaveLength(4);
  });
});

describe('fileLabel', () => {
  it('drops the part of the path the group header already shows', () => {
    expect(fileLabel('internal/adapter/http/api.go', 'package')).toBe('api.go');
    expect(fileLabel('internal/adapter/http/api.go', 'toplevel')).toBe('http/api.go');
    expect(fileLabel('internal/adapter/http/api.go', 'flat')).toBe('internal/adapter/http/api.go');
    expect(fileLabel('README.md', 'package')).toBe('README.md');
  });
});

describe('statusLabel', () => {
  it('reads similarity-suffixed codes and prefers the untracked flag', () => {
    expect(statusLabel(file({ path: 'a', status: 'R100' }))).toBe('renamed');
    expect(statusLabel(file({ path: 'a', status: 'A' }))).toBe('added');
    expect(statusLabel(file({ path: 'a', status: 'A', untracked: true }))).toBe('untracked');
  });
});
