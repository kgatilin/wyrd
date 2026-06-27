'use client';

import { useMemo, useState } from 'react';
import { Tree, type NodeRendererProps } from 'react-arborist';
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
  return conv(roots);
}

function Node({ node, style }: NodeRendererProps<TreeNode>) {
  const isLeaf = node.isLeaf;
  return (
    <div
      style={style}
      className={`ft-node${node.isSelected ? ' is-selected' : ''}`}
      onClick={() => (isLeaf ? node.tree.props.onActivate?.(node) : node.toggle())}
    >
      <span className="ft-icon">{isLeaf ? '📄' : node.isOpen ? '▾' : '▸'}</span>
      <span className="ft-label">{node.data.name}</span>
    </div>
  );
}

function FileTreeInner({ paths, height = 460 }: { paths: string[]; height?: number }) {
  const data = useMemo(() => buildTree(paths), [paths]);
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <div className="filetree" style={{ height }}>
      <div className="filetree-pane">
        {data.length === 0 ? (
          <div className="graph-block-loading">No files.</div>
        ) : (
          <Tree<TreeNode>
            data={data}
            openByDefault
            width={280}
            height={height}
            indent={14}
            rowHeight={26}
            onActivate={(node) => {
              if (node.data.path) setSelected(node.data.path);
            }}
          >
            {Node}
          </Tree>
        )}
      </div>
      <div className="filetree-file">
        {selected ? (
          <FileView key={selected} path={selected} defaultExpanded height={height - 40} />
        ) : (
          <div className="graph-block-loading">Select a file…</div>
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

  if (!graph) return <div className="graph-block-loading">Loading files for “{root}”…</div>;
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
 * or derive a package's files from the code graph via `root`. Click a file to
 * open it (with an inline diff when it differs from the base ref).
 */
export function FileTree({ root, paths, height = 460 }: FileTreeProps) {
  if (paths && paths.length) return <FileTreeInner paths={paths} height={height} />;
  if (root) return <FileTreeFromGraph root={root} height={height} />;
  return <div className="graph-block-loading">FileTree needs a `root` or `paths`.</div>;
}

export default FileTree;
