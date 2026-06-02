export type { Marker } from '../domain/state';
import type { Marker } from '../domain/state';

export interface PinnedMarkerProps {
  /** The marker data */
  marker: Marker;
  /** Whether this marker is currently active/selected */
  active: boolean;
  /** Callback when marker is clicked */
  onClick?: (marker: Marker) => void;
}

/**
 * Pinned numbered marker placed on the canvas at the comment location.
 * Displays as a small numbered pill.
 * Ported from hifi-v4.jsx PinnedMarker.
 */
export function PinnedMarker({ marker, active, onClick }: PinnedMarkerProps) {
  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClick?.(marker);
  };

  return (
    <div
      className={`hf-pin-marker ${active ? 'active' : ''}`}
      style={{ left: marker.x, top: marker.y }}
      onClick={handleClick}
    >
      <span className="hf-pin-marker-num">{marker.n}</span>
    </div>
  );
}
