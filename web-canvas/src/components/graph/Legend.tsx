'use client';

export interface LegendProps {
  showDiff: boolean;
}

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
