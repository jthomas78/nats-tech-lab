# example-plugin-unreachable

A fixture with **no code and no web server on purpose**. It exists to prove the
shell's failure path for a remote whose chunk 404s: its `remote.url` points at
`http://localhost:7111/no-such-remoteEntry.js`, a dead path on `example-plugin`'s
own port. The plugin goes `failed`; nothing else in the shell moves.

So this directory holds `public/manifest.json` and nothing else — no
`package.json`, no `src/`, no `Dockerfile`. The manifest sits under `public/` even
though nothing serves it here, so every plugin's manifest is at the same path and
the five announcer mounts in compose are identical. Its announcer sidecar
(`example-plugin-unreachable-announcer` in `demos/01-dictionary/docker-compose.yml`)
runs the shared `announce-plugin` binary from `mfe-registry-service` and mounts
this manifest, exactly as the other four sidecars do. It is announcer-only.

The alternative — letting `example-plugin`'s sidecar announce this manifest too —
was rejected: it would make one publisher own two plugins, against the plan's
one-publisher-per-plugin premise (design decision 4), and would waste the signing
keypair and NATS credential already minted for this publisher in 13c.
