// Pure UI state that needs to survive a component remount but isn't worth a
// full backend-backed store. accountsTab specifically: AccountsView is
// unmounted whenever the left nav switches to a different section (App.vue's
// v-else-if chain tears down the previous view), so a ref local to
// AccountsView would reset to its default every time the user navigates away
// and back. Keeping it here instead makes the tab choice stick for the rest
// of the session.
//
// rpcTab is the same pattern for RpcPanel's [traces]/[messages] toggle
// (Phase 28g, BR-035) — RpcPanel is torn down by the same App.vue v-else-if
// chain, so this needs to live outside the component too. "traces" is the
// default per ARCHITECTURE-ADMIN.md §4.5 ("Traces (the default, Phase 28)").
//
// traceRailWidth is the same pattern again, for TraceWaterfall's draggable
// trace-list rail — 420 is 50% wider than the panel's original fixed 280px.
//
// spanListHeight is the same pattern for TraceWaterfall's Span list / Span
// details vertical split (Phase 28j) — 260px default shows a handful of
// waterfall rows before the Span details card takes the rest.
import { defineStore } from 'pinia'

export const useUiStore = defineStore('ui', {
  state: () => ({
    accountsTab: 'provisioning',
    rpcTab: 'traces',
    traceRailWidth: 420,
    spanListHeight: 260,
  }),
})
