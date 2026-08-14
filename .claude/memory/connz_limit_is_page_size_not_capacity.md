---
name: connz-limit-is-page-size-not-capacity
description: NATS /connz `limit` (1024) is the response page size, not a connection cap — the real ceiling is max_connections from /varz
metadata:
  type: reference
---

The NATS monitoring endpoints report two numbers that look alike and are not:

- **`/connz` → `limit`** (default `1024`): the **page size of one response** —
  the most rows the server returns per request, alongside `total`, `offset` and
  `num_connections`. A response is partial when
  `offset + num_connections < total`; the next slice needs `?offset=1024`. This
  says nothing about how many connections the server accepts.
- **`/varz` → `max_connections`** (default `65536`): the **real ceiling**. The
  server refuses the next client past it.

So `14 / 1024` is a meaningless pairing, while `14 / 65,536` is a true capacity
ratio. `shipping-service`'s `GET /api/nats/connections` passes both through —
`page` (the /connz envelope) and `server.maxConnections` — with `/varz` treated
as a secondary read whose failure yields `maxConnections: 0` instead of a 502.

**How to apply:** any UI or report pairing a connection count with a limit must
take the limit from `/varz`, not from the `/connz` envelope. See
[[admin_stat_card_one_ratio_rule]] for how the Connections panel renders it.
