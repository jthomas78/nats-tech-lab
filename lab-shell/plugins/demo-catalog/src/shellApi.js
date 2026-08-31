// Activation precedes rendering. This module belongs to this plugin bundle,
// so descendants can reach only the API the host deliberately supplied.
let shellApi

export function setShellApi(api) {
  if (api?.version !== 1 || !api.ui?.ExtensionRegion) {
    throw new Error('Demo Catalog requires shell API 1 with ui.ExtensionRegion')
  }
  shellApi = api
}

export function getShellApi() {
  if (!shellApi) throw new Error('Demo Catalog has not been activated')
  return shellApi
}
