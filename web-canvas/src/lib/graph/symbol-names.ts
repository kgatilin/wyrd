/**
 * Symbol name display utilities.
 * Ported from web/src/domain/symbolNames.ts
 */

export function shortSymbolName(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return name;

  let symbol = trimmed
    .replace(/^func\s+/, '')
    .replace(/^(type|const|var)\s+/, '')
    .replace(/^\([^)]*\)\.?\s*/, '');

  const paren = symbol.indexOf('(');
  if (paren > 0) symbol = symbol.slice(0, paren);

  symbol = symbol.split(/\s*[:=]\s*/)[0]?.trim() ?? symbol;
  symbol = symbol.split(/\s+/)[0]?.trim() ?? symbol;
  return symbol || trimmed;
}

export function displaySymbolName(name: string, showInlineSignatures: boolean): string {
  return showInlineSignatures ? name : shortSymbolName(name);
}
