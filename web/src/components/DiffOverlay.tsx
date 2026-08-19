import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MouseEvent as ReactMouseEvent, UIEvent } from 'react';
import { fetchGitDiff } from '../data/gitDiff';
import {
  fileLabel,
  groupFiles,
  parseHunks,
  sameDiff,
  statusBadge,
  statusLabel,
  type DiffFile,
  type DiffHunk,
  type GitDiff,
  type GroupMode,
} from '../domain/gitDiff';
import { highlightLine, languageForPath } from './highlight';
import { SymbolGraphOverlay } from './SymbolGraphOverlay';
import { buildSymbolLookup, markSymbols, type SymbolLookup } from '../domain/codeSymbols';
import type { SymbolFocusTarget } from '../domain/symbolFocus';
import type { UIGraph } from '../types';

/**
 * Past this many lines a single file renders without syntax highlighting.
 * Generated files run to tens of thousands of lines, and highlighting each
 * one costs more than the reader gains.
 */
const MAX_HIGHLIGHT_LINES = 2000;

type LoadStatus = 'loading' | 'ready' | 'error';

/** Where the reviewer was inside the diff. Scroll offsets only — kept in a
 *  ref because they change on every wheel tick and re-render nothing. */
interface DiffScrollMemory {
  /** Scroll offset of the file rail. */
  files: number;
  /** Scroll offset of the patch pane, per file path. */
  patch: Map<string, number>;
}

/**
 * The diff under review plus the reviewer's position in it: which file is
 * open, which sections are folded, how far down each patch they read.
 */
export interface DiffSession {
  diff: GitDiff | null;
  status: LoadStatus;
  error: string | null;
  groupMode: GroupMode;
  setGroupMode: (mode: GroupMode) => void;
  selected: string | null;
  select: (path: string) => void;
  collapsed: ReadonlySet<string>;
  toggleGroup: (key: string) => void;
  scroll: { current: DiffScrollMemory };
  /** Read the diff again — now if the overlay is open, on its next open
   *  otherwise. The diff already on screen stays until the new one differs. */
  reload: () => void;
}

type SessionData = { status: LoadStatus; diff: GitDiff | null; error: string | null };

/**
 * Own the diff session in the app rather than in the overlay, so closing
 * the overlay does not throw the review away.
 *
 * The overlay is opened and closed constantly while reading the canvas, and
 * each open used to remount it: a fresh `/api/gitdiff` round trip — which
 * the daemon answers by re-shelling ~10 git commands and re-serializing
 * every patch — plus the loss of the selected file, the folded sections and
 * the scroll. Nothing about the review changed in between, so nothing
 * should have been recomputed.
 *
 * Staleness is not left to a timeout. The cache is dropped when the
 * reviewed worktree or base changes, and the app calls `reload` on the same
 * model-changed signal that reloads the canvas: the file diff and the
 * architecture diff must never disagree about which working tree they show.
 */
export function useDiffSession(worktree: string, baseRef: string, open: boolean): DiffSession {
  const key = `${worktree}\n${baseRef}`;
  const [token, setToken] = useState(0);
  const [data, setData] = useState<SessionData>({ status: 'loading', diff: null, error: null });
  const [groupMode, setGroupMode] = useState<GroupMode>('package');
  const [selected, setSelected] = useState<string | null>(null);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(() => new Set());
  const scroll = useRef<DiffScrollMemory>({ files: 0, patch: new Map() });
  // Which diff is loaded or in flight. While this matches, the effect below
  // is a no-op — that is what makes reopening the overlay free.
  const loaded = useRef<string | null>(null);

  useEffect(() => {
    // Another worktree or base is another review: the file list, and every
    // position inside it, belonged to the previous one.
    setSelected(null);
    setCollapsed(new Set());
    scroll.current = { files: 0, patch: new Map() };
  }, [key]);

  useEffect(() => {
    if (!open) return;
    const stamp = `${key}#${token}`;
    if (loaded.current === stamp) return;
    // A reload of the same review is a refresh, not a new read: the diff on
    // screen stays there while the daemon is asked again. Blanking it to a
    // spinner for an answer that is usually byte-identical is what made the
    // overlay flicker under a chatty model-changed stream.
    const refreshing = loaded.current !== null && loaded.current.startsWith(`${key}#`);
    loaded.current = stamp;
    let cancelled = false;
    if (!refreshing) setData({ status: 'loading', diff: null, error: null });
    fetchGitDiff(worktree, baseRef).then(
      (diff) => {
        if (cancelled) return;
        // Keep the previous object when the bytes match, so the selected
        // file and every scroll position survive the refresh untouched.
        setData((prev) =>
          prev.diff && sameDiff(prev.diff, diff) ? prev : { status: 'ready', diff, error: null }
        );
      },
      (err: unknown) => {
        if (cancelled) return;
        // A failed read is not a cache entry: reopening should try the
        // daemon again instead of replaying the same error.
        loaded.current = null;
        // A refresh that fails leaves the review readable — the diff on
        // screen is the last one the daemon actually answered — instead of
        // trading it for an error page.
        if (refreshing) return;
        setData({
          status: 'error',
          diff: null,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    );
    return () => {
      cancelled = true;
    };
  }, [open, key, token, worktree, baseRef]);

  const toggleGroup = useCallback((groupKey: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(groupKey)) next.delete(groupKey);
      else next.add(groupKey);
      return next;
    });
  }, []);

  const reload = useCallback(() => setToken((n) => n + 1), []);

  return {
    ...data,
    groupMode,
    setGroupMode,
    selected,
    select: setSelected,
    collapsed,
    toggleGroup,
    scroll,
    reload,
  };
}

export interface DiffOverlayProps {
  /** The cached session, owned by the app (see useDiffSession). */
  session: DiffSession;
  /**
   * The reviewed architecture. The diff shows the source of a change; the
   * graph is what turns a name in that source into "and here is what uses
   * it" without leaving the patch.
   */
  graph: UIGraph;
  /** Worktree whose working tree is diffed; empty = the served root. */
  worktree: string;
  /** Review base ref the diff is taken against (e.g. "main"). */
  baseRef: string;
  onClose: () => void;
}

/**
 * The file-level diff of the reviewed branch: a grouped file list on the
 * left, the selected file's patch on the right. This is the textual
 * counterpart to the architecture canvas — same review, other altitude.
 *
 * It renders the session and reports back into it; it owns no diff state of
 * its own, so it can be unmounted on close without costing anything.
 */
export function DiffOverlay({ session, graph, worktree, baseRef, onClose }: DiffOverlayProps) {
  const { diff, status, error, groupMode, selected, collapsed, scroll } = session;
  const { setGroupMode, select, toggleGroup, reload } = session;
  const patchRef = useRef<HTMLDivElement | null>(null);
  const filesRef = useRef<HTMLDivElement | null>(null);
  // The wiring panel opened from a name in the patch. Local to the diff: it
  // is a detour inside this reading, not a change of what the canvas shows.
  const [symbolFocus, setSymbolFocus] = useState<SymbolFocusTarget | null>(null);
  const lookup = useMemo(() => buildSymbolLookup(graph), [graph]);

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
    if (active && active.path !== selected) select(active.path);
  }, [active, selected, select]);

  useEffect(() => {
    // Land where this file was last read: at the top the first time, where
    // the reviewer left off when they come back to it.
    const pane = patchRef.current;
    if (!pane) return;
    pane.scrollTop = active ? scroll.current.patch.get(active.path) ?? 0 : 0;
  }, [active?.path, diff, scroll]);

  useEffect(() => {
    if (filesRef.current) filesRef.current.scrollTop = scroll.current.files;
  }, [diff, scroll]);

  const rememberPatchScroll = (event: UIEvent<HTMLDivElement>) => {
    if (active) scroll.current.patch.set(active.path, event.currentTarget.scrollTop);
  };
  const rememberFilesScroll = (event: UIEvent<HTMLDivElement>) => {
    scroll.current.files = event.currentTarget.scrollTop;
  };

  const step = useCallback(
    (delta: number) => {
      if (ordered.length === 0) return;
      const index = ordered.findIndex((file) => file.path === active?.path);
      const next = ordered[Math.min(ordered.length - 1, Math.max(0, index + delta))];
      if (next) select(next.path);
    },
    [ordered, active, select]
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target as HTMLElement | null;
      if (target && /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)) return;
      // While the wiring panel is up it owns the keyboard: Esc dismisses it,
      // and j/k must not walk the file list hidden behind it.
      if (symbolFocus) return;
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
  }, [onClose, step, symbolFocus]);

  // One delegated handler for the whole patch: a file's worth of marked
  // identifiers is thousands of spans, and none of them needs its own.
  const openSymbol = (event: ReactMouseEvent<HTMLDivElement>) => {
    const el = (event.target as HTMLElement | null)?.closest('.hf-code-sym');
    const name = el?.getAttribute('data-sym');
    if (!name || !active) return;
    const target = lookup.resolve(name, active.path);
    if (target) setSymbolFocus(target);
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
          {/* The revision, not just the ref: this diff starts at the merge
              base, which is the commit the architecture canvas is compared
              against too. Naming it is what lets a reviewer see the two
              agree. */}
          <span title={diff?.baseRev}>
            {diff?.baseRef ?? baseRef}
            {diff?.baseRev && <span className="rev">@{shortRev(diff.baseRev)}</span>}
          </span>
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
        <button className="hf-btn" onClick={reload} disabled={status === 'loading'}>
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
          <div className="hf-diff-files" ref={filesRef} onScroll={rememberFilesScroll}>
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
                      onClick={() => select(file.path)}
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

          <div
            className="hf-diff-view"
            ref={patchRef}
            onScroll={rememberPatchScroll}
            onClick={openSymbol}
          >
            {active && <FilePatch file={active} lookup={lookup} />}
          </div>
        </div>
      )}

      {symbolFocus && (
        <SymbolGraphOverlay graph={graph} target={symbolFocus} onClose={() => setSymbolFocus(null)} />
      )}
    </div>
  );
}

function FilePatch({ file, lookup }: { file: DiffFile; lookup: SymbolLookup }) {
  const hunks = useMemo(() => parseHunks(file.patch), [file.patch]);
  const lineCount = useMemo(
    () => hunks.reduce((total, hunk) => total + hunk.lines.length, 0),
    [hunks]
  );
  const language = lineCount <= MAX_HIGHLIGHT_LINES ? languageForPath(file.path) : undefined;
  // Resolution is per (name, file), and a patch repeats the same names on
  // line after line — decide once per name and reuse it down the file.
  const known = useMemo(() => {
    if (lookup.size === 0) return null;
    const cache = new Map<string, boolean>();
    return (name: string) => {
      const hit = cache.get(name);
      if (hit !== undefined) return hit;
      const resolved = lookup.resolve(name, file.path) !== null;
      cache.set(name, resolved);
      return resolved;
    };
  }, [lookup, file.path]);

  return (
    <>
      <div className="hf-diff-filehead">
        <span className={`badge ${statusBadge(file.status).toLowerCase()}`}>{statusBadge(file.status)}</span>
        <span className="path">{file.path}</span>
        <span className="kind">{statusLabel(file)}</span>
        {file.oldPath && <span className="from">from {file.oldPath}</span>}
        <span className="hf-spacer" />
        {known && <span className="hint">click a symbol for its wiring</span>}
        <span className="add">+{file.insertions}</span>
        <span className="rem">&minus;{file.deletions}</span>
      </div>

      {file.binary && <div className="hf-diff-note">Binary file &mdash; no textual diff.</div>}
      {!file.binary && hunks.length === 0 && (
        <div className="hf-diff-note">No textual change (mode or metadata only).</div>
      )}

      {hunks.map((hunk, index) => (
        <Hunk key={index} hunk={hunk} language={language} known={known} />
      ))}

      {file.truncated && (
        <div className="hf-diff-note">Patch truncated &mdash; open the file to see the rest.</div>
      )}
    </>
  );
}

function Hunk({
  hunk,
  language,
  known,
}: {
  hunk: DiffHunk;
  language: string | undefined;
  known: ((name: string) => boolean) | null;
}) {
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
              dangerouslySetInnerHTML={{ __html: codeHTML(line.text, language, known) }}
            />
          )}
        </div>
      ))}
    </div>
  );
}

/** Highlighted line, with the identifiers the graph knows made clickable. */
function codeHTML(
  text: string,
  language: string | undefined,
  known: ((name: string) => boolean) | null
): string {
  const html = highlightLine(text, language);
  return known ? markSymbols(html, known) : html;
}

function shortRev(rev: string): string {
  return rev.length > 7 ? rev.slice(0, 7) : rev;
}

function sourceLabel(diff: GitDiff | null, worktree: string): string {
  if (!diff) return worktree || 'HEAD';
  if (diff.branch === diff.baseRef) return 'working tree';
  return diff.branch;
}
