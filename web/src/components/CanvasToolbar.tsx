import type { CardDensity } from '../domain/state';

export interface CanvasToolbarProps {
  /** Current zoom as a fraction (1 = 100%). */
  zoom?: number;
  /** Zoom out one step. */
  onZoomOut?: () => void;
  /** Zoom in one step. */
  onZoomIn?: () => void;
  /** Reset zoom to fit the diagram in the viewport. */
  onFit?: () => void;
  /** Expand every visible package card. */
  onExpandAll?: () => void;
  /** Collapse every visible package card. */
  onCollapseAll?: () => void;
  /** Number of manually pinned layout positions in the current review scope. */
  pinnedCount?: number;
  /** Reset saved pinned layout positions for the current review scope. */
  onResetLayout?: () => void;
  /** Reset every saved layout for this repo. */
  onResetRepoLayout?: () => void;
  /** Whether group labels are currently visible. */
  showGroupLabels?: boolean;
  /** Toggle group labels on the canvas. */
  onToggleGroupLabels?: () => void;
  /** Current collapsed-card density. */
  cardDensity?: CardDensity;
  /** Toggle collapsed-card density. */
  onToggleCardDensity?: () => void;
  /** Whether full signatures are shown inline in expanded blocks. */
  showInlineSignatures?: boolean;
  /** Toggle inline signature display. */
  onToggleInlineSignatures?: () => void;
}

/**
 * Canvas toolbar with working zoom controls.
 * Lives in the canvas viewport (not the scroller), so it stays pinned to the
 * bottom-left while the diagram scrolls underneath it.
 */
export function CanvasToolbar({
  zoom = 1,
  onZoomOut,
  onZoomIn,
  onFit,
  onExpandAll,
  onCollapseAll,
  pinnedCount = 0,
  onResetLayout,
  onResetRepoLayout,
  showGroupLabels = true,
  onToggleGroupLabels,
  cardDensity = 'detailed',
  onToggleCardDensity,
  showInlineSignatures = true,
  onToggleInlineSignatures,
}: CanvasToolbarProps) {
  const pct = Math.round(zoom * 100);
  return (
    <div className="hf-canvas-toolbar">
      <button title="Zoom out" onClick={onZoomOut}>−</button>
      <button className="zoom" title="Reset to fit" onClick={onFit}>{pct}%</button>
      <button title="Zoom in" onClick={onZoomIn}>+</button>
      <button title="Fit" onClick={onFit}>⊡</button>
      {onExpandAll && <button title="Expand all packages" onClick={onExpandAll}>⇲</button>}
      {onCollapseAll && <button title="Collapse all packages" onClick={onCollapseAll}>⇱</button>}
      {onToggleCardDensity && (
        <button
          className={cardDensity === 'compact' ? 'on' : ''}
          title={cardDensity === 'compact' ? 'Use detailed cards' : 'Use compact cards'}
          onClick={onToggleCardDensity}
        >
          ▤
        </button>
      )}
      {onToggleInlineSignatures && (
        <button
          className={showInlineSignatures ? 'on' : ''}
          title={showInlineSignatures ? 'Hide inline signatures' : 'Show inline signatures'}
          onClick={onToggleInlineSignatures}
        >
          S()
        </button>
      )}
      {onToggleGroupLabels && (
        <button
          className={showGroupLabels ? 'on' : ''}
          title={showGroupLabels ? 'Hide group labels' : 'Show group labels'}
          onClick={onToggleGroupLabels}
        >
          Aa
        </button>
      )}
      {pinnedCount > 0 && (
        <button
          className="pins"
          title={`Reset current view layout (${pinnedCount} pinned position${pinnedCount === 1 ? '' : 's'})`}
          onClick={onResetLayout}
        >
          ↺
        </button>
      )}
      {onResetRepoLayout && (
        <button
          className="pins"
          title="Reset all saved layouts for this repo"
          onClick={onResetRepoLayout}
        >
          ⟲
        </button>
      )}
    </div>
  );
}
