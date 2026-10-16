// Pure UI state that needs to survive a component remount but isn't worth a
// full backend-backed store. accountsTab specifically: AccountsView is
// unmounted whenever the left nav switches to a different section (App.vue's
// v-else-if chain tears down the previous view), so a ref local to
// AccountsView would reset to its default every time the user navigates away
// and back. Keeping it here instead makes the tab choice stick for the rest
// of the session.
//
// rpcTab is the same pattern for RpcPanel's [pulse]/[traces]/[messages]
// toggle (Phase 28g, BR-035; pulse tab added Phase 44) — RpcPanel is torn
// down by the same App.vue v-else-if chain, so this needs to live outside
// the component too. "traces" is the default since Phase 54c: Pulse is
// marked unavailable until Phase 55 (decision 15), so the panel must not
// open on an empty tab. Pulse keeps its place first in the tab bar.
//
// traceFocusId is a trace another panel asked to open (Phase 54c, the
// Messages-to-trace link). TraceWaterfall loads it by ID through
// trace.get, selects it, and clears this value. openTrace() sets both it
// and rpcTab, so the caller only has to switch the nav to Request/Reply.
//
// traceRailWidth is the same pattern again, for TraceWaterfall's draggable
// trace-list rail — 420 is 50% wider than the panel's original fixed 280px.
//
// frontendShellTab is the same pattern again, for FrontendShellView's
// [Plugins]/[Registry Audit] tabs (Phase 2) — the two panels were separate
// nav items until they were collapsed under one "Frontend Shell" entry.
//
// spanListHeight is the same pattern for TraceWaterfall's Span list / Span
// details vertical split (Phase 28j) — 260px default shows a handful of
// waterfall rows before the Span details card takes the rest.
import { defineStore } from 'pinia'

export const useUiStore = defineStore('ui', {
  state: () => ({
    accountsTab: 'overview',
    frontendShellTab: 'plugins',
    rpcTab: 'traces',
    traceFocusId: '',
    traceRailWidth: 420,
    spanListHeight: 260,
  }),
  actions: {
    openTrace(traceId) {
      this.traceFocusId = traceId
      this.rpcTab = 'traces'
    },
  },
})
