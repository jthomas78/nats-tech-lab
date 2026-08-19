---
layout: home
hero:
  name: Dictionary POC Docs
  text: NATS Tech Lab — Demo 01
  tagline: Architecture and reference docs for the Ship/Container CQRS demo, browsable as a real site instead of scattered markdown.
  actions:
    - theme: brand
      text: Architecture
      link: /architecture/
features:
  - title: JetStream as the backbone
    details: Ship and Container aggregates are hydrated by replaying JetStream events — Postgres and NATS KV are downstream projections, never written directly.
  - title: KV as a cache, not the read model
    details: The POC compared three CQRS shapes over the same domain and settled on Postgres-as-source-of-truth with NATS KV as an eager write-through cache.
  - title: Reference data as its own service
    details: refdata-service owns dictionary/localization data behind its own Postgres schema, REST API, and NATS rpc.* surface, consumed by shipping-service and the Tech Lab Operator frontend.
---
