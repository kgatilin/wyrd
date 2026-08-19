import { searchSymbols } from '../data/search';
import type { SearchPort } from '../domain/ports';

/** SearchPort backed by the daemon's retrieval API. */
export function createHttpSearchSource(): SearchPort {
  return { search: (query, options) => searchSymbols(query, options) };
}
