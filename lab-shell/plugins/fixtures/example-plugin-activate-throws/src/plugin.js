import OverviewView from './views/OverviewView.vue'

export const components = { overview: OverviewView }

export function activate() {
  throw new Error('example-plugin-activate-throws: deliberate failure inside activate()')
}
