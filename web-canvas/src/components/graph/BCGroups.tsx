'use client';

import type { BoundedContext } from '@/lib/graph/types';

export interface BCGroupsProps {
  boundedContexts: BoundedContext[];
  show?: boolean;
  showLabels?: boolean;
}

export function BCGroups({
  boundedContexts,
  show = true,
  showLabels = true,
}: BCGroupsProps) {
  if (!show) return null;

  return (
    <>
      {boundedContexts.map((bc) => (
        <div
          key={bc.id}
          className="hf-bc-group"
          style={{
            left: bc.x,
            top: bc.y,
            width: bc.w,
            height: bc.h,
          }}
        >
          {showLabels && (
            <span className="hf-bc-label">{bc.name}</span>
          )}
        </div>
      ))}
    </>
  );
}
