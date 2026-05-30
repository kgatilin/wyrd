export interface CanvasToolbarProps {
  /** Current zoom level as percentage (cosmetic only for POC) */
  zoom?: number;
}

/**
 * Canvas toolbar with zoom controls.
 * Cosmetic only for POC - buttons are non-functional placeholders.
 */
export function CanvasToolbar({ zoom = 100 }: CanvasToolbarProps) {
  return (
    <div className="hf-canvas-toolbar">
      <button title="Zoom out">−</button>
      <button className="zoom">{zoom}%</button>
      <button title="Zoom in">+</button>
      <button title="Fit">⊡</button>
      <button title="Mini-map">⊞</button>
    </div>
  );
}
