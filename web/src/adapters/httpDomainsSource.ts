import { fetchDomains } from '../data/domains';
import type { DomainsPort } from '../domain/ports';

/** DomainsPort backed by the daemon's domains endpoint. */
export function createHttpDomainsSource(): DomainsPort {
  return { load: (scope, options) => fetchDomains(scope, options) };
}
