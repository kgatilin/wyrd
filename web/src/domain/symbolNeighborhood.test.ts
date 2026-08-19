import { describe, expect, it } from 'vitest';
import type { SymbolRelation, UIGraph } from '../types';
import { buildNeighborhood, toTarget } from './symbolNeighborhood';

function graph(relations: SymbolRelation[]): UIGraph {
  return {
    schema: 'test',
    boundedContexts: [],
    comments: [],
    edges: [],
    relations,
    components: [
      {
        id: 'controllers',
        name: 'controllers',
        tech: 'go',
        desc: '',
        bc: 'core',
        ports: [],
        internals: [
          {
            id: 'controllers.ValidateDeclaration',
            kind: 'func',
            name: 'ValidateDeclaration',
            exported: true,
            members: [],
          },
          { id: 'controllers.validateKind', kind: 'func', name: 'validateKind', exported: false, members: [] },
          {
            id: 'controllers.Declaration',
            kind: 'class',
            name: 'Declaration',
            exported: true,
            members: [
              { id: 'controllers.Declaration.Compile', kind: 'method', name: 'Compile', exported: true },
              { id: 'controllers.Declaration.name', kind: 'prop', name: 'name', exported: false },
            ],
          },
        ],
      },
      {
        id: 'agent',
        name: 'agent',
        tech: 'go',
        desc: '',
        bc: 'core',
        ports: [],
        internals: [
          { id: 'agent.New', kind: 'func', name: 'New', exported: true, members: [] },
          { id: 'agent.Session', kind: 'class', name: 'Session', exported: true, members: [] },
        ],
      },
    ],
  };
}

function relation(over: Partial<SymbolRelation> & { id: string; kind: string }): SymbolRelation {
  return { fromComponentId: 'controllers', toComponentId: 'controllers', ...over };
}

const anchor = { componentId: 'controllers', internalId: 'controllers.ValidateDeclaration' };

describe('buildNeighborhood', () => {
  it('keeps only direct neighbours, never the transitive closure', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'calls',
          fromInternalId: 'controllers.ValidateDeclaration',
          toInternalId: 'controllers.validateKind',
        }),
        // Two hops out: reachable, but not a first-level neighbour.
        relation({
          id: 'r2',
          kind: 'calls',
          fromInternalId: 'controllers.validateKind',
          toComponentId: 'agent',
          toInternalId: 'agent.New',
        }),
      ]),
      anchor
    );

    expect(model.outgoing.flatMap((group) => group.links.map((link) => link.symbol.label))).toEqual(['validateKind']);
    expect(model.counts.outgoing).toBe(1);
  });

  it('splits incoming from outgoing and groups both by package', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'calls',
          fromComponentId: 'agent',
          fromInternalId: 'agent.New',
          toInternalId: 'controllers.ValidateDeclaration',
        }),
        relation({
          id: 'r2',
          kind: 'uses',
          fromInternalId: 'controllers.ValidateDeclaration',
          toComponentId: 'agent',
          toInternalId: 'agent.Session',
        }),
        relation({
          id: 'r3',
          kind: 'calls',
          fromInternalId: 'controllers.ValidateDeclaration',
          toInternalId: 'controllers.validateKind',
        }),
      ]),
      anchor
    );

    expect(model.incoming.map((group) => group.packageName)).toEqual(['agent']);
    expect(model.incoming[0].links[0].symbol.label).toBe('New');
    // Cross-package groups sort ahead of the anchor's own package.
    expect(model.outgoing.map((group) => [group.packageName, group.crossPackage])).toEqual([
      ['agent', true],
      ['controllers', false],
    ]);
    expect(model.counts).toMatchObject({ incoming: 1, outgoing: 2, crossPackage: 2, packages: 2 });
  });

  it('collapses several relations to one neighbour into one link', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'uses',
          fromInternalId: 'controllers.ValidateDeclaration',
          toComponentId: 'agent',
          toInternalId: 'agent.Session',
        }),
        relation({
          id: 'r2',
          kind: 'returns',
          fromInternalId: 'controllers.ValidateDeclaration',
          toComponentId: 'agent',
          toInternalId: 'agent.Session',
        }),
      ]),
      anchor
    );

    const links = model.outgoing.flatMap((group) => group.links);
    expect(links).toHaveLength(1);
    expect(links[0].kinds).toEqual(['returns', 'uses']);
  });

  it('rolls a type’s member edges up to the type and records the member', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'calls',
          fromInternalId: 'controllers.Declaration',
          fromMemberId: 'controllers.Declaration.Compile',
          toComponentId: 'agent',
          toInternalId: 'agent.New',
        }),
      ]),
      { componentId: 'controllers', internalId: 'controllers.Declaration' }
    );

    const [link] = model.outgoing.flatMap((group) => group.links);
    expect(link.symbol.label).toBe('New');
    expect(link.via).toEqual(['Compile']);
  });

  it('scopes to the member alone when a member is focused', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'calls',
          fromInternalId: 'controllers.Declaration',
          fromMemberId: 'controllers.Declaration.Compile',
          toComponentId: 'agent',
          toInternalId: 'agent.New',
        }),
        relation({
          id: 'r2',
          kind: 'uses',
          fromInternalId: 'controllers.Declaration',
          fromMemberId: 'controllers.Declaration.name',
          toComponentId: 'agent',
          toInternalId: 'agent.Session',
        }),
      ]),
      {
        componentId: 'controllers',
        internalId: 'controllers.Declaration',
        memberId: 'controllers.Declaration.Compile',
      }
    );

    expect(model.outgoing.flatMap((group) => group.links.map((link) => link.symbol.label))).toEqual(['New']);
    expect(model.outgoing.flatMap((group) => group.links.map((link) => link.via))).toEqual([[]]);
  });

  it('drops wiring internal to the focused type', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'uses',
          fromInternalId: 'controllers.Declaration',
          fromMemberId: 'controllers.Declaration.Compile',
          toInternalId: 'controllers.Declaration',
        }),
      ]),
      { componentId: 'controllers', internalId: 'controllers.Declaration' }
    );

    expect(model.counts).toMatchObject({ incoming: 0, outgoing: 0 });
  });

  it('keeps an endpoint the loaded graph does not contain, but marks it unwalkable', () => {
    const model = buildNeighborhood(
      graph([
        relation({
          id: 'r1',
          kind: 'calls',
          fromInternalId: 'controllers.ValidateDeclaration',
          toComponentId: 'storage',
          toInternalId: 'storage.Put',
          toLabel: 'Put',
        }),
      ]),
      anchor
    );

    const [link] = model.outgoing.flatMap((group) => group.links);
    expect(link.symbol).toMatchObject({ label: 'Put', packageName: 'storage', navigable: false });
    expect(link.crossPackage).toBe(true);
  });

  it('pairs interface methods with the concrete methods that implement them', () => {
    const model = buildNeighborhood(
      {
        ...graph([
          relation({
            id: 'r1',
            kind: 'implements',
            fromInternalId: 'controllers.Declaration',
            toComponentId: 'agent',
            toInternalId: 'agent.Compiler',
          }),
        ]),
        components: [
          ...graph([]).components.slice(0, 1),
          {
            id: 'agent',
            name: 'agent',
            tech: 'go',
            desc: '',
            bc: 'core',
            ports: [],
            internals: [
              {
                id: 'agent.Compiler',
                kind: 'iface',
                name: 'Compiler',
                exported: true,
                members: [{ id: 'agent.Compiler.Compile', kind: 'method', name: 'Compile', exported: true }],
              },
            ],
          },
        ],
      },
      {
        componentId: 'controllers',
        internalId: 'controllers.Declaration',
        memberId: 'controllers.Declaration.Compile',
      }
    );

    const [link] = model.outgoing.flatMap((group) => group.links);
    expect(link.kinds).toEqual(['implements']);
    expect(link.symbol.id).toBe('agent.Compiler.Compile');
  });

  it('returns an empty model when the focused symbol is not in the graph', () => {
    const model = buildNeighborhood(graph([]), { componentId: 'controllers', internalId: 'controllers.Gone' });
    expect(model.anchor).toBeNull();
  });
});

describe('toTarget', () => {
  it('re-anchors on a member with its owning type', () => {
    expect(
      toTarget({
        id: 'agent.Session.Close',
        componentId: 'agent',
        packageName: 'agent',
        internalId: 'agent.Session',
        memberId: 'agent.Session.Close',
        label: 'Close',
        kind: 'method',
        navigable: true,
      })
    ).toEqual({ componentId: 'agent', internalId: 'agent.Session', memberId: 'agent.Session.Close' });
  });
});
