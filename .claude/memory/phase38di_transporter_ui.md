# Phase 38c-i / 38d-i — Transporter UI and the error-envelope finding

Implemented 2026-08-20 in `frontend/refdata` against BR-TP29–39.

## Shape

- Transporters get a **dedicated `TransporterPanel.vue`** with a **drill-in
  detail view** (list is replaced, back-link breadcrumb), not a DataTable
  expansion row or a drawer. `TradingPartnersPanel.vue` stays for Shippers;
  its `partnerType` prop was kept rather than hard-coded, to avoid an
  unrelated edit.
- Tabs: Company Information, Fleet, Documents, Vetting, Rate Sheets. 38d-ii's
  Operating Areas and Tracking Credentials are **further tabs on this
  drill-in**, not new screens.
- The register wizard **commits at step 1** — fleet and documents need an
  existing ID — and says so, rather than surprising the operator on cancel.
- Rejection renders as the *review step failing*, not a fourth stepper step,
  so the stepper never implies rejection follows vetting.
- Vetting state is read **only** from `partner.profile.get`, one call per list
  row via `Promise.allSettled`. The N+1 is deliberate: a list-level join would
  put vetting state on the wrong side of the aggregate boundary, and settling
  per-row means one bad row cannot blank the table.

## The finding: error-envelope discriminators are the contract

`frontend/refdata/src/nats/connectionFactory.js`'s `request()` threw
`new Error(body.error)` and **discarded `conflict` and `notFound`**. So a 409
was indistinguishable from a 500 **in every frontend in this repo**, and
BR-TP39's conflict banner had nothing to trigger on. Fixed additively (flags
copied onto the Error, defaulting false).

The durable lesson: **branch on the envelope's boolean flags, never on message
prose.** A reworded backend error would otherwise silently downgrade a
conflict banner to a generic failure toast — the same lost update the
optimistic lock exists to prevent, reached by a different route. HTTP status
plays the same role on the byte routes (409 supersede / 413 too large / 403
ticket spent are three different instructions to the operator).

Corollary worth remembering: `accounts-service` and `refdata-service` still
map documented 409s to 500 over `api.*`; they could adopt
`shared/browserrpc.ReplyWithConflicts`.

## Dev-stack gotchas

- **No `vehicle-type` corpus is seeded**, so BR-TP14 rejects every code and
  **fleet assets cannot be added in the dev stack at all**. Pre-existing, and
  it blocks parts of 38d-ii. `refdata-service/cmd/seed-vehicle-types` does not
  fix it as written: it targets context `linebooker` over the REST surface
  Phase 33 retired, while the working UI context is under `acme`.
- **docker-compose publishes every port 7100–7106**, and `nats-ui` holds 7103
  — the exact port `frontend/refdata/vite.docker.config.js` pins with
  `strictPort`. So the `refdata-vs-docker` launch config cannot start while the
  stack is up; use the `refdata-dev-docker-7107` entry instead.
- `v-tooltip` is **not** registered in `frontend/refdata` (`main.js` installs
  only `PrimeVue` + `ToastService`, no directives). Use a `title` attribute
  rather than registering a directive for one hint.
- Newly importing PrimeVue Stepper/Tabs triggers a burst of Vite
  `504 (Outdated Optimize Dep)` console errors on first load. Dev-only;
  clears on a full reload.
- The working context in the dev stack is the **Fleet** selector's value
  (e.g. `acme-atlantic-fleet`), not the tenant name — worth checking before
  hand-crafting `api.{context}.…` subjects against it.

Related: [[phase38_document_object_store]], [[phase38b_transporter_vetting]],
[[frontend_port_structure]], [[linebooker_trading_partner_phase_v1_scope]].
