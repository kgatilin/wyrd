import { fetchEventModel } from '../data/eventModel';
import type { EventModelPort } from '../domain/ports';

/** EventModelPort backed by the events plugin's endpoint. */
export function createHttpEventModelSource(): EventModelPort {
  return { load: (options) => fetchEventModel(options) };
}
