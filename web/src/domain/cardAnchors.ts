import type { Component } from '../types';
import { CARD_LAYOUT_METRICS } from '../layout/cardLayout';
import type { CardBlock, CardFile } from './cardModel';

/**
 * Where a symbol sits inside a laid-out package card.
 *
 * Both the arrows drawn inside a card and the cross-package symbol relations
 * need the same answer, and getting it from two places is how anchors drift
 * apart. Coordinates are relative to the card's canvas — the area below the
 * component header — because that is the box the file containers are
 * positioned in.
 */

/** Height of the component header strip above the card canvas. */
export const COMPONENT_HEADER_H = 36;

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** The block holding a symbol, with its file, or null when the card is collapsed. */
export function findBlock(
  cmp: Component,
  internalId: string
): { file: CardFile; block: CardBlock } | null {
  for (const file of cmp.files ?? []) {
    for (const block of file.blocks) {
      if (block.internalIds.includes(internalId)) return { file, block };
    }
  }
  return null;
}

/** A symbol's block in card-canvas coordinates. */
export function blockRect(cmp: Component, internalId: string): Rect | null {
  const found = findBlock(cmp, internalId);
  if (!found) return null;
  const { file, block } = found;
  if (file.x == null || file.y == null || block.x == null || block.y == null) return null;
  return {
    x: file.x + block.x,
    y: file.y + block.y,
    w: block.w ?? 0,
    h: block.h ?? 0,
  };
}

/**
 * A single row of a class body in card-canvas coordinates, or null when the
 * row is not rendered (bodies hidden, or the id belongs to no row).
 */
export function rowRect(cmp: Component, internalId: string, rowId: string): Rect | null {
  const found = findBlock(cmp, internalId);
  const rect = blockRect(cmp, internalId);
  if (!found || !rect) return null;
  const index = found.block.rows.findIndex((row) => row.id === rowId);
  if (index < 0) return null;
  // A block laid out with bodies hidden is exactly one header tall.
  if (rect.h <= CARD_LAYOUT_METRICS.BLOCK_HEADER_H) return null;
  return {
    x: rect.x,
    y:
      rect.y +
      CARD_LAYOUT_METRICS.BLOCK_HEADER_H +
      CARD_LAYOUT_METRICS.ROW_LIST_PAD_V +
      index * CARD_LAYOUT_METRICS.ROW_H,
    w: rect.w,
    h: CARD_LAYOUT_METRICS.ROW_H,
  };
}

/**
 * The best anchor for a symbol reference: the member row when one is named and
 * rendered, otherwise the block header, otherwise nothing.
 */
export function symbolAnchor(
  cmp: Component,
  internalId: string,
  memberId?: string
): { x: number; y: number } | null {
  if (memberId) {
    const row = rowRect(cmp, internalId, memberId);
    // Anchor a row on its right edge, where the type column ends.
    if (row) return { x: row.x + row.w - 8, y: row.y + row.h / 2 };
  }
  const block = blockRect(cmp, internalId);
  if (!block) return null;
  return {
    x: block.x + block.w / 2,
    y: block.y + Math.min(block.h / 2, CARD_LAYOUT_METRICS.BLOCK_HEADER_H / 2),
  };
}
