'use client';

import { useEffect, useMemo, useState } from 'react';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { diffLines } from 'diff';
import { useSource } from '@/lib/data/source';

const LANG: Record<string, string> = {
  go: 'go',
  ts: 'typescript',
  tsx: 'tsx',
  js: 'javascript',
  jsx: 'jsx',
  json: 'json',
  css: 'css',
  md: 'markdown',
  py: 'python',
  yaml: 'yaml',
  yml: 'yaml',
  sh: 'bash',
  sql: 'sql',
  html: 'markup',
  rs: 'rust',
  java: 'java',
  proto: 'protobuf',
};

function langFor(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? '';
  return LANG[ext] ?? 'text';
}

function baseName(path: string): string {
  return path.split('/').pop() || path;
}

export interface FileViewProps {
  /** Repo-relative path, e.g. "internal/retrieval/service.go". */
  path: string;
  /** Start expanded instead of collapsed. */
  defaultExpanded?: boolean;
  /** Body height (scrolls). */
  height?: number;
}

/**
 * A single source file: collapsed to a header by default, expand to read it
 * with syntax highlighting + line numbers. When the file differs from the base
 * ref, an inline diff is available (and shown first).
 */
export function FileView({ path, defaultExpanded = false, height = 420 }: FileViewProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  // Only fetch once expanded — a collapsed file costs nothing.
  const { data, error, loading } = useSource(expanded ? path : null, true);
  const [mode, setMode] = useState<'file' | 'diff'>('file');

  // When a diff is detected, default to showing it.
  useEffect(() => {
    if (data?.hasDiff) setMode('diff');
    else setMode('file');
  }, [data?.hasDiff]);

  return (
    <figure className="file-block">
      <figcaption className="file-block-header" onClick={() => setExpanded((e) => !e)}>
        <span className="file-block-chevron">{expanded ? '▾' : '▸'}</span>
        <span className="file-block-name" title={path}>
          {baseName(path)}
        </span>
        <span className="file-block-path">{path}</span>
        {data?.hasDiff && <span className="file-block-badge">diff</span>}
        {expanded && data?.hasDiff && (
          <span className="file-block-modes" role="group" onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              className={mode === 'diff' ? 'is-active' : ''}
              onClick={() => setMode('diff')}
            >
              Diff
            </button>
            <button
              type="button"
              className={mode === 'file' ? 'is-active' : ''}
              onClick={() => setMode('file')}
            >
              File
            </button>
          </span>
        )}
      </figcaption>

      {expanded && (
        <div className="file-block-body" style={{ maxHeight: height }}>
          {loading && <div className="graph-block-loading">Loading {baseName(path)}…</div>}
          {error && (
            <div className="artifact-error">
              File error:
              <pre>{error}</pre>
            </div>
          )}
          {data &&
            (mode === 'diff' && data.hasDiff ? (
              <DiffView oldValue={data.baseContent ?? ''} newValue={data.content} />
            ) : (
              <SyntaxHighlighter
                language={langFor(path)}
                style={oneLight}
                showLineNumbers
                wrapLongLines={false}
                customStyle={{ margin: 0, background: 'transparent', fontSize: '0.78rem' }}
              >
                {data.content}
              </SyntaxHighlighter>
            ))}
        </div>
      )}
    </figure>
  );
}

// --- inline unified diff (self-styled; GitHub-like, with context folding) ---

type DiffRowType = 'add' | 'del' | 'ctx';
interface DiffRow {
  oldNo: number | null;
  newNo: number | null;
  type: DiffRowType;
  text: string;
}
type DiffItem = { kind: 'row'; row: DiffRow } | { kind: 'gap'; count: number };

function buildDiffRows(oldStr: string, newStr: string): DiffRow[] {
  const parts = diffLines(oldStr, newStr);
  const rows: DiffRow[] = [];
  let oldNo = 1;
  let newNo = 1;
  for (const part of parts) {
    const lines = part.value.split('\n');
    if (lines.length && lines[lines.length - 1] === '') lines.pop();
    for (const line of lines) {
      if (part.added) rows.push({ oldNo: null, newNo: newNo++, type: 'add', text: line });
      else if (part.removed) rows.push({ oldNo: oldNo++, newNo: null, type: 'del', text: line });
      else rows.push({ oldNo: oldNo++, newNo: newNo++, type: 'ctx', text: line });
    }
  }
  return rows;
}

/** Keep `ctx` lines around each change; fold long unchanged runs into a gap. */
function foldContext(rows: DiffRow[], ctx = 3): DiffItem[] {
  const keep = new Array(rows.length).fill(false);
  rows.forEach((r, i) => {
    if (r.type !== 'ctx') {
      for (let j = Math.max(0, i - ctx); j <= Math.min(rows.length - 1, i + ctx); j++) keep[j] = true;
    }
  });
  const out: DiffItem[] = [];
  let i = 0;
  while (i < rows.length) {
    if (keep[i]) {
      out.push({ kind: 'row', row: rows[i] });
      i++;
    } else {
      let j = i;
      while (j < rows.length && !keep[j]) j++;
      out.push({ kind: 'gap', count: j - i });
      i = j;
    }
  }
  return out;
}

function DiffView({ oldValue, newValue }: { oldValue: string; newValue: string }) {
  const items = useMemo(() => foldContext(buildDiffRows(oldValue, newValue)), [oldValue, newValue]);
  return (
    <div className="diff">
      {items.map((it, idx) =>
        it.kind === 'gap' ? (
          <div className="diff-gap" key={idx}>
            ⋯ {it.count} unchanged line{it.count > 1 ? 's' : ''}
          </div>
        ) : (
          <div className={`diff-row diff-${it.row.type}`} key={idx}>
            <span className="diff-no">{it.row.oldNo ?? ''}</span>
            <span className="diff-no">{it.row.newNo ?? ''}</span>
            <span className="diff-mark">
              {it.row.type === 'add' ? '+' : it.row.type === 'del' ? '−' : ''}
            </span>
            <code className="diff-text">{it.row.text === '' ? ' ' : it.row.text}</code>
          </div>
        ),
      )}
    </div>
  );
}

export default FileView;
