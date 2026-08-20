import { callLens } from '../data/lens';
import type { LensPort } from '../domain/ports';

/** LensPort backed by the daemon's MCP tool endpoint. */
export function createHttpLensSource(): LensPort {
  return { call: (name, args, options) => callLens(name, args, options) };
}
