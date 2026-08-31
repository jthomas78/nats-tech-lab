import MenuView from './views/MenuView.vue'
import DemoIntroView from './views/DemoIntroView.vue'
import { setShellApi } from './shellApi.js'

export const components = { catalog: MenuView, intro: DemoIntroView }
export { default as ExtensionRegion } from './ExtensionRegion.js'

export function activate(shellApi) {
  setShellApi(shellApi)
}
