import { describe, expect, it } from 'vitest';
import { buildCardModel, isDiagramRelation, rowLabel, rowText, UNKNOWN_SOURCE_FILE } from './cardModel';
import type { Internal } from '../types';

function internal(over: Partial<Internal> & Pick<Internal, 'id' | 'kind' | 'name'>): Internal {
  return { members: [], ...over };
}

describe('buildCardModel', () => {
  it('groups symbols into source-file containers ordered by name', () => {
    const files = buildCardModel([
        internal({ id: 'p.Z', kind: 'class', name: 'Z', sourceFile: 'zeta.go' }),
        internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'alpha.go' }),
        internal({ id: 'p.M', kind: 'class', name: 'M', sourceFile: 'mid.go' }),
      ]);

    expect(files.map((f) => f.label)).toEqual(['alpha.go', 'mid.go', 'zeta.go']);
    expect(files[0].blocks.map((b) => b.name)).toEqual(['A']);
  });

  it('sorts the unknown-file bucket last', () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'class', name: 'A' }),
        internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'zeta.go' }),
      ]);

    expect(files.map((f) => f.label)).toEqual(['zeta.go', UNKNOWN_SOURCE_FILE]);
  });

  it('orders blocks within a file the way the D2 writer does', () => {
    const files = buildCardModel([
        internal({ id: 'p.C', kind: 'const', name: 'C', sourceFile: 'a.go' }),
        internal({ id: 'p.T', kind: 'type', name: 'T', sourceFile: 'a.go' }),
        internal({ id: 'p.F', kind: 'func', name: 'F', sourceFile: 'a.go' }),
        internal({ id: 'p.S', kind: 'class', name: 'S', sourceFile: 'a.go' }),
        internal({ id: 'p.I', kind: 'iface', name: 'I', sourceFile: 'a.go' }),
      ]);

    expect(files[0].blocks.map((b) => b.kind)).toEqual(['iface', 'class', 'func', 'type', 'consts']);
  });

  it('folds constants, variables and errors of one file into single blocks', () => {
    const files = buildCardModel([
        internal({ id: 'p.MaxA', kind: 'const', name: 'MaxA', type: 'int = 1', sourceFile: 'a.go' }),
        internal({ id: 'p.MaxB', kind: 'const', name: 'MaxB', type: 'int = 2', sourceFile: 'a.go' }),
        internal({ id: 'p.Reg', kind: 'var', name: 'Reg', type: '[]string', sourceFile: 'a.go' }),
        internal({ id: 'p.ErrX', kind: 'error', name: 'ErrX', type: '"boom"', sourceFile: 'a.go' }),
      ]);

    const blocks = files[0].blocks;
    expect(blocks.map((b) => b.name)).toEqual(['Constants', 'Variables', 'Errors']);

    const consts = blocks[0];
    expect(consts.rows.map((r) => [r.name, r.type])).toEqual([
      ['MaxA', 'int = 1'],
      ['MaxB', 'int = 2'],
    ]);
    // The aggregate keeps every symbol addressable for comments and focus.
    expect(consts.internalIds).toEqual(['p.MaxA', 'p.MaxB']);
    expect(consts.internalId).toBeUndefined();
  });

  it('keeps a constant in its own file container', () => {
    const files = buildCardModel([
        internal({ id: 'p.A', kind: 'const', name: 'A', sourceFile: 'a.go' }),
        internal({ id: 'p.B', kind: 'const', name: 'B', sourceFile: 'b.go' }),
      ]);

    expect(files.map((f) => f.label)).toEqual(['a.go', 'b.go']);
    expect(files[0].blocks[0].internalIds).toEqual(['p.A']);
    expect(files[1].blocks[0].internalIds).toEqual(['p.B']);
  });

  it('leads a type definition body with its underlying type, then its constants', () => {
    const files = buildCardModel([
        internal({
          id: 'p.Status',
          kind: 'type',
          name: 'Status',
          type: 'int',
          sourceFile: 'a.go',
          members: [{ id: 'p.Status.New', kind: 'const', name: 'StatusNew' }],
        }),
      ]);

    const rows = files[0].blocks[0].rows;
    expect(rows.map((r) => [r.kind, r.name, r.type])).toEqual([
      ['type', 'type', 'int'],
      ['const', 'StatusNew', undefined],
    ]);
  });

  it('lists struct fields before methods, as the D2 writer does', () => {
    // The projection sorts members by id, which interleaves the two kinds.
    const files = buildCardModel([
      internal({
        id: 'p.S',
        kind: 'class',
        name: 'S',
        sourceFile: 'a.go',
        members: [
          { id: 'p.S.Apply', kind: 'method', name: 'Apply', params: '', type: 'error' },
          { id: 'p.S.count', kind: 'prop', name: 'count', type: 'int' },
          { id: 'p.S.run', kind: 'method', name: 'run', params: '' },
          { id: 'p.S.zone', kind: 'prop', name: 'zone', type: 'string' },
        ],
      }),
    ]);

    expect(files[0].blocks[0].rows.map((r) => [r.kind, r.name])).toEqual([
      ['prop', 'count'],
      ['prop', 'zone'],
      ['method', 'Apply'],
      ['method', 'run'],
    ]);
  });

  it('lists function parameters before the return row', () => {
    const files = buildCardModel([
      internal({
        id: 'p.F',
        kind: 'func',
        name: 'F',
        sourceFile: 'a.go',
        members: [
          { id: 'p.F.return', kind: 'return', name: 'return', type: 'error' },
          { id: 'p.F.param.0', kind: 'param', name: 'ctx', type: 'context.Context' },
        ],
      }),
    ]);

    expect(files[0].blocks[0].rows.map((r) => r.kind)).toEqual(['param', 'return']);
  });

  it('carries the stereotype onto the block', () => {
    const files = buildCardModel([
        internal({ id: 'p.R', kind: 'iface', name: 'R', stereotype: 'repository', sourceFile: 'a.go' }),
        internal({ id: 'p.M', kind: 'class', name: 'M', sourceFile: 'a.go' }),
      ]);

    const blocks = files[0].blocks;
    expect(blocks.find((b) => b.name === 'R')?.stereotype).toBe('repository');
    expect(blocks.find((b) => b.name === 'M')?.stereotype).toBeUndefined();
  });

  describe('diff rollup', () => {
    it('reads a symbol as changed when only a member moved', () => {
      const files = buildCardModel([
          internal({
            id: 'p.S',
            kind: 'class',
            name: 'S',
            sourceFile: 'a.go',
            members: [{ id: 'p.S.F', kind: 'prop', name: 'F', diff: 'added' }],
          }),
        ]);

      expect(files[0].blocks[0].diff).toBe('changed');
    });

    it('keeps a unanimous state on the file container', () => {
      const files = buildCardModel([
          internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go', diff: 'added' }),
          internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'a.go', diff: 'added' }),
        ]);

      expect(files[0].diff).toBe('added');
    });

    it('reads a partially changed file as changed', () => {
      const files = buildCardModel([
          internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go', diff: 'added' }),
          internal({ id: 'p.B', kind: 'class', name: 'B', sourceFile: 'a.go' }),
        ]);

      expect(files[0].diff).toBe('changed');
    });

    it('leaves an untouched file undiffed', () => {
      const files = buildCardModel([internal({ id: 'p.A', kind: 'class', name: 'A', sourceFile: 'a.go' })]);

      expect(files[0].diff).toBeUndefined();
    });
  });
});

describe('row text', () => {
  it('renders a method with its parameter list and return column', () => {
    const row = {
      id: 'm',
      kind: 'method' as const,
      name: 'Read',
      params: 'ctx context.Context, paths []string',
      type: '([]domain.PackageModel, error)',
      internalId: 'p.S',
    };
    expect(rowLabel(row)).toBe('Read(ctx context.Context, paths []string)');
    expect(rowText(row)).toBe('Read(ctx context.Context, paths []string) ([]domain.PackageModel, error)');
  });

  it('renders a parameterless method with empty parentheses', () => {
    expect(rowLabel({ id: 'm', kind: 'method', name: 'Close', internalId: 'p.S' })).toBe('Close()');
  });

  it('renders an unnamed parameter as its type alone', () => {
    const row = { id: 'p', kind: 'param' as const, name: '', type: 'string', internalId: 'p.F' };
    expect(rowText(row)).toBe('string');
  });

  it('renders a field as name and type', () => {
    const row = { id: 'f', kind: 'prop' as const, name: 'Paths', type: '[]string', internalId: 'p.S' };
    expect(rowText(row)).toBe('Paths []string');
  });
});

describe('isDiagramRelation', () => {
  it('keeps the structural kinds the D2 writer draws', () => {
    for (const kind of ['uses', 'returns', 'implements', 'extends']) {
      expect(isDiagramRelation({ kind }), kind).toBe(true);
    }
  });

  it('drops call edges — the wiring overlay owns those', () => {
    expect(isDiagramRelation({ kind: 'calls' })).toBe(false);
  });
});
