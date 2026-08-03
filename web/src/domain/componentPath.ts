/**
 * Header path display for package components. Many packages share a basename
 * (controller/, contract/ under different roots), so the card header shows the
 * full package path: a muted directory prefix plus the emphasized basename.
 * Past MAX_PREFIX_CHARS the prefix is middle-ellipsized keeping the first and
 * last segment ("internal/…/agent/"), so deep paths stay readable without
 * making cards absurdly wide; the full id belongs in the title tooltip.
 *
 * Shared by the card renderer and the layout engine so the estimated header
 * width always matches what is actually drawn.
 */
const MAX_PREFIX_CHARS = 34;

export function componentPathPrefix(id: string, name: string): string {
  if (!id || !id.includes('/')) return '';
  const dir = id.endsWith(`/${name}`)
    ? id.slice(0, id.length - name.length - 1)
    : id.slice(0, id.lastIndexOf('/'));
  if (!dir) return '';
  const prefix = `${dir}/`;
  if (prefix.length <= MAX_PREFIX_CHARS) return prefix;
  const segs = dir.split('/');
  if (segs.length <= 2) return prefix;
  return `${segs[0]}/…/${segs[segs.length - 1]}/`;
}
