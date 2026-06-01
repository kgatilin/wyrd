import { useState } from 'react';
import type { BoundedContext, Component, Internal, Diff } from '../types';

/** Identifies which canvas object a tree row points at. */
export interface TreeFocusTarget {
  componentId: string;
  internalId?: string;
  memberId?: string;
}

export interface TreeProps {
  /** Bounded contexts to display */
  boundedContexts: BoundedContext[];
  /** All components (filtered by BC) */
  components: Component[];
  /** Whether to show diff badges */
  showDiff: boolean;
  /** Currently focused component id — highlights the matching row */
  activeId?: string | null;
  /** Click a row → focus the matching object on the canvas */
  onFocus?: (target: TreeFocusTarget) => void;
}

/**
 * Collapsible tree of the model: bounded context → component → internal → member.
 * Bounded contexts start expanded so their components are visible; components and
 * internals start collapsed. Clicking the chevron expands a node; clicking the row
 * focuses the corresponding object on the canvas.
 */
export function Tree({
  boundedContexts,
  components,
  showDiff,
  activeId,
  onFocus,
}: TreeProps) {
  // Track collapsed node keys. Components and internals start collapsed; bounded
  // contexts (absent from the set) start open.
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    const s = new Set<string>();
    for (const c of components) {
      s.add(`cmp:${c.id}`);
      for (const it of c.internals) s.add(`int:${it.id}`);
    }
    return s;
  });
  const isOpen = (key: string) => !collapsed.has(key);
  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <div className="hf-tree">
      {boundedContexts.map((bc) => {
        const key = `bc:${bc.id}`;
        const open = isOpen(key);
        const bcComps = components.filter((c) => c.bc === bc.id);
        return (
          <div key={bc.id}>
            <div className="hf-tree-row bc" onClick={() => toggle(key)}>
              <Chevron open={open} has={bcComps.length > 0} />
              <span className="ico">&#9635;</span>
              <span className="name">{bc.name}</span>
            </div>
            {open &&
              bcComps.map((c) => (
                <ComponentNode
                  key={c.id}
                  cmp={c}
                  showDiff={showDiff}
                  activeId={activeId}
                  isOpen={isOpen}
                  toggle={toggle}
                  onFocus={onFocus}
                />
              ))}
          </div>
        );
      })}
    </div>
  );
}

interface ComponentNodeProps {
  cmp: Component;
  showDiff: boolean;
  activeId?: string | null;
  isOpen: (key: string) => boolean;
  toggle: (key: string) => void;
  onFocus?: (target: TreeFocusTarget) => void;
}

function ComponentNode({ cmp, showDiff, activeId, isOpen, toggle, onFocus }: ComponentNodeProps) {
  const key = `cmp:${cmp.id}`;
  const open = isOpen(key);
  const has = cmp.internals.length > 0;
  const diffCls = showDiff && cmp.diff ? cmp.diff : '';
  return (
    <div>
      <div
        className={`hf-tree-row cmp ${diffCls} ${activeId === cmp.id ? 'active' : ''}`}
        style={{ paddingLeft: 20 }}
        onClick={() => onFocus?.({ componentId: cmp.id })}
      >
        <Chevron open={open} has={has} onToggle={() => toggle(key)} />
        <span className="ico">&#9670;</span>
        <span className="name">{cmp.name}</span>
        {showDiff && cmp.diff && <span className="badge">{diffBadge(cmp.diff)}</span>}
      </div>
      {open &&
        cmp.internals.map((it) => (
          <InternalNode
            key={it.id}
            cmp={cmp}
            internal={it}
            showDiff={showDiff}
            isOpen={isOpen}
            toggle={toggle}
            onFocus={onFocus}
          />
        ))}
    </div>
  );
}

interface InternalNodeProps {
  cmp: Component;
  internal: Internal;
  showDiff: boolean;
  isOpen: (key: string) => boolean;
  toggle: (key: string) => void;
  onFocus?: (target: TreeFocusTarget) => void;
}

function InternalNode({ cmp, internal, showDiff, isOpen, toggle, onFocus }: InternalNodeProps) {
  const key = `int:${internal.id}`;
  const open = isOpen(key);
  const members = internal.members ?? [];
  const has = members.length > 0;
  const diffCls = showDiff && internal.diff ? internal.diff : '';
  return (
    <div>
      <div
        className={`hf-tree-row internal ${internal.kind} ${diffCls}`}
        style={{ paddingLeft: 40 }}
        onClick={() => onFocus?.({ componentId: cmp.id, internalId: internal.id })}
      >
        <Chevron open={open} has={has} onToggle={() => toggle(key)} />
        <span className="ico">{internal.kind === 'iface' ? '○' : '◇'}</span>
        <span className="name">{internal.name}</span>
        {showDiff && internal.diff && <span className="badge">{diffBadge(internal.diff)}</span>}
      </div>
      {open &&
        members.map((m) => {
          const mDiff = showDiff && m.diff ? m.diff : '';
          return (
            <div
              key={m.id}
              className={`hf-tree-row member ${mDiff}`}
              style={{ paddingLeft: 60 }}
              onClick={() =>
                onFocus?.({ componentId: cmp.id, internalId: internal.id, memberId: m.id })
              }
            >
              <Chevron open={false} has={false} />
              <span className="ico">{m.kind === 'method' ? 'ƒ' : ':'}</span>
              <span className="name">{m.name}</span>
              {showDiff && m.diff && <span className="badge">{diffBadge(m.diff)}</span>}
            </div>
          );
        })}
    </div>
  );
}

interface ChevronProps {
  open: boolean;
  has: boolean;
  onToggle?: () => void;
}

/** Twisty triangle. Hidden (but space-reserved) when the node has no children. */
function Chevron({ open, has, onToggle }: ChevronProps) {
  return (
    <span
      className="chev"
      style={{ visibility: has ? 'visible' : 'hidden' }}
      onClick={
        onToggle
          ? (e) => {
              e.stopPropagation();
              if (has) onToggle();
            }
          : undefined
      }
    >
      {open ? '▾' : '▸'}
    </span>
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
