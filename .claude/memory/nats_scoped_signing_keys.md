---
name: nats-scoped-signing-keys
description: Scoped signing keys let the NATS server enforce a permission template and discard whatever the user JWT claims — the mechanism for org-inside-account isolation and for fixing unrestricted service creds
metadata:
  type: reference
---

From `docs.nats.io/learn/security/operator-mode` (checked 2026-08-11): an account can declare **scoped signing keys**. When a user JWT is signed by one, **the server applies the signing key's permission template and discards the permissions in the user JWT itself**.

**Why this matters here:** it converts subject-scoped isolation from "correct by code discipline at mint time" into "correct by server policy."

- Today `auth.MintBrowserToken` bakes permissions into each user JWT, so the security guarantee is only as good as that function. Under a scoped key, a user JWT claiming `>` still gets clamped.
- It also structurally fixes the top open gap in [[accounts_service_plan]]: `Provisioner.CreateUser` mints backend users with **no permissions set at all** (unrestricted within the account). Phase 24b's planned `CreateRestrictedUser` becomes unnecessary discipline if the key is scoped.
- It is the correct way to get the operator identity key out of `accounts-service` (`OPERATOR_SIGNING_KEY_FILE` today): delegate account admin to a scoped key, keep the operator key offline.

**The unmeasured cost — do this before committing.** The template is fixed per signing key, so isolating org-from-org needs **one scoped key per organisation**, not per role (a per-role key with a wildcard template cannot separate two customers). Every signing key lives in the account JWT, which is re-pushed to the resolver over `$SYS.REQ.CLAIMS.UPDATE` on every change (see [[nats_sys_claims_subjects]]) — so a tenant with N participants means an N-key account JWT rewritten whenever anyone joins.

**How to apply:** before designing org isolation, run a lab spike — mint a synthetic account with a few hundred scoped keys, measure account-JWT size, `$SYS.REQ.CLAIMS.UPDATE` push time, and resolver directory growth. That measurement decides between per-org keys, per-role keys, or accepting the bloat. Related: [[v3_tenancy_axes_decision]] axis 3.

**Also worth remembering:** NATS has **no account "types"** — `$SYS`, `PLATFORM` and a tenant are all just accounts, distinguished by convention and contents only. So account count ≠ tenant count (hence the `reservedAccountNames` BR-AC06 and `_` prefix BR-AC07 guards), and nothing structurally stops business state drifting into PLATFORM once a first exception is made.
