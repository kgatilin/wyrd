export interface Marker {
  /** Unique marker ID */
  id: string;
  /** Comment number (1-indexed) */
  n: number;
  /** Canvas-relative X coordinate */
  x: number;
  /** Canvas-relative Y coordinate */
  y: number;
  /** The target element */
  target: { type: string; id: string };
  /** Comment body text */
  body: string;
  /** Author (display name) */
  author: string;
  /** Relative time (e.g., "just now", "2m") */
  when: string;
}

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
