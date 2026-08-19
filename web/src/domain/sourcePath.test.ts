import { describe, expect, it } from 'vitest';
import { sourceFilePath } from './sourcePath';

describe('sourceFilePath', () => {
  it('prefixes a bare file name with the package path', () => {
    expect(sourceFilePath('internal/adapter/mcp', 'tools.go')).toBe('internal/adapter/mcp/tools.go');
  });

  it('keeps a path that already carries its directory', () => {
    expect(sourceFilePath('internal/adapter/mcp', 'internal/adapter/mcp/tools.go')).toBe(
      'internal/adapter/mcp/tools.go'
    );
  });

  it('leaves a root-package file unprefixed', () => {
    expect(sourceFilePath('.', 'main.go')).toBe('main.go');
    expect(sourceFilePath('', 'main.go')).toBe('main.go');
  });

  it('has no path for a symbol with no source file', () => {
    expect(sourceFilePath('pkg', undefined)).toBeNull();
    expect(sourceFilePath('pkg', '  ')).toBeNull();
  });
});
