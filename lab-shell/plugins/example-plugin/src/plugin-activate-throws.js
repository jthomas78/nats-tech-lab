/*
  Failure switch (b) of BR-AS04 / task 1b-4: the remote loads fine and then
  throws inside activate().

  It is a separate exposed module rather than a flag on the healthy one so the
  failure is selected by a *curated registry entry* — an operator decision —
  and never by anything the browser could forge (BR-AS01).
*/

export { components } from './plugin.js'

export function activate() {
  throw new Error('example-plugin-activate-throws: deliberate failure inside activate()')
}
