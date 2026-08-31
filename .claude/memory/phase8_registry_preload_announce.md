# Phase 8a / 8b registry wiring (2026-08-31)

- `mfe-registry-service` now reads optional `REGISTRY_PRELOAD_FILE` at startup,
  after migration and before NATS mounts. It re-plans against Curated per entry,
  applies each seed with the current revision, logs per-entry withheld causes,
  and fails boot only on whole-file/read failures. Removed means disabled, per
  BR-AS24; no delete operation was added.
- Lifecycle is lifted out of JSON into a Postgres column. Preload stores static;
  legacy empty remains unclassified, not dynamic.
- `shared/mferegistry.Announce` names the exact rpc subject, outside Subjects()
  and Operator(). Its separate servicerpc mount always uses NoVerifier in
  production. Real signatures remain Phase 7; no bypass is present.
- Accepted announcements retain first/latest observation times in entry JSON;
  static ignored announcements append audit only, with no revision or notify.
- Mounted demo `registry.json` contains the five existing example entries plus
  the staged catalog remote on 7112. No lab-shell files changed; 8d/8f still own
  catalog deployment and the frontend split.
- Verification: baseline 74/74; final registry 89/89, accounts 160/160, auth 41/41,
  no skipped specs. Full Compose build/start succeeded. Isolated fresh production
  Compose services seeded six; restart seeded zero, retained revision/audit six.
- Known pre-existing edge, deliberately retained under the user's no-refactor
  constraint: DecideAnnounce lets enabled lifecycle-empty entries take the
  same-origin update branch despite decision 74's dynamic-only scope. Resolve
  before replacing NoVerifier. Do not work around it in the handler.
