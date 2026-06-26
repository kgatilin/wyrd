import { ArtifactDocument } from '@/components/canvas/ArtifactDocument';
import { sampleArtifact } from '@/lib/artifact/sample';

/**
 * The canvas is a scrollable document surface. It renders the active artifact
 * (prose + embedded widgets), not a single full-bleed graph.
 */
export function CanvasPanel() {
  return (
    <div className="canvas-surface">
      <ArtifactDocument artifact={sampleArtifact} />
    </div>
  );
}
