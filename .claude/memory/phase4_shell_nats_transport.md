# Shell registry NATS transport — Phase 4

Implemented 2026-08-30; plan: `.claude/plans/Application-Shell-Microfrontend-Plan.md`.

- Shell PLATFORM profile is `shell-platform`, minted as ephemeral `lab-shell` via
  `/api/auth/shellConnectInfo`; read/notify only, never operator rights. No nsc reseed.
- Host paints built-ins before mint/connect/read. NATS-only runtime registry; HTTP
  routes and watcher deleted. Historical HTTP client remains for contract tests.
- Reconnect epochs always read unconditionally. Boot read → subscribe → flush →
  conditional catch-up closes the snapshot/subscription race. Native browser timers
  need wrappers/binding; calling a stored native timer as a foreign object's method
  caused an Illegal invocation that unit fakes initially missed.
- Explicit JSON `ifRevision: 0` must differ from omitted/null (first curation).
- Admin callers and manual seed CLI now use the four operator registry subjects.
- Core verification passed (accounts Ginkgo 238/238, no skips; shell 334; Admin 287).
  Broader shipping notifycoverage still fails on its pre-existing omission of the
  registry application's NotifySubject builder. Guard was deliberately not edited
  under the user's no-spec-edit instruction. Docker images were not rebuilt.
