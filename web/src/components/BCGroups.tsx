import type { BoundedContext } from '../types';

export interface BCGroupsProps {
  /** Bounded contexts with layout geometry */
  boundedContexts: BoundedContext[];
  /** Whether to show the BC groups (false hides them) */
  show?: boolean;
}

/**
 * Renders positioned bounded-context boxes with labels.
 * Each BC is a dashed-border container that groups related components.
 */
export function BCGroups({ boundedContexts, show = true }: BCGroupsProps) {
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
          <span className="hf-bc-label">{bc.name}</span>
        </div>
      ))}
    </>
  );
}
