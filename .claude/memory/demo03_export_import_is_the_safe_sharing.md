---
name: demo03-export-import-is-the-safe-sharing
description: "[demo 03] Account export/import is one-way, renamed and SUBJECT-only — the importer never reaches the exporter's JetStream. It is the deliberate opposite of T5's silent double capture."
metadata:
  type: project
---

Measured 2026-09-17 by `demos/03-multi-cluster-and-accounts/lab/05-export-import.sh`
(topology **E**, 6 servers on the T2 gateway shape). Answers `D03-R8`.

This is the **one** script that changes the accounts block. `accounts_block()`
in `_common.sh` is held still for every other topology on purpose — that is the
rig's control. Do not "unify" them.

**The config.** Exporter opens one hole; importer renames what comes through:

```
LB_ZA { jetstream: enabled
        exports: [ { stream: "evt.odo.>" } ] }
LB_AU { jetstream: enabled
        imports: [ { stream: { account: LB_ZA, subject: "evt.odo.>" },
                     prefix: "za" } ] }
```

**What was measured.** Three receiver streams follow one publish:
`ODOMETER` in LB_ZA on the raw subject, `IMPORTED` in LB_AU on `za.evt.odo.>`,
and `RAW` in LB_AU on the raw subject — which must stay empty, and does.

- `E1`/`E2` — one publish in ZA is stored in BOTH accounts, across the gateway.
- `E3`/`E3a` — it arrives **prefixed**. The raw-subject stream stays empty, so
  the copy can never be mistaken for the importer's own data.
- `E4`/`E5` — the hole is **one way**. AU publishing does not move ZA's stream.
- `E6` — LB_AU cannot even read `stream info` on ZA's `ODOMETER`. The hole is a
  subject hole, not a JetStream hole.
- `E7`/`E8` — LB_AU may reuse the name `ODOMETER` for a stream of its own.

**Why it matters.** Contrast T5 (`04-hub-and-leaf.sh`, `D5`/`D6`): there a
shared account stores one publish twice, under the same name, with nothing
announcing it. Export/import is explicit, one-way and renamed — safe by design.

Related: [[demo03-mirror-over-gateway-vs-leaf]],
[[gateway_double_capture_and_option3]], [[phase21_account_exports_imports]].
