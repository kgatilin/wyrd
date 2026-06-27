'use client';

import { useEffect, useState } from 'react';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import ReactDiffViewer from 'react-diff-viewer-continued';
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
              <ReactDiffViewer
                oldValue={data.baseContent ?? ''}
                newValue={data.content}
                splitView={false}
                showDiffOnly
                useDarkTheme={false}
                leftTitle={`${data.baseRef}:${baseName(path)}`}
                rightTitle="working tree"
              />
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

export default FileView;
