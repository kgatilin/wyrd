import { ArtifactRenderer } from '@/lib/artifact/ArtifactRenderer';
import { exampleArtifactFile } from '@/lib/artifact/example-file';

/**
 * The canvas is a scrollable document surface that renders the active artifact
 * — a single agent-authored file executed at runtime. (For now it's a seeded
 * example file; the real agent will read/write/edit it.)
 */
export function CanvasPanel() {
  return (
    <div className="canvas-surface">
      <ArtifactRenderer code={exampleArtifactFile} />
    </div>
  );
}
