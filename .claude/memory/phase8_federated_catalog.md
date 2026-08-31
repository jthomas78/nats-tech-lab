# Phase 8f → 8d → 8e (2026-08-31)

- Five independent plugin packages/images: example 7111, catalog 7112, slow 7113,
  activation-throws 7114, incompatible 7115. Each serves public/manifest.json
  and one plugin exposure. Missing-remote stays a real 404 on 7111 with no service.
- Single seed/preload file: demos/01-dictionary/registry.json. Existing rows need
  explicit operator curation/seeding; preload preserves them. registry.dev.json
  removed. Five frontend services carry the preload source label.
- Builtin kind/adapter/boot option removed. Catalog source, views and README raw
  import live in lab-shell/plugins/demo-catalog. Host Docker no longer copies README.
- activate gets one shared frozen {version:1,ui:{ExtensionRegion}}. Freeze ui too,
  never the Vue definition. Catalog stores API in module scope; local wrapper
  forwards attrs/slots. No host runtime imports, no NATS API exposed.
- Federation gotcha: cssCodeSplit:false alone leaves remote CSS unloaded. Use
  bundleAllCSS:true. PrimeVue remotes share @primeuix/styled with host, otherwise
  their theme store is empty and buttons/tags lose the configured UniFi theme.
- Native Home/Plugins survive initial unreachable/malformed/degraded discovery
  with zero plugins. Native fallback links must not point to /demos.
- Verified 366 tests/39 files, lint 0 errors (20 warnings), independent Docker
  builds, HTTP/CORS/404 checks, browser1920x1080 catalog/nested region/statuses,
  stopped slow service isolation and registry-offline native frame. Services restored.
- Host fingerprint 8c55a929ce508c66e5495d4c78cfacd078b0a44ba7b7a192b78ce2dfb0eec620
  stays unchanged across catalog/example rebuilds; excludes identities and README.
- At this checkpoint 8c drift map/UI remained deferred; subsequently completed — see
  `phase8c_manifest_drift.md`. This phase did not change Go or Phase 7 verification/grants.
- Committed with its registry-service/preload prerequisites at user request.
  Generated NATS credentials/config and unrelated shipping test work remain
  local. Plan + business rules + architecture updated together.
