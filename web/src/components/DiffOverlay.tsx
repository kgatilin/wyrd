import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  fileLabel,
  groupFiles,
  parseHunks,
  statusBadge,
  statusLabel,
  type DiffFile,
  type DiffHunk,
  type GitDiff,
  type GroupMode,
} from '../domain/gitDiff';
import { highlightLine, languageForPath } from './highlight';

/**
 * Past this many lines a single file renders without syntax highlighting.
 * Generated files run to tens of thousands of lines, and highlighting each
 * one costs more than the reader gains.
 */
const MAX_HIGHLIGHT_LINES = 2000;

export interface DiffOverlayProps {
  /** Worktree whose working tree is diffed; empty = the served root. */
  worktree: string;
  /** Review base ref the diff is taken against (e.g. "main"). */
  baseRef: string;
  onClose: () => void;
}

type LoadStatus = 'loading' | 'ready' | 'error';

/**
 * The file-level diff of the reviewed branch: a grouped file list on the
 * left, the selected file's patch on the right. This is the textual
 * counterpart to the architecture canvas — same review, other altitude.
 */
export function DiffOverlay({ worktree, baseRef, onClose }: DiffOverlayProps) {
  const [diff, setDiff] = useState<GitDiff | null>(null);
  const [status, setStatus] = useState<LoadStatus>('loading');
  const [error, setError] = useState<string | null>(null);
  const [groupMode, setGroupMode] = useState<GroupMode>('package');
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const patchRef = useRef<HTMLDivElement | null>(null);

  const load = useCallback(async () => {
    setStatus('loading');
    setError(null);
    try {
      const res = await fetch(gitDiffURL(worktree, baseRef));
      if (!res.ok) {
        const message = await res.text();
        throw new Error(message.trim() || `HTTP ${res.status}`);
      }
      setDiff((await res.json()) as GitDiff);
      setStatus('ready');
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setStatus('error');
    }
  }, [worktree, baseRef]);

  useEffect(() => {
    void load();
  }, [load]);

  const files = diff?.files ?? [];
  const groups = useMemo(() => groupFiles(files, groupMode), [files, groupMode]);
  // Group order is the navigation order, so j/k walks the list as it reads.
  const ordered = useMemo(() => groups.flatMap((group) => group.files), [groups]);
  const active = useMemo(
    () => ordered.find((file) => file.path === selected) ?? ordered[0] ?? null,
    [ordered, selected]
  );

  useEffect(() => {
    // Keep a valid selection across reloads and grouping changes.
    if (active && active.path !== selected) setSelected(active.path);
  }, [active, selected]);

  useEffect(() => {
    if (patchRef.current) patchRef.current.scrollTop = 0;
  }, [active?.path]);

  const step = useCallback(
    (delta: number) => {
      if (ordered.length === 0) return;
      const index = ordered.findIndex((file) => file.path === active?.path);
      const next = ordered[Math.min(ordered.length - 1, Math.max(0, index + delta))];
      if (next) setSelected(next.path);
    },
    [ordered, active]
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return;
      if (event.key === 'Escape') {
        onClose();
      } else if (event.key === 'j' || event.key === 'ArrowDown') {
        event.preventDefault();
        step(1);
      } else if (event.key === 'k' || event.key === 'ArrowUp') {
        event.preventDefault();
        step(-1);
      } else {
        return;
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, step]);

  const toggleGroup = (key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  return (
    <div className="hf-diff-overlay" role="dialog" aria-label="File diff">
      <div className="hf-diff-head">
        <span className="hf-diff-tag">DIFF</span>
        <span className="hf-diff-compare">
          {/* On the base branch itself the diff is whatever is uncommitted,
              so naming the branch twice ("main ← main") would read as a
              no-op rather than as the working tree. */}
          <span className="branch">{sourceLabel(diff, worktree)}</span>
          <span className="sep">&larr;</span>
          <span>{diff?.baseRef ?? baseRef}</span>
        </span>
        {diff && (
          <span className="hf-diff-total">
            {diff.stats.files} {diff.stats.files === 1 ? 'file' : 'files'}
            <span className="add">+{diff.stats.insertions}</span>
            <span className="rem">&minus;{diff.stats.deletions}</span>
          </span>
        )}
        <span className="hf-spacer" />
        <label className="hf-diff-groupsel" title="How the file list is sectioned.">
          <span>Group</span>
          <select value={groupMode} onChange={(e) => setGroupMode(e.target.value as GroupMode)}>
            <option value="package">Package</option>
            <option value="toplevel">Top-level</option>
            <option value="flat">Flat</option>
          </select>
        </label>
        <button className="hf-btn" onClick={() => void load()} disabled={status === 'loading'}>
          {status === 'loading' ? 'Reading...' : 'Reload'}
        </button>
        <button className="hf-diff-close" onClick={onClose} title="Close (Esc)">
          &times;
        </button>
      </div>

      {status === 'loading' && !diff && <div className="hf-diff-note">Reading working tree...</div>}
      {status === 'error' && <div className="hf-diff-note error">{error ?? 'Failed to read the diff'}</div>}
      {status === 'ready' && files.length === 0 && (
        <div className="hf-diff-note">
          No file changes against {diff?.baseRef ?? baseRef}.
        </div>
      )}

      {files.length > 0 && (
        <div className="hf-diff-body">
          <div className="hf-diff-files">
            {groups.map((group) => (
              <div className="hf-diff-group" key={group.key}>
                <button
                  className="hf-diff-group-head"
                  onClick={() => toggleGroup(group.key)}
                  title={`${group.files.length} changed files in ${group.label}`}
                >
                  <span className="caret">{collapsed.has(group.key) ? '▸' : '▾'}</span>
                  <span className="label">{group.label}</span>
                  <span className="count">{group.files.length}</span>
                  <span className="add">+{group.insertions}</span>
                  <span className="rem">&minus;{group.deletions}</span>
                </button>
                {!collapsed.has(group.key) &&
                  group.files.map((file) => (
                    <button
                      key={file.path}
                      className={`hf-diff-file ${active?.path === file.path ? 'active' : ''}`}
                      onClick={() => setSelected(file.path)}
                      title={file.path}
                    >
                      <span className={`badge ${statusBadge(file.status).toLowerCase()}`}>
                        {statusBadge(file.status)}
                      </span>
                      <span className="name">{fileLabel(file.path, groupMode)}</span>
                      {file.insertions > 0 && <span className="add">+{file.insertions}</span>}
                      {file.deletions > 0 && <span className="rem">&minus;{file.deletions}</span>}
                    </button>
                  ))}
              </div>
            ))}
          </div>

          <div className="hf-diff-view" ref={patchRef}>
            {active && <FilePatch file={active} />}
          </div>
        </div>
      )}
    </div>
  );
}

function FilePatch({ file }: { file: DiffFile }) {
  const hunks = useMemo(() => parseHunks(file.patch), [file.patch]);
  const lineCount = useMemo(
    () => hunks.reduce((total, hunk) => total + hunk.lines.length, 0),
    [hunks]
  );
  const language = lineCount <= MAX_HIGHLIGHT_LINES ? languageForPath(file.path) : undefined;

  return (
    <>
      <div className="hf-diff-filehead">
        <span className={`badge ${statusBadge(file.status).toLowerCase()}`}>{statusBadge(file.status)}</span>
        <span className="path">{file.path}</span>
        <span className="kind">{statusLabel(file)}</span>
        {file.oldPath && <span className="from">from {file.oldPath}</span>}
        <span className="hf-spacer" />
        <span className="add">+{file.insertions}</span>
        <span className="rem">&minus;{file.deletions}</span>
      </div>

      {file.binary && <div className="hf-diff-note">Binary file &mdash; no textual diff.</div>}
      {!file.binary && hunks.length === 0 && (
        <div className="hf-diff-note">No textual change (mode or metadata only).</div>
      )}

      {hunks.map((hunk, index) => (
        <Hunk key={index} hunk={hunk} language={language} />
      ))}

      {file.truncated && (
        <div className="hf-diff-note">Patch truncated &mdash; open the file to see the rest.</div>
      )}
    </>
  );
}

function Hunk({ hunk, language }: { hunk: DiffHunk; language: string | undefined }) {
  return (
    <div className="hf-diff-hunk">
      <div className="hf-diff-hunk-head">
        <span className="range">{hunk.header.slice(0, hunk.header.indexOf('@@', 2) + 2)}</span>
        {hunk.section && <span className="section">{hunk.section}</span>}
      </div>
      {hunk.lines.map((line, index) => (
        <div className={`hf-diff-line ${line.kind}`} key={index}>
          <span className="ln">{line.oldNumber ?? ''}</span>
          <span className="ln">{line.newNumber ?? ''}</span>
          <span className="sign">{line.kind === 'add' ? '+' : line.kind === 'del' ? '-' : ' '}</span>
          {line.kind === 'meta' ? (
            <code className="text meta">{line.text}</code>
          ) : (
            <code
              className="text"
              dangerouslySetInnerHTML={{ __html: highlightLine(line.text, language) }}
            />
          )}
        </div>
      ))}
    </div>
  );
}

function sourceLabel(diff: GitDiff | null, worktree: string): string {
  if (!diff) return worktree || 'HEAD';
  if (diff.branch === diff.baseRef) return 'working tree';
  return diff.branch;
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
