import type { Artifact } from '@/lib/artifact/types';
import { ProseBlock } from './ProseBlock';
import { GraphBlock } from './GraphBlock';

/**
 * Renders an artifact as a flowing document: a centred, padded column of
 * blocks (prose interleaved with embedded widgets). This is the canvas's
 * primary surface.
 */
export function ArtifactDocument({ artifact }: { artifact: Artifact }) {
  return (
    <article className="artifact-doc">
      {artifact.blocks.map((block, i) => {
        switch (block.type) {
          case 'prose':
            return <ProseBlock key={i} markdown={block.markdown} />;
          case 'graph':
            return <GraphBlock key={i} block={block} />;
          default:
            return null;
        }
      })}
    </article>
  );
}
