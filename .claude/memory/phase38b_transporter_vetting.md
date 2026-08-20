# Phase 38b — Transporter vetting

Implemented 2026-08-20 in `trading-partner-service` under
`tradingpartner/transporterprofile` against BR-TP21–28.

- `TransporterProfile` owns monotonic `AttemptNumber`, document-review state,
  GIT verification state, and `FleetAvailabilityGate`; legacy `FleetAsset` is
  untouched.
- Temporal runs GIT verification in parallel with signal-driven per-reference
  document review. Failed GIT after approvals emits
  `DocumentApprovalReverted`; final success requires both branches.
- Workflow-triggered JetStream writes are Activities with message ID
  `{tradingPartnerID}:{event}:{attemptNumber}:{step}` and the TRANSPORTER
  stream has a ten-minute duplicate window.
- Resubmit appends `VettingResubmitted` before starting a fresh run under the
  same workflow ID with `ALLOW_DUPLICATE`; the attempt number comes from
  aggregate history, never RunID.
- Periodic GIT checks use a Temporal Schedule. `HandleGitStatusDrop` appends
  `FleetAvailabilityRevoked` before suspending the TradingPartner and can
  finish the suspension safely after a partial failure.
- Compose exposes Temporal gRPC on 7233 and its UI on 8233, with an internal
  Temporal Postgres service.
- In restricted Codex sandboxes, embedded NATS suites fail at
  `ReadyForConnections`; non-socket 38b suites pass and build/vet are clean.
