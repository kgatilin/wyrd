import type { UIGraph } from '../types';
import type { SymbolFocusTarget } from './symbolFocus';

/**
 * Identifiers in code text, resolved against the graph.
 *
 * The file diff shows source, and the graph knows what the symbols in that
 * source are wired to — but only if a name written in a patch can be turned
 * back into a graph node. That is this module: a name index over the graph
 * keyed by the file a symbol is declared in, plus the marker that tags the
 * resolvable names inside an already-highlighted line.
 */

interface SymbolSite {
  target: SymbolFocusTarget;
  /** Repo-relative path of the file the symbol is declared in. */
  filePath: string;
  componentId: string;
  /** 0 for a top-level declaration, 1 for a member — declarations win ties. */
  depth: number;
}

export interface SymbolLookup {
  /**
   * The symbol a name refers to when read in `filePath`, or null when the
   * graph has no symbol by that name or cannot tell which one is meant.
   */
  resolve(name: string, filePath: string): SymbolFocusTarget | null;
  /** Number of distinct names indexed — 0 means nothing is clickable. */
  size: number;
}

/** Member kinds that are rows on a card, not symbols in their own right. */
const NON_SYMBOL_MEMBERS = new Set(['param', 'return']);

export function buildSymbolLookup(graph: UIGraph): SymbolLookup {
  const byName = new Map<string, SymbolSite[]>();
  const add = (name: string, site: SymbolSite) => {
    if (!name) return;
    byName.set(name, [...(byName.get(name) ?? []), site]);
  };

  for (const component of graph.components) {
    for (const internal of component.internals) {
      const target: SymbolFocusTarget = { componentId: component.id, internalId: internal.id };
      add(bareName(internal.name), {
        target,
        filePath: filePathOf(component.id, internal.sourceFile),
        componentId: component.id,
        depth: 0,
      });
      for (const member of internal.members ?? []) {
        if (NON_SYMBOL_MEMBERS.has(member.kind)) continue;
        add(bareName(member.name), {
          target: { ...target, memberId: member.id },
          filePath: filePathOf(component.id, member.sourceFile ?? internal.sourceFile),
          componentId: component.id,
          depth: 1,
        });
      }
    }
  }

  return {
    size: byName.size,
    resolve(name, filePath) {
      const candidates = byName.get(name);
      if (!candidates) return null;
      const dir = packageOf(filePath);
      // Nearest scope wins: the file being read, then its package, then the
      // whole graph. A name that stays ambiguous is left alone — jumping to
      // the wrong `Save` is worse than not being clickable.
      return (
        pick(candidates.filter((site) => site.filePath === filePath)) ??
        pick(candidates.filter((site) => site.componentId === dir)) ??
        pick(candidates)
      );
    },
  };
}

function pick(sites: SymbolSite[]): SymbolFocusTarget | null {
  if (sites.length === 0) return null;
  if (sites.length === 1) return sites[0].target;
  const declarations = sites.filter((site) => site.depth === 0);
  return declarations.length === 1 ? declarations[0].target : null;
}

/**
 * Graph symbols record their source file as a basename inside the package
 * directory; the diff names files from the repo root.
 */
function filePathOf(componentId: string, sourceFile?: string): string {
  if (!sourceFile) return '';
  if (!componentId || componentId === '.') return sourceFile;
  return `${componentId}/${sourceFile}`;
}

function packageOf(filePath: string): string {
  const cut = filePath.lastIndexOf('/');
  return cut < 0 ? '.' : filePath.slice(0, cut);
}

/** Names carry generics and signatures on some rows; the identifier is the head. */
function bareName(name: string): string {
  const match = /^[A-Za-z_][A-Za-z0-9_]*/.exec(name.trim());
  return match ? match[0] : '';
}

const TAG = /(<[^>]*>)/;
const ENTITY = /(&[a-zA-Z][a-zA-Z0-9]*;|&#\d+;|&#[xX][0-9a-fA-F]+;)/;
const IDENTIFIER = /[A-Za-z_][A-Za-z0-9_]*/g;

/**
 * Wraps the identifiers `known` accepts in a clickable span, leaving the
 * highlighter's own markup untouched. Works on the HTML string rather than
 * the DOM so the diff keeps rendering one `dangerouslySetInnerHTML` per line
 * instead of a React tree per token.
 */
export function markSymbols(html: string, known: (name: string) => boolean): string {
  if (!html) return html;
  return html
    .split(TAG)
    .map((chunk) => (chunk.startsWith('<') ? chunk : markText(chunk, known)))
    .join('');
}

function markText(text: string, known: (name: string) => boolean): string {
  if (!text) return text;
  return text
    .split(ENTITY)
    .map((part) =>
      part.startsWith('&') && part.endsWith(';')
        ? part
        : part.replace(IDENTIFIER, (name) =>
            known(name) ? `<span class="hf-code-sym" data-sym="${name}">${name}</span>` : name
          )
    )
    .join('');
}
