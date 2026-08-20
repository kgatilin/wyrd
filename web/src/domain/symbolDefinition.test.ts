import { describe, expect, it } from 'vitest';
import {
  definitionLocation,
  definitionLookupIds,
  isDeclaringTypeFallback,
  type SymbolDefinition,
} from './symbolDefinition';

const definition = (over: Partial<SymbolDefinition> = {}): SymbolDefinition => ({
  nodeId: 'api.Store',
  kind: 'iface',
  packageName: 'api',
  name: 'Store',
  file: 'api/store.go',
  line: 12,
  ...over,
});

describe('definitionLookupIds', () => {
  it('asks for the symbol itself when the anchor is a top-level symbol', () => {
    expect(definitionLookupIds({ id: 'api.Store', internalId: 'api.Store' })).toEqual(['api.Store']);
  });

  it('falls back to the declaring type, which is where a field is written', () => {
    expect(
      definitionLookupIds({ id: 'api.Store.db', internalId: 'api.Store', memberId: 'api.Store.db' })
    ).toEqual(['api.Store.db', 'api.Store']);
  });
});

describe('definitionLocation', () => {
  it('reads as file:line', () => {
    expect(definitionLocation(definition())).toBe('api/store.go:12');
  });

  it('drops the line when the span carries none', () => {
    expect(definitionLocation(definition({ line: 0 }))).toBe('api/store.go');
  });
});

describe('isDeclaringTypeFallback', () => {
  it('is false when the declaration is its own', () => {
    expect(isDeclaringTypeFallback(definition(), { id: 'api.Store', internalId: 'api.Store' })).toBe(false);
  });

  it('is true when the type answered for one of its members', () => {
    expect(
      isDeclaringTypeFallback(definition(), {
        id: 'api.Store.db',
        internalId: 'api.Store',
        memberId: 'api.Store.db',
      })
    ).toBe(true);
  });
});
