/**
 * Where a symbol is declared, and the code that declares it.
 *
 * The wiring panel answers what is *around* a symbol. Read from a patch, that
 * leaves the first question unanswered: a name in someone else's file says
 * nothing about what it is until you have gone and found its declaration. The
 * daemon's retrieval graph already carries every symbol's span — the same
 * index `search` and the MCP `get_node` tool read — so the declaration is one
 * lookup away from the id the panel is already anchored on.
 */
export interface SymbolDefinition {
  /** Retrieval node the declaration was read from. */
  nodeId: string;
  kind: string;
  packageName: string;
  /** `Name`, or `Receiver.Name` for a method. */
  name: string;
  /** Module-relative path of the declaring file. */
  file: string;
  /** 1-based line the declaration starts on; 0 when the span is unknown. */
  line: number;
  /** One-line form of the declaration, as the indexer recorded it. */
  signature?: string;
  /** Doc comment, already stripped of its comment markers. */
  doc?: string;
  /** The declaration's own source text. Absent when the span could not be read. */
  body?: string;
}

/** The panel's anchor, reduced to the ids a definition can be looked up by. */
export interface DefinitionAnchor {
  id: string;
  internalId: string;
  memberId?: string;
}

/**
 * Node ids to ask the daemon for, best first.
 *
 * A method is a node of its own, so the clicked id resolves directly. A field
 * and an interface method are not: their text lives inside the type that
 * declares them and the graph records no node for either. Falling back to the
 * declaring type still answers the question that was asked — where is this
 * written — where reporting nothing would not.
 */
export function definitionLookupIds(anchor: DefinitionAnchor): string[] {
  const ids = [anchor.id];
  if (anchor.internalId && !ids.includes(anchor.internalId)) ids.push(anchor.internalId);
  return ids.filter((id) => id !== '');
}

/** `file:line`, or the bare path when the span carries no line. */
export function definitionLocation(definition: SymbolDefinition): string {
  if (!definition.file) return '';
  return definition.line > 0 ? `${definition.file}:${definition.line}` : definition.file;
}

/**
 * True when the declaration on screen belongs to the type that contains the
 * anchor rather than to the anchor itself — the field / interface-method
 * fallback. The panel says so rather than letting a struct's body read as the
 * field's own declaration.
 */
export function isDeclaringTypeFallback(definition: SymbolDefinition, anchor: DefinitionAnchor): boolean {
  return definition.nodeId !== anchor.id;
}
