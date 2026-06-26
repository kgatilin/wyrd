"use client";

import { ArtifactRenderer } from '@/lib/artifact/ArtifactRenderer';
import { useArtifactStore } from '@/lib/artifact/store';

/**
 * The canvas renders the active artifact from the store — a single
 * agent-authored file executed at runtime.
 */
export function CanvasPanel() {
  const active = useArtifactStore(
    (s) => s.artifacts.find((a) => a.id === s.activeId) ?? null,
  );

  return (
    <div className="canvas-surface">
      {active ? (
        <ArtifactRenderer code={active.content} />
      ) : (
        <div className="canvas-empty">
          No artifact selected — generate one from the chat.
        </div>
      )}
    </div>
  );
}
