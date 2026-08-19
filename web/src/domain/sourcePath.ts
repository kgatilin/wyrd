/**
 * Repo-relative path of a symbol's source file.
 *
 * The graph records `sourceFile` as a bare base name for most symbols and as
 * a path for a few, so the package id supplies the directory when it is
 * missing. Everything that has to name a file to the daemon — the source
 * drawer, the file diff — goes through here, so they cannot disagree about
 * which file a card is showing.
 */
export function sourceFilePath(componentId: string, sourceFile?: string): string | null {
  const file = (sourceFile ?? '').trim();
  if (!file) return null;
  if (file.includes('/')) return file;
  if (!componentId || componentId === '.') return file;
  return `${componentId}/${file}`;
}
