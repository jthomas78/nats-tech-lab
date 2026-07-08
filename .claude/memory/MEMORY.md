# Memory Index

- [Project plan location](project_plan_location.md) — Plan .md files live in `.claude/plans/` inside the repo, not at the root
- [Dev machine toolchain](dev_machine_toolchain.md) — Go/Node in ~/.local (user-space); Docker NOT installed, use `go test` with embedded NATS instead
- [Phase 6: Shipping domain](phase6_shipping_domain.md) — DictionaryEntry replaced by ShipState/ShipAggregate; three shapes (A/B/C) live on `poc/dictionary1.6`
- [NATS volume legacy messages](nats_volume_legacy_messages.md) — Old DICTIONARY.entry.* messages in nats-data volume cause projector Nak loop; fix: `docker compose down -v`
- [ShipAggregate pattern decisions](ship_aggregate_pattern.md) — Hydrate in commands (not domain), projector uses read-modify-write, Shape C full replay with lastSeq stop, 422 for domain errors
