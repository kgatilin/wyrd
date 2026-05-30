export interface LegendProps {
  /** Whether to show the legend (only when diff mode is active) */
  showDiff: boolean;
}

/**
 * Diff legend showing added/removed/changed swatches.
 * Positioned in the top-right of the canvas.
 */
export function Legend({ showDiff }: LegendProps) {
  if (!showDiff) return null;

  return (
    <div className="hf-canvas-legend">
      <div className="hf-legend-item">
        <span
          className="hf-legend-swatch"
          style={{ background: 'var(--add-fg)' }}
        />
        added
      </div>
      <div className="hf-legend-item">
        <span
          className="hf-legend-swatch"
          style={{ background: 'var(--rem-fg)' }}
        />
        removed
      </div>
      <div className="hf-legend-item">
        <span
          className="hf-legend-swatch"
          style={{ background: 'var(--chg-fg)' }}
        />
        changed
      </div>
    </div>
  );
}
