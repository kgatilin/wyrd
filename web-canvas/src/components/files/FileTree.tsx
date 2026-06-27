'use client';

import { useEffect, useMemo, useState } from 'react';
import { useGraph } from '@/lib/data/graph';
import { FileView } from './FileView';

interface TreeNode {
  id: string;
  name: string;
  /** Set on file leaves (repo-relative path). */
  path?: string;
  children?: TreeNode[];
}

/** Build a nested folder/file tree from a flat list of repo-relative paths. */
function buildTree(paths: string[]): TreeNode[] {
  interface Raw {
    id: string;
    name: string;
    isFile: boolean;
    children: Map<string, Raw>;
  }
  const roots = new Map<string, Raw>();
  for (const p of paths) {
    const parts = p.split('/').filter(Boolean);
    let level = roots;
    let acc = '';
    parts.forEach((part, i) => {
      acc = acc ? `${acc}/${part}` : part;
      let n = level.get(part);
      if (!n) {
        n = { id: acc, name: part, isFile: i === parts.length - 1, children: new Map() };
        level.set(part, n);
      }
      level = n.children;
    });
  }
  const conv = (m: Map<string, Raw>): TreeNode[] => {
    const arr = [...m.values()];
    // Folders first, then alphabetical.
    arr.sort((a, b) => (a.isFile === b.isFile ? a.name.localeCompare(b.name) : a.isFile ? 1 : -1));
    return arr.map((n) =>
      n.isFile ? { id: n.id, name: n.name, path: n.id } : { id: n.id, name: n.name, children: conv(n.children) },
    );
  };
  return collapseChains(conv(roots));
}

/** Merge single-child folder chains (internal → sequence → …) into one row. */
function collapseChains(nodes: TreeNode[]): TreeNode[] {
  return nodes.map((n) => {
    if (!n.children) return n;
    let node: TreeNode = { ...n, children: collapseChains(n.children) };
    while (node.children && node.children.length === 1 && node.children[0].children) {
      const only = node.children[0];
      node = { id: only.id, name: `${node.name}/${only.name}`, children: only.children };
    }
    return node;
  });
}

/** Depth-first first file path, for an initial selection. */
function firstFile(nodes: TreeNode[]): string | null {
  for (const n of nodes) {
    if (n.path) return n.path;
    if (n.children) {
      const f = firstFile(n.children);
      if (f) return f;
    }
  }
  return null;
}

function TreeRows({
  nodes,
  depth,
  selected,
  onSelect,
}: {
  nodes: TreeNode[];
  depth: number;
  selected: string | null;
  onSelect: (path: string) => void;
}) {
  return (
    <>
      {nodes.map((n) => (
        <TreeRow key={n.id} node={n} depth={depth} selected={selected} onSelect={onSelect} />
      ))}
    </>
  );
}

function TreeRow({
  node,
  depth,
  selected,
  onSelect,
}: {
  node: TreeNode;
  depth: number;
  selected: string | null;
  onSelect: (path: string) => void;
}) {
  const isFile = !node.children;
  const [open, setOpen] = useState(true);
  return (
    <>
      <button
        type="button"
        className={`ft-row${isFile && selected === node.path ? ' is-selected' : ''}`}
        style={{ paddingLeft: 8 + depth * 14 }}
        onClick={() => (isFile ? onSelect(node.path!) : setOpen((o) => !o))}
        title={node.path ?? node.name}
      >
        <span className="ft-twisty">{isFile ? '' : open ? '▾' : '▸'}</span>
        <span className="ft-icon">{isFile ? '📄' : '📁'}</span>
        <span className="ft-label">{node.name}</span>
      </button>
      {!isFile && open && node.children && (
        <TreeRows nodes={node.children} depth={depth + 1} selected={selected} onSelect={onSelect} />
      )}
    </>
  );
}

function FileTreeInner({ paths, height = 460 }: { paths: string[]; height?: number }) {
  const data = useMemo(() => buildTree(paths), [paths]);
  const initial = useMemo(() => firstFile(data), [data]);
  const [selected, setSelected] = useState<string | null>(initial);

  // Reset selection when the file set changes (e.g. a different package).
  useEffect(() => setSelected(initial), [initial]);

  return (
    <div className="filetree" style={{ height }}>
      <nav className="filetree-pane">
        {data.length === 0 ? (
          <div className="filetree-empty">No files.</div>
        ) : (
          <TreeRows nodes={data} depth={0} selected={selected} onSelect={setSelected} />
        )}
      </nav>
      <div className="filetree-file">
        {selected ? (
          <FileView key={selected} path={selected} defaultExpanded height={height - 16} />
        ) : (
          <div className="filetree-empty">Select a file…</div>
        )}
      </div>
    </div>
  );
}

function FileTreeFromGraph({ root, height }: { root: string; height?: number }) {
  const graph = useGraph({ source: root });
  const paths = useMemo(() => {
    if (!graph) return [];
    const set = new Set<string>();
    for (const c of graph.components) {
      if (root && !c.id.includes(root)) continue;
      for (const i of c.internals) {
        if (i.sourceFile) set.add(`${c.id}/${i.sourceFile}`);
      }
    }
    return [...set].sort();
  }, [graph, root]);

  if (!graph) return <div className="filetree-empty">Loading files for “{root}”…</div>;
  return <FileTreeInner paths={paths} height={height} />;
}

export interface FileTreeProps {
  /** Package/path prefix to derive the file set from the code graph. */
  root?: string;
  /** Explicit repo-relative file paths (overrides root-derivation). */
  paths?: string[];
  height?: number;
}

/**
 * A mini file browser over a chosen subtree: pick files explicitly via `paths`
 * or derive a package's files from the code graph via `root`. The first file is
 * opened automatically; click another to open it (with an inline diff when it
 * differs from the base ref).
 */
export function FileTree({ root, paths, height = 460 }: FileTreeProps) {
  if (paths && paths.length) return <FileTreeInner paths={paths} height={height} />;
  if (root) return <FileTreeFromGraph root={root} height={height} />;
  return <div className="filetree-empty">FileTree needs a `root` or `paths`.</div>;
}

export default FileTree;
