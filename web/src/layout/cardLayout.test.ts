import { describe, expect, it } from 'vitest';
import { CARD_LAYOUT_METRICS, layoutCard } from './cardLayout';
import { buildCardModel } from '../domain/cardModel';
import type { Internal, SymbolRelation } from '../types';

function internal(over: Partial<Internal> & Pick<Internal, 'id' | 'kind' | 'name'>): Internal {
  return { members: [], ...over };
}

const OPTS = { showRows: true, showTypes: true, minWidth: 900 };

describe('layoutCard', () => {
  it('places blocks inside their file container', async () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'a.go' }),
      ]);

    const laid = await layoutCard(files, [], OPTS);

    expect(laid.files).toHaveLength(1);
    const [file] = laid.files;
    // Blocks are positioned relative to their file, below its header strip.
    for (const block of file.blocks) {
      expect(block.x).toBeGreaterThanOrEqual(CARD_LAYOUT_METRICS.FILE_PAD);
      expect(block.y).toBeGreaterThanOrEqual(CARD_LAYOUT_METRICS.FILE_HEADER_H);
      expect(block.x! + block.w!).toBeLessThanOrEqual(file.w!);
      expect(block.y! + block.h!).toBeLessThanOrEqual(file.h!);
    }
  });

  it('stacks blocks without overlapping', async () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'a.go' }),
        internal({ id: 'p.C', kind: 'class', name: 'C', sourceFile: 'a.go' }),
      ]);

    const [file] = (await layoutCard(files, [], OPTS)).files;
    const sorted = [...file.blocks].sort((a, b) => a.y! - b.y!);
    for (let i = 1; i < sorted.length; i++) {
      expect(sorted[i].y!).toBeGreaterThanOrEqual(sorted[i - 1].y! + sorted[i - 1].h!);
    }
  });

  it('grows a block to fit its rows', async () => {
    const narrow = buildCardModel([internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' })]);
    const wide = buildCardModel([
        internal({
          id: 'p.A',
          kind: 'class',
          name: 'A',
          sourceFile: 'a.go',
          members: [
            {
              id: 'p.A.Read',
              kind: 'method',
              name: 'Read',
              params: 'ctx context.Context, paths []string',
              type: '([]domain.PackageModel, error)',
            },
          ],
        }),
      ]);

    const narrowBlock = (await layoutCard(narrow, [], OPTS)).files[0].blocks[0];
    const wideBlock = (await layoutCard(wide, [], OPTS)).files[0].blocks[0];

    expect(wideBlock.w!).toBeGreaterThan(narrowBlock.w!);
    // One header + one row.
    expect(wideBlock.h!).toBeGreaterThan(narrowBlock.h!);
  });

  it('keeps rows out of the height when bodies are hidden', async () => {
    const files = buildCardModel([
        internal({
          id: 'p.A',
          kind: 'class',
          name: 'A',
          sourceFile: 'a.go',
          members: [
            { id: 'p.A.X', kind: 'prop', name: 'X', type: 'string' },
            { id: 'p.A.Y', kind: 'prop', name: 'Y', type: 'string' },
          ],
        }),
      ]);

    const withRows = (await layoutCard(files, [], OPTS)).files[0].blocks[0];
    const withoutRows = (await layoutCard(files, [], { ...OPTS, showRows: false })).files[0].blocks[0];

    expect(withoutRows.h!).toBe(CARD_LAYOUT_METRICS.BLOCK_HEADER_H);
    expect(withRows.h!).toBe(
      CARD_LAYOUT_METRICS.BLOCK_HEADER_H + 2 * CARD_LAYOUT_METRICS.ROW_H + 2 * CARD_LAYOUT_METRICS.ROW_LIST_PAD_V
    );
  });

  it('narrows a block when the type column is hidden', async () => {
    const files = buildCardModel([
        internal({
          id: 'p.A',
          kind: 'class',
          name: 'A',
          sourceFile: 'a.go',
          members: [{ id: 'p.A.X', kind: 'prop', name: 'X', type: 'map[string][]domain.PackageModel' }],
        }),
      ]);

    const withTypes = (await layoutCard(files, [], OPTS)).files[0].blocks[0];
    const withoutTypes = (await layoutCard(files, [], { ...OPTS, showTypes: false })).files[0].blocks[0];

    expect(withoutTypes.w!).toBeLessThan(withTypes.w!);
  });

  it('wraps file containers once a row exceeds the available width', async () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'b.go' }),
        internal({ id: 'p.C', kind: 'class', name: 'C', sourceFile: 'c.go' }),
      ]);

    const laid = await layoutCard(files, [], { ...OPTS, minWidth: 200 });

    // Narrow card: containers cannot all sit on one row.
    expect(new Set(laid.files.map((f) => f.y)).size).toBeGreaterThan(1);
  });

  it('routes through ELK when symbols reference each other', async () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'b.go' }),
      ]);
    const relations: SymbolRelation[] = [
      {
        id: 'r1',
        kind: 'uses',
        fromComponentId: 'p',
        fromInternalId: 'p.A',
        toComponentId: 'p',
        toInternalId: 'p.B',
      },
    ];

    const laid = await layoutCard(files, relations, OPTS);

    expect(laid.files).toHaveLength(2);
    for (const file of laid.files) {
      expect(file.w).toBeGreaterThan(0);
      expect(file.h).toBeGreaterThan(0);
      expect(file.blocks[0].w).toBeGreaterThan(0);
    }
    // Containers must not overlap each other.
    const [first, second] = [...laid.files].sort((a, b) => a.y! - b.y! || a.x! - b.x!);
    const disjoint =
      first.y! + first.h! <= second.y! ||
      first.x! + first.w! <= second.x! ||
      second.x! + second.w! <= first.x!;
    expect(disjoint).toBe(true);
  });

  it('ignores relations that stay inside one block', async () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'const', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'const', name: 'B', sourceFile: 'a.go' }),
      ]);
    const relations: SymbolRelation[] = [
      {
        id: 'r1',
        kind: 'uses',
        fromComponentId: 'p',
        fromInternalId: 'p.A',
        toComponentId: 'p',
        toInternalId: 'p.B',
      },
    ];

    // Both constants fold into one block, so there is no edge to route and the
    // deterministic pack is used — a single container at the origin.
    const laid = await layoutCard(files, relations, OPTS);
    expect(laid.files).toHaveLength(1);
    expect(laid.files[0].x).toBe(0);
    expect(laid.files[0].y).toBe(0);
  });

  it('returns empty geometry for a package with no symbols', async () => {
    const laid = await layoutCard([], [], OPTS);
    expect(laid).toEqual({ files: [], contentW: 0, contentH: 0 });
  });
});

describe('card size', () => {
  it('keeps a file container as wide as its widest class shape', async () => {
    // Six symbols referencing each other inside one file: a layered layout
    // would spread them sideways; a single ordered column must not.
    const internals: Internal[] = ['A', 'B', 'C', 'D', 'E', 'F'].map((name) =>
      internal({ id: `p.${name}`, kind: 'class', name, sourceFile: 'a.go' })
    );
    const relations: SymbolRelation[] = [
      ['A', 'B'],
      ['A', 'C'],
      ['A', 'D'],
      ['B', 'E'],
      ['C', 'F'],
    ].map(([from, to], i) => ({
      id: `r${i}`,
      kind: 'uses',
      fromComponentId: 'p',
      fromInternalId: `p.${from}`,
      toComponentId: 'p',
      toInternalId: `p.${to}`,
    }));

    const files = buildCardModel(internals);
    const laid = await layoutCard(files, relations, OPTS);
    const [file] = laid.files;
    const widest = Math.max(...file.blocks.map((b) => b.w!));

    expect(file.w!).toBeLessThanOrEqual(widest + 2 * CARD_LAYOUT_METRICS.FILE_PAD);
    // One column: every shape shares the same left edge.
    expect(new Set(file.blocks.map((b) => b.x)).size).toBe(1);
  });

  it('orders class shapes so a symbol sits above what it references', async () => {
    const internals: Internal[] = ['Leaf', 'Mid', 'Root'].map((name) =>
      internal({ id: `p.${name}`, kind: 'class', name, sourceFile: 'a.go' })
    );
    const relations: SymbolRelation[] = [
      ['Root', 'Mid'],
      ['Mid', 'Leaf'],
    ].map(([from, to], i) => ({
      id: `r${i}`,
      kind: 'uses',
      fromComponentId: 'p',
      fromInternalId: `p.${from}`,
      toComponentId: 'p',
      toInternalId: `p.${to}`,
    }));

    const laid = await layoutCard(buildCardModel(internals), relations, OPTS);
    const byName = new Map(laid.files[0].blocks.map((b) => [b.name, b.y!]));

    expect(byName.get('Root')!).toBeLessThan(byName.get('Mid')!);
    expect(byName.get('Mid')!).toBeLessThan(byName.get('Leaf')!);
  });

  it('survives a reference cycle without dropping a shape', async () => {
    const internals: Internal[] = ['A', 'B'].map((name) =>
      internal({ id: `p.${name}`, kind: 'class', name, sourceFile: 'a.go' })
    );
    const relations: SymbolRelation[] = [
      ['A', 'B'],
      ['B', 'A'],
    ].map(([from, to], i) => ({
      id: `r${i}`,
      kind: 'uses',
      fromComponentId: 'p',
      fromInternalId: `p.${from}`,
      toComponentId: 'p',
      toInternalId: `p.${to}`,
    }));

    const laid = await layoutCard(buildCardModel(internals), relations, OPTS);
    expect(laid.files[0].blocks.map((b) => b.name).sort()).toEqual(['A', 'B']);
  });
});
