import { useState } from 'react';
import type { BoundedContext, Component, Internal, Diff } from '../types';
export type { TreeFocusTarget } from '../domain/events';
import type { TreeFocusTarget } from '../domain/events';
import { SignatureDiff } from './SignatureDiff';

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
  /** Open a source file inside the review UI */
  onOpenFile?: (path: string) => void;
}

function internalIcon(kind: Internal['kind']): string {
  switch (kind) {
    case 'iface':
      return '○';
    case 'class':
      return '◇';
    case 'func':
      return 'ƒ';
    case 'type':
      return 'T';
    case 'const':
      return 'C';
    case 'var':
      return 'V';
    case 'error':
      return '!';
    default:
      return '◇';
  }
}

function memberIcon(kind: Internal['members'][number]['kind']): string {
  switch (kind) {
    case 'method':
      return 'ƒ';
    case 'prop':
      return ':';
    case 'const':
      return 'C';
    default:
      return ':';
  }
}

/**
 * Collapsible review tree: package → source file → type/symbol → member.
 * This intentionally ignores top-level graph grouping; grouping here is about
 * understanding one package's changed contents.
 */
export function Tree({
  components,
  showDiff,
  activeId,
  onFocus,
  onOpenFile,
}: TreeProps) {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    const s = new Set<string>();
    for (const c of components) {
      for (const it of c.internals) {
        if (it.diff === 'added' || it.diff === 'removed') s.add(`int:${it.id}`);
      }
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
      {components.map((c) => (
        <ComponentNode
          key={c.id}
          cmp={c}
          showDiff={showDiff}
          activeId={activeId}
          isOpen={isOpen}
          toggle={toggle}
          onFocus={onFocus}
          onOpenFile={onOpenFile}
        />
      ))}
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
  onOpenFile?: (path: string) => void;
}

function ComponentNode({ cmp, showDiff, activeId, isOpen, toggle, onFocus, onOpenFile }: ComponentNodeProps) {
  const key = `cmp:${cmp.id}`;
  const open = isOpen(key);
  const has = cmp.internals.length > 0;
  const diffCls = showDiff && cmp.diff ? cmp.diff : '';
  const files = internalsByFile(cmp.internals);
  return (
    <div>
      <div
        className={`hf-tree-row cmp ${diffCls} ${activeId === cmp.id ? 'active' : ''}`}
        style={{ paddingLeft: 8 }}
        onClick={() => onFocus?.({ componentId: cmp.id })}
      >
        <Chevron open={open} has={has} onToggle={() => toggle(key)} />
        <span className="ico">&#9670;</span>
        <span className="name">{cmp.name}</span>
        {showDiff && cmp.diff && <span className="badge">{diffBadge(cmp.diff)}</span>}
      </div>
      {open &&
        files.map((file) => (
          <FileNode
            key={`${cmp.id}:${file.path}`}
            cmp={cmp}
            file={file}
            showDiff={showDiff}
            isOpen={isOpen}
            toggle={toggle}
            onFocus={onFocus}
            onOpenFile={onOpenFile}
          />
        ))}
    </div>
  );
}

interface FileGroup {
  path: string;
  name: string;
  internals: Internal[];
}

function internalsByFile(internals: Internal[]): FileGroup[] {
  const groups = new Map<string, Internal[]>();
  for (const internal of internals) {
    const path = sourceFilePath(internal.sourceFile);
    groups.set(path, [...(groups.get(path) ?? []), internal]);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([path, internals]) => ({ path, name: fileLabel(path), internals }));
}

const UNKNOWN_FILE = '(unknown file)';

function sourceFilePath(sourceFile?: string): string {
  return sourceFile || UNKNOWN_FILE;
}

function fileLabel(sourceFile: string): string {
  if (sourceFile === UNKNOWN_FILE) return UNKNOWN_FILE;
  return sourceFile.split('/').pop() ?? sourceFile;
}

function sourcePathForComponent(componentId: string, sourceFile: string): string | null {
  if (sourceFile === UNKNOWN_FILE) return null;
  return sourcePath(componentId, sourceFile);
}

function sourcePath(componentId: string, sourceFile: string): string {
  if (sourceFile.includes('/')) return sourceFile;
  if (!componentId || componentId === '.') return sourceFile;
  return `${componentId}/${sourceFile}`;
}

interface FileNodeProps {
  cmp: Component;
  file: FileGroup;
  showDiff: boolean;
  isOpen: (key: string) => boolean;
  toggle: (key: string) => void;
  onFocus?: (target: TreeFocusTarget) => void;
  onOpenFile?: (path: string) => void;
}

function FileNode({ cmp, file, showDiff, isOpen, toggle, onFocus, onOpenFile }: FileNodeProps) {
  const key = `file:${cmp.id}:${file.path}`;
  const open = isOpen(key);
  const sourcePath = sourcePathForComponent(cmp.id, file.path);
  return (
    <div>
      <div
        className="hf-tree-row file"
        style={{ paddingLeft: 24 }}
        onClick={() => toggle(key)}
      >
        <Chevron open={open} has={file.internals.length > 0} onToggle={() => toggle(key)} />
        <span className="ico">#</span>
        {sourcePath ? (
          <button
            className="name hf-file-open"
            type="button"
            title={sourcePath}
            onClick={(e) => {
              e.stopPropagation();
              onOpenFile?.(sourcePath);
            }}
          >
            {file.name}
          </button>
        ) : (
          <span className="name">{file.name}</span>
        )}
      </div>
      {open &&
        file.internals.map((it) => (
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
        style={{ paddingLeft: 44 }}
        onClick={() => onFocus?.({ componentId: cmp.id, internalId: internal.id })}
      >
        <Chevron open={open} has={has} onToggle={() => toggle(key)} />
        <span className="ico">{internalIcon(internal.kind)}</span>
        <span className="name">{internal.name}</span>
        {showDiff && internal.diff && <span className="badge">{diffBadge(internal.diff)}</span>}
      </div>
      {internal.diff === 'changed' && <SignatureDiff before={internal.diffBefore} after={internal.diffAfter} />}
      {open &&
        members.map((m) => {
          const mDiff = showDiff && m.diff ? m.diff : '';
          return (
            <div key={m.id}>
              <div
                className={`hf-tree-row member ${mDiff}`}
                style={{ paddingLeft: 64 }}
                onClick={() =>
                  onFocus?.({ componentId: cmp.id, internalId: internal.id, memberId: m.id })
                }
              >
                <Chevron open={false} has={false} />
                <span className="ico">{memberIcon(m.kind)}</span>
                <span className="name">{m.name}</span>
                {showDiff && m.diff && <span className="badge">{diffBadge(m.diff)}</span>}
              </div>
              {m.diff === 'changed' && <SignatureDiff before={m.diffBefore} after={m.diffAfter} />}
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
