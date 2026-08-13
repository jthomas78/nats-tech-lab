---
name: linebooker-payments-settlement-phase
description: the third end-to-end phase after Marketplace and Transport execution — Payments & settlement, backed by real V2 entities (PaymentEntity/PaymentStatusType, InvoiceSplitType, EarlySettlementRequest factoring feature, component/accounting ERP sync) — completes the Plan-Source-Execute-Settle TMS model
metadata:
  type: project
---

Design discussion 2026-08-13, direct continuation of [[linebooker_transport_execution_phase_naming]]. User asked what follows fulfillment/transport execution, guessing "payments" — confirmed correct against the V2 codebase.

**Evidence found (grep over `backend/src/main/java/com/linebooker/console`):**
- `PaymentEntity` / `PaymentStatusType` (`IN_PROGRESS, SUCCESS, CANCELLED`) — payment status is tracked as a first-class event on the Load's timeline (`LoadHistoryType.PAYMENT_*`, `LoadEventSnapshotActionType.PAYMENT_*`), not bolted onto execution.
- `InvoiceSplitType` — invoices can be split (e.g. across cost centers).
- `EarlySettlementRequest` (`EarlySettlementEstimateDTO`, `EarlySettlementRequestConfirmation`, `EarlySettlementRequestToLb`) — a factoring-style feature: a Transporter can request payout **before** normal payment terms mature, likely at a discount. Not yet traced: the actual discount/estimate calculation.
- `component/accounting/` — a real subsystem for external ERP sync/reconciliation, including a live `AcumaticaProvider` integration (`AcumaticaTokenClient`, `AcumaticaVendorResponse`, etc.) and `BusinessAccountingProfileEntity` linking a `BusinessEntity` to its external accounting identity.
- No hits for "reconcil", "creditnote", or "debitnote" — so no separate formal reconciliation/credit-note subsystem was found under those names; may exist under different naming, not yet checked.

**This completes the classic TMS phase model** for the platform-level diagram: **Marketplace (source/procure) → Transport execution (execute, aka fulfillment) → Payments (settle)**. See [[linebooker_platform_marketplace_tenant_diagram]] for how this maps onto the Marketplace/Tenders/Bids and per-tenant Trips boxes — Payments/settlement wasn't represented in that original diagram and may need its own box once the platform-vs-tenant split is decided.

**How to apply:** per [[design_discussion_vs_implementation_signal]], discussion-stage only — nothing built or renamed yet. If/when this end-to-end model is adopted for V3, decide whether Payments lives at the platform level (single settlement engine, like Accounts/Refdata) or per-tenant (like Trips) — worth its own explicit call, same open question flagged for Marketplace/Tenders/Bids.
