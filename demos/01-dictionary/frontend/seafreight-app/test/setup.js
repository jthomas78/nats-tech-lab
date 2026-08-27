// Vitest's happy-dom environment copies the happy-dom window's *own* properties
// onto globalThis, but `localStorage` and `sessionStorage` are accessor
// properties on BrowserWindow.prototype rather than own properties (happy-dom
// v20 / vitest v4). They are therefore never copied, and both read back as
// undefined even though `environment: 'happy-dom'` is set and the rest of the
// DOM is present — `window.Storage` itself lands on globalThis just fine, so it
// is only the two bindings that are missing.
//
// Reading `globalThis.localStorage` then falls through to Node's own
// experimental localStorage, which is inert without --localstorage-file and
// answers `undefined` after emitting an ExperimentalWarning — which is why the
// failure surfaced as `Cannot read properties of undefined (reading 'clear')`
// rather than a missing global.
//
// So bind them here from happy-dom's real Storage class. This is the same
// implementation a browser test would exercise, not a stand-in, and each test
// file gets its own instances because setup files re-run per file.
//
// Deliberately unconditional: Node's inert global is already present and would
// satisfy an `if (!globalThis.localStorage)` guard on the property-exists
// check while still being unusable. Redefining is also what keeps
// vi.stubGlobal('localStorage', …) working (see src/requestorId.spec.js) —
// the properties stay configurable and writable.
for (const name of ['localStorage', 'sessionStorage']) {
  Object.defineProperty(globalThis, name, {
    value: new globalThis.Storage(),
    configurable: true,
    writable: true,
  })
}
