import OverviewView from './views/OverviewView.vue'

// Delay module evaluation, before the shell calls activate (BR-AS08).
export const LOAD_DELAY_MS = 6000
await new Promise((resolve) => setTimeout(resolve, LOAD_DELAY_MS))

export const components = { overview: OverviewView }

export function activate() {
}
