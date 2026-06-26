import type { UIGraph, CardDensity } from '@/lib/graph/types';

/**
 * An artifact is a flowing document the agent composes: a sequence of blocks
 * (prose explanations interleaved with embedded widgets like graphs). The
 * canvas renders an artifact as a scrollable document, NOT a single full-bleed
 * widget. This is the contract the agent's `write_artifact` tool will emit.
 */
export interface Artifact {
  id: string;
  title: string;
  blocks: ArtifactBlock[];
}

export type ArtifactBlock = ProseBlockData | GraphBlockData;

/** A prose explanation rendered as lightweight markdown. */
export interface ProseBlockData {
  type: 'prose';
  /** Markdown source. */
  markdown: string;
}

/** An embedded architecture-graph widget, sized to its card (not full-bleed). */
export interface GraphBlockData {
  type: 'graph';
  /** Card header title, e.g. "internal/serve". */
  title?: string;
  /** Optional one-line caption under the title. */
  caption?: string;
  /** The graph data to render. */
  graph: UIGraph;
  /** Card body height in px (the widget is bounded, not full-screen). */
  height?: number;
  options?: {
    showDiff?: boolean;
    cardDensity?: CardDensity;
    showInlineSignatures?: boolean;
  };
}
