import { useRef, useState } from 'react';
import type { BoundedContext } from '../types';

export interface BCGroupsProps {
  /** Bounded contexts with layout geometry */
  boundedContexts: BoundedContext[];
  /** Whether to show the BC groups (false hides them) */
  show?: boolean;
  /** Whether to show group text labels */
  showLabels?: boolean;
  /** Canvas zoom used to convert screen-pixel drag deltas to graph coordinates */
  zoom?: number;
  /** Callback when a group is manually moved; deltas are in graph coordinates */
  onMoveGroup?: (id: string, dx: number, dy: number) => void;
  /** Group ids with at least one manually pinned component */
  pinnedGroupIds?: ReadonlySet<string>;
  /** Callback to clear pinned layout positions for every component in a group */
  onResetGroupLayout?: (id: string) => void;
}

/**
 * Renders positioned bounded-context boxes with labels.
 * Each BC is a dashed-border container that groups related components.
 */
export function BCGroups({
  boundedContexts,
  show = true,
  showLabels = true,
  zoom = 1,
  onMoveGroup,
  pinnedGroupIds,
  onResetGroupLayout,
}: BCGroupsProps) {
  const [draggingGroupId, setDraggingGroupId] = useState<string | null>(null);
  const dragRef = useRef<{
    id: string;
    pointerId: number;
    lastClientX: number;
    lastClientY: number;
    dragging: boolean;
  } | null>(null);

  if (!show) return null;

  const handlePointerDown = (bc: BoundedContext, e: React.PointerEvent<HTMLDivElement>) => {
    if (!onMoveGroup || e.button !== 0) return;
    e.stopPropagation();
    dragRef.current = {
      id: bc.id,
      pointerId: e.pointerId,
      lastClientX: e.clientX,
      lastClientY: e.clientY,
      dragging: false,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    const screenDx = e.clientX - drag.lastClientX;
    const screenDy = e.clientY - drag.lastClientY;
    if (!drag.dragging && Math.hypot(screenDx, screenDy) < 4) return;
    if (!drag.dragging) {
      drag.dragging = true;
      setDraggingGroupId(drag.id);
    }
    drag.lastClientX = e.clientX;
    drag.lastClientY = e.clientY;
    e.preventDefault();
    const scale = zoom > 0 ? zoom : 1;
    onMoveGroup?.(drag.id, screenDx / scale, screenDy / scale);
  };

  const handlePointerEnd = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    dragRef.current = null;
    setDraggingGroupId(null);
    e.currentTarget.releasePointerCapture(e.pointerId);
  };

  return (
    <>
      {boundedContexts.map((bc) => {
        const canResetGroup = pinnedGroupIds?.has(bc.id) && onResetGroupLayout;
        return (
          <div
            key={bc.id}
            className={`hf-bc-group ${onMoveGroup ? 'draggable' : ''} ${draggingGroupId === bc.id ? 'dragging' : ''}`}
            style={{
              left: bc.x,
              top: bc.y,
              width: bc.w,
              height: bc.h,
            }}
            onClick={(e) => onMoveGroup && e.stopPropagation()}
            onPointerDown={(e) => handlePointerDown(bc, e)}
            onPointerMove={handlePointerMove}
            onPointerUp={handlePointerEnd}
            onPointerCancel={handlePointerEnd}
          >
            {(showLabels || canResetGroup) && (
              <span className={`hf-bc-label ${showLabels ? '' : 'icon-only'}`}>
                {showLabels && bc.name}
                {canResetGroup && (
                  <button
                    className="hf-bc-reset-layout"
                    title="Reset this group layout"
                    onClick={(e) => {
                      e.stopPropagation();
                      onResetGroupLayout(bc.id);
                    }}
                    onPointerDown={(e) => e.stopPropagation()}
                  >
                    ↺
                  </button>
                )}
              </span>
            )}
          </div>
        );
      })}
    </>
  );
}
