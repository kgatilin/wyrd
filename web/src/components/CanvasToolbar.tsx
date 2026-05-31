export interface CanvasToolbarProps {
  /** Current zoom as a fraction (1 = 100%). */
  zoom?: number;
  /** Zoom out one step. */
  onZoomOut?: () => void;
  /** Zoom in one step. */
  onZoomIn?: () => void;
  /** Reset zoom to fit the diagram in the viewport. */
  onFit?: () => void;
}

/**
 * Canvas toolbar with working zoom controls.
 * Lives in the canvas viewport (not the scroller), so it stays pinned to the
 * bottom-left while the diagram scrolls underneath it.
 */
export function CanvasToolbar({ zoom = 1, onZoomOut, onZoomIn, onFit }: CanvasToolbarProps) {
  const pct = Math.round(zoom * 100);
  return (
    <div className="hf-canvas-toolbar">
      <button title="Zoom out" onClick={onZoomOut}>−</button>
      <button className="zoom" title="Reset to fit" onClick={onFit}>{pct}%</button>
      <button title="Zoom in" onClick={onZoomIn}>+</button>
      <button title="Fit" onClick={onFit}>⊡</button>
    </div>
  );
}
