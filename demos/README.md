<!-- GENERATED FILE — do not edit by hand.
     Written by lab-shell/tools/demosReadme.mjs from the demo folders themselves.
     Regenerate with `npm --prefix lab-shell run demos:readme`.
     `demosReadme.spec.js` fails when this file and the folders disagree. -->

# Demos

One row per demo frontend. **`plugin-source` is a property of the running
shell, not of a demo** — it says how THIS shell would obtain that frontend's
entry, and the same plugin can be discovered through either source. A demo
with no frontend is still a demo; it simply has no plugin to source.

| Demo | Frontend | `plugin-source` | Port | What it is |
| --- | --- | --- | --- | --- |
| `01-dictionary` | admin | registry | 7100 | EventSourcing and CQRS |
| `01-dictionary` | seafreight-app | registry | 7101 |  |
| `01-dictionary` | refdata | registry | 7102 |  |
| `02-multi-region` | — | — | — | Multi-Region Cluster Mechanics |
| `03-multi-cluster-and-accounts` | — | — | — | Multi-Cluster Topologies and Accounts |
| `04-jetstream-cqrs` | frontend | build | 20401 | JetStream as an Event Source, with CQRS |
