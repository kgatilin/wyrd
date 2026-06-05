import { describe, expect, it } from 'vitest';
import { displaySymbolName, shortSymbolName } from './symbolNames';

describe('symbolNames', () => {
  it('keeps full symbol names when inline signatures are enabled', () => {
    expect(displaySymbolName('NewClient(ctx context.Context) (*Client, error)', true)).toBe(
      'NewClient(ctx context.Context) (*Client, error)'
    );
  });

  it('shortens common Go signatures for compact inline display', () => {
    expect(shortSymbolName('NewClient(ctx context.Context) (*Client, error)')).toBe('NewClient');
    expect(shortSymbolName('func (c *Client) Do(ctx context.Context) error')).toBe('Do');
    expect(shortSymbolName('DefaultClient : Client')).toBe('DefaultClient');
    expect(shortSymbolName('const DefaultTimeout = 30')).toBe('DefaultTimeout');
  });
});
