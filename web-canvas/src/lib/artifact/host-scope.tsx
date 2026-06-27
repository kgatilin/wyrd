"use client";

import { useEffect, useState } from 'react';
import dynamic from 'next/dynamic';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import { useGraph } from '@/lib/data/graph';

/**
 * `Markdown` is the host component for prose. The agent writes
 * `<Markdown>{`## Heading\n…`}</Markdown>` instead of raw HTML. Backed by
 * react-markdown (raw HTML escaped by default → no HTML injection) + GFM, and
 * styled by the existing `.prose-block` typography.
 */
export function MarkdownView({ children }: { children?: React.ReactNode }) {
  const content =
    typeof children === 'string'
      ? children
      : Array.isArray(children)
        ? children.join('')
        : String(children ?? '');
  return (
    <div className="prose-block">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

const GraphRenderer = dynamic(
  () => import('@/components/graph/Graph').then((m) => m.Graph),
  {
    ssr: false,
    loading: () => <div className="graph-block-loading">Loading graph renderer…</div>,
  },
);

/**
 * `Graph` is the host component exposed to artifact code. The agent writes
 * `<Graph source="component" height={520} />` — it names a data-source query and
 * the widget pulls the subgraph itself (via {@link useGraph}). The agent never
 * sees or bakes graph data; it only references a source.
 */
export function GraphView({
  source,
  query,
  nodes,
  hops,
  edges,
  height = 520,
  title,
  caption,
}: {
  source?: string;
  query?: string;
  nodes?: string[];
  hops?: number;
  edges?: string[];
  height?: number;
  title?: string;
  caption?: string;
}) {
  const graph = useGraph({ source, query, nodes, hops, edges });
  const [maximized, setMaximized] = useState(false);

  // Esc exits fullscreen.
  useEffect(() => {
    if (!maximized) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMaximized(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [maximized]);

  return (
    <figure className={`graph-block${maximized ? ' graph-block-maximized' : ''}`}>
      <figcaption className="graph-block-header">
        {title && <span className="graph-block-title">{title}</span>}
        {caption && <span className="graph-block-caption">{caption}</span>}
        <button
          type="button"
          className="graph-block-fullscreen"
          onClick={() => setMaximized((m) => !m)}
          title={maximized ? 'Exit fullscreen (Esc)' : 'Fullscreen'}
          aria-label={maximized ? 'Exit fullscreen' : 'Fullscreen'}
        >
          {maximized ? '✕' : '⛶'}
        </button>
      </figcaption>
      <div className="graph-block-body" style={maximized ? undefined : { height }}>
        {graph ? (
          <GraphRenderer graph={graph} showDiff cardDensity="compact" showInlineSignatures />
        ) : (
          <div className="graph-block-loading">
            Loading {query ? `“${query}”` : source ? `“${source}”` : 'graph'} from data-source…
          </div>
        )}
      </div>
    </figure>
  );
}

// mermaid is heavy and browser-only; load it lazily and initialize once.
let mermaidInited = false;
async function loadMermaid() {
  const mermaid = (await import('mermaid')).default;
  if (!mermaidInited) {
    mermaid.initialize({ startOnLoad: false, theme: 'default', securityLevel: 'strict' });
    mermaidInited = true;
  }
  return mermaid;
}

let mermaidSeq = 0;

/**
 * `Mermaid` renders a Mermaid diagram (flowchart, sequence, etc.) from its text.
 * The agent writes ``<Mermaid chart={`flowchart TD\n A --> B`} />`` — use this
 * for diagrams instead of a markdown code block (which only shows the source).
 * Rendering happens client-side; a syntax error is shown inline.
 */
export function MermaidView({
  chart,
  children,
}: {
  chart?: string;
  children?: React.ReactNode;
}) {
  const code = (
    chart ??
    (typeof children === 'string'
      ? children
      : Array.isArray(children)
        ? children.join('')
        : String(children ?? ''))
  ).trim();

  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const mermaid = await loadMermaid();
        const { svg } = await mermaid.render(`mmd-${mermaidSeq++}`, code);
        if (!cancelled) {
          setSvg(svg);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [code]);

  if (error) {
    return (
      <div className="artifact-error">
        Mermaid diagram error:
        <pre>{error}</pre>
      </div>
    );
  }
  if (!svg) return <div className="graph-block-loading">Rendering diagram…</div>;
  return <div className="mermaid-block" dangerouslySetInnerHTML={{ __html: svg }} />;
}
