/**
 * The textual side of a review: what changed in the files, as served by
 * /api/gitdiff. This module owns the payload types, the unified-patch
 * parser and the grouping of files into sections. It is pure — no fetch,
 * no DOM — so the parser is testable on its own.
 */

export type DiffLineKind = 'add' | 'del' | 'context' | 'meta';

export interface DiffLine {
  kind: DiffLineKind;
  /** Line content without the leading +/-/space marker. */
  text: string;
  /** 1-based line number on the base side; absent for additions. */
  oldNumber?: number;
  /** 1-based line number on the branch side; absent for deletions. */
  newNumber?: number;
}

export interface DiffHunk {
  /** The raw `@@ -a,b +c,d @@ context` header line. */
  header: string;
  /** Trailing context after the second `@@` (the enclosing func, usually). */
  section: string;
  lines: DiffLine[];
}

export interface DiffFile {
  path: string;
  oldPath?: string;
  /** git name-status code: A, M, D, R100, C75, ... */
  status: string;
  insertions: number;
  deletions: number;
  binary?: boolean;
  untracked?: boolean;
  patch?: string;
  truncated?: boolean;
}

export interface GitDiffStats {
  files: number;
  insertions: number;
  deletions: number;
}

export interface GitDiff {
  schema: string;
  worktree?: string;
  branch: string;
  baseRef: string;
  baseRev: string;
  files: DiffFile[];
  stats: GitDiffStats;
}

export interface DiffGroup {
  key: string;
  label: string;
  files: DiffFile[];
  insertions: number;
  deletions: number;
}

/**
 * How the file list is sectioned.
 * - `package`: one section per directory, i.e. per Go package.
 * - `toplevel`: one section per top-level area (`internal/adapter`, `cmd`, `web`).
 * - `flat`: no sections.
 */
export type GroupMode = 'package' | 'toplevel' | 'flat';

/**
 * Path roots that are containers rather than areas: `internal/adapter` is a
 * meaningful section, plain `internal` is the whole codebase. For these the
 * top-level grouping keeps two segments.
 */
const NESTED_ROOTS = new Set(['internal', 'pkg', 'cmd', 'src', 'apps', 'services', 'lib']);

const ROOT_LABEL = '(root)';

/** Splits a file's unified patch into hunks with per-side line numbers. */
export function parseHunks(patch: string | undefined): DiffHunk[] {
  if (!patch) return [];
  const hunks: DiffHunk[] = [];
  let current: DiffHunk | null = null;
  let oldNumber = 0;
  let newNumber = 0;

  for (const line of patch.split('\n')) {
    const head = parseHunkHeader(line);
    if (head) {
      current = { header: line, section: head.section, lines: [] };
      hunks.push(current);
      oldNumber = head.oldStart;
      newNumber = head.newStart;
      continue;
    }
    // Everything before the first @@ is the `diff --git` / index / ---
    // preamble: the file header is already in DiffFile, so drop it.
    if (!current) continue;

    if (line.startsWith('+')) {
      current.lines.push({ kind: 'add', text: line.slice(1), newNumber: newNumber++ });
    } else if (line.startsWith('-')) {
      current.lines.push({ kind: 'del', text: line.slice(1), oldNumber: oldNumber++ });
    } else if (line.startsWith('\\')) {
      // "\ No newline at end of file" belongs to the previous line, and
      // advances neither counter.
      current.lines.push({ kind: 'meta', text: line.slice(2) });
    } else if (line.startsWith(' ')) {
      current.lines.push({ kind: 'context', text: line.slice(1), oldNumber: oldNumber++, newNumber: newNumber++ });
    } else if (line === '') {
      // git emits a bare empty line for an empty context line at the end
      // of a hunk; treat it as context so the numbering stays aligned.
      current.lines.push({ kind: 'context', text: '', oldNumber: oldNumber++, newNumber: newNumber++ });
    }
  }

  // A trailing newline in the patch produces one phantom context line.
  const last = hunks[hunks.length - 1];
  if (last) {
    const tail = last.lines[last.lines.length - 1];
    if (tail && tail.kind === 'context' && tail.text === '' && !patch.endsWith('\n\n')) {
      last.lines.pop();
    }
  }
  return hunks;
}

function parseHunkHeader(line: string): { oldStart: number; newStart: number; section: string } | null {
  if (!line.startsWith('@@')) return null;
  const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/.exec(line);
  if (!match) return null;
  return { oldStart: Number(match[1]), newStart: Number(match[2]), section: match[3] ?? '' };
}

/** Sections files for the list rail. Groups and files are path-sorted. */
export function groupFiles(files: DiffFile[], mode: GroupMode): DiffGroup[] {
  const byKey = new Map<string, DiffGroup>();
  for (const file of files) {
    const key = groupKey(file.path, mode);
    let group = byKey.get(key);
    if (!group) {
      group = { key, label: key, files: [], insertions: 0, deletions: 0 };
      byKey.set(key, group);
    }
    group.files.push(file);
    group.insertions += file.insertions;
    group.deletions += file.deletions;
  }
  const groups = [...byKey.values()];
  for (const group of groups) {
    group.files.sort((a, b) => a.path.localeCompare(b.path));
  }
  groups.sort((a, b) => a.key.localeCompare(b.key));
  return groups;
}

function groupKey(path: string, mode: GroupMode): string {
  if (mode === 'flat') return 'Changed files';
  const segments = path.split('/');
  if (segments.length < 2) return ROOT_LABEL;
  if (mode === 'package') return segments.slice(0, -1).join('/');
  const depth = NESTED_ROOTS.has(segments[0]) && segments.length > 2 ? 2 : 1;
  return segments.slice(0, depth).join('/');
}

/** The file name alone, for a list row whose group already shows the directory. */
export function fileLabel(path: string, mode: GroupMode): string {
  if (mode === 'flat') return path;
  const segments = path.split('/');
  if (mode === 'package') return segments[segments.length - 1];
  const key = groupKey(path, mode);
  return key === ROOT_LABEL ? path : path.slice(key.length + 1);
}

/** One-letter badge for a git status code (R100 → R). */
export function statusBadge(status: string): string {
  return status.charAt(0).toUpperCase() || 'M';
}

export function statusLabel(file: DiffFile): string {
  if (file.untracked) return 'untracked';
  switch (statusBadge(file.status)) {
    case 'A':
      return 'added';
    case 'D':
      return 'deleted';
    case 'R':
      return 'renamed';
    case 'C':
      return 'copied';
    case 'T':
      return 'type changed';
    default:
      return 'modified';
  }
}

/**
 * Whether two reads of the diff describe the same working tree.
 *
 * The daemon recomputes the whole diff per request and has no ETag, so the
 * client is the only place that can tell a refresh that found nothing from
 * one that found an edit. Structural equality over the payload is exact
 * here: the diff is plain JSON the daemon serializes in a fixed order.
 */
export function sameDiff(a: GitDiff, b: GitDiff): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}
