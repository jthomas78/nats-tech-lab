---
name: swag-regen-diff-noise
description: Running `swag init` locally rewrites all $ref names repo-wide (fully-qualified → short) — don't regenerate, hand-patch doc strings instead
metadata:
  type: project
---

Running `swag init -g cmd/main.go -o docs` from `demos/01-dictionary/backend/shipping-service/` regenerates `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml` — but it also rewrites every `$ref` from the existing fully-qualified form (`#/definitions/github_com_jthomas78_nats-tech-lab_..._commands.ContainerInput`) to a short form (`#/definitions/commands.ContainerInput`), producing a ~336-line diff across all three files for what should have been a one-line doc-comment change. This is a swag version/config mismatch with whatever generated the committed docs, not a real content change.

**Fix used:** revert the regenerated files (`git checkout -- docs/`) and hand-edit the specific description string in all three files (`docs.go`, `swagger.json`, `swagger.yaml` — note the YAML wraps long strings across lines with a different quoting style, edit carefully).

**How to apply:** Do not run `swag init` to pick up a swagger doc-comment change in this repo unless you're prepared to review/revert the `$ref`-naming diff it introduces. For small text-only changes to `@Description` annotations, hand-patch the three generated files directly instead.
