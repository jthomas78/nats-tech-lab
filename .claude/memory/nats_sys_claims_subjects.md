---
name: nats-sys-claims-subjects
description: $SYS.REQ.CLAIMS.* are core NATS request-reply subjects (not JetStream) for runtime JWT resolver management in operator mode — UPDATE/DELETE/LIST
metadata:
  type: reference
---

`$SYS.REQ.CLAIMS.*` is **not** JetStream — it's part of NATS's built-in system services layer, available only in operator mode (decentralized JWT auth). These are plain request-reply subjects (`nc.Request(subject, payload, timeout)`) — no streams, consumers, or acks. The NATS server itself is the responder.

Subjects:
- `$SYS.REQ.CLAIMS.UPDATE` — push a new/updated account JWT into the resolver
- `$SYS.REQ.CLAIMS.DELETE` — revoke an account JWT from the resolver
- `$SYS.REQ.CLAIMS.LIST` — list all stored account JWTs

Only connections authenticated under the **SYS account** can publish to `$SYS.>`. In this lab, that means connecting with `nats/creds/sys.creds`. This is why `accounts-service`'s `provisioner.go` connects via SYS creds — it needs system-level access to manage the resolver.

See [[accounts-service-plan]] for how these subjects are used in practice (create via UPDATE, suspend via DELETE, reactivate via UPDATE with a unique tag).
