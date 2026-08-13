// Pure UI state that needs to survive a component remount but isn't worth a
// full backend-backed store. accountsTab specifically: AccountsView is
// unmounted whenever the left nav switches to a different section (App.vue's
// v-else-if chain tears down the previous view), so a ref local to
// AccountsView would reset to its default every time the user navigates away
// and back. Keeping it here instead makes the tab choice stick for the rest
// of the session.
import { defineStore } from 'pinia'

export const useUiStore = defineStore('ui', {
  state: () => ({
    accountsTab: 'provisioning',
  }),
})
