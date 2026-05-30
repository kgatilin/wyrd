import type { BoundedContext, Component, Diff } from '../types';

export interface TreeProps {
  /** Bounded contexts to display */
  boundedContexts: BoundedContext[];
  /** All components (to filter by BC) */
  components: Component[];
  /** Whether to show diff badges */
  showDiff: boolean;
  /** Callback when a component row is clicked */
  onComponentClick?: (componentId: string) => void;
}

/**
 * Tree view of bounded contexts and their components.
 * Shows chevrons, icons, names, and optional diff badges.
 */
export function Tree({
  boundedContexts,
  components,
  showDiff,
  onComponentClick,
}: TreeProps) {
  return (
    <div className="hf-tree">
      {boundedContexts.map((bc) => (
        <div key={bc.id}>
          <div className="hf-tree-row bc">
            <span className="chev">&#9662;</span>
            <span className="ico">&#9635;</span>
            <span className="name">{bc.name}</span>
          </div>
          {components
            .filter((c) => c.bc === bc.id)
            .map((c) => {
              const diffCls = showDiff && c.diff ? c.diff : '';
              return (
                <div
                  key={c.id}
                  className={`hf-tree-row cmp ${diffCls}`}
                  style={{ paddingLeft: 20 }}
                  onClick={() => onComponentClick?.(c.id)}
                >
                  <span className="ico">&#9670;</span>
                  <span className="name">{c.name}</span>
                  {showDiff && c.diff && (
                    <span className="badge">{diffBadge(c.diff)}</span>
                  )}
                </div>
              );
            })}
        </div>
      ))}
    </div>
  );
}

function diffBadge(diff: Diff): string {
  switch (diff) {
    case 'added':
      return '+';
    case 'removed':
      return '-';
    case 'changed':
      return '~';
  }
}
