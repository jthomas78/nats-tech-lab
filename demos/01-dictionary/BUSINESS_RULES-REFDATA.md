# Business Rules — Reference Data Service (`backend/refdata-service/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping domain rules (BR-001 through BR-019).

Phase 11, [Dictionary-Service-Plan.md](../../.claude/plans/Dictionary-Service-Plan.md). A
separate service, separate Postgres schema (`refdata`) — plain CRUD, not
event-sourced (nothing ever replays a lookup value; see "Event Sourcing vs
Plain CRUD" in `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md`). Rules live in `backend/refdata-service/refdata/internal/domain/dictionary.go`
and are enforced by the command handlers in
`backend/refdata-service/refdata/internal/application/commands/`. BR-D07 (AI-translation
review gate) is implemented — Phase 11.12.

Phase 12 is governed by the [Refdata Versioning, Tenancy & Template Inheritance design](../../.claude/plans/Refdata-Versioning-Tenancy-Design.md), including its [resolved open questions](../../.claude/plans/Refdata-Versioning-Tenancy-Design.md#11-open-questions--resolved-2026-07-22).

### BR-V01–BR-V08 — Corpus versioning, tenancy, and template inheritance

- **BR-V01:** A context has at most one draft corpus version at a time.
- **BR-V02:** Only a draft can be published.
- **BR-V03:** Publishing atomically commits the version status and its complete snapshot, or neither.
- **BR-V04:** A rollback target must be a previously published version.
- **BR-V05:** Rollback creates a new, forward-only published version; it never mutates history backwards.
- **BR-V06:** A child cannot delete an inherited item; it may override it or leave it inherited.
- **BR-V07:** A child override wins for that item for itself and all descendants, breaking parent propagation.
- **BR-V08:** Publishing a parent never automatically creates, updates, or publishes descendant corpora.

The domain guards are in `internal/domain/corpus.go` and `internal/domain/inheritance.go`; the
database's partial unique index is the concurrent-write backstop for BR-V01. The Ginkgo context
`Corpus Versioning and Template Inheritance Rules` covers the lifecycle and flattening rules in
isolation (pure domain, no database); `corpus_repository_integration_test.go`'s Ginkgo context
`Corpus and context repositories (Postgres integration)` covers the same rules as enforced by
`internal/postgres/corpus_repository.go` and `context_repository.go` against a real Postgres —
this is where BR-V06/V07 (inheritance across a real ancestor context, not just the same
context's own prior version) and BR-V04/V05's audit fields are actually exercised end to end.

**Localization inheritance** (resolved open question 3 in the design doc): a localization
flows with its item down the inheritance chain. A context can override one locale of an item
it did not itself author via `PutDraftLocalization` / `PUT .../draft/localizations` — a
deliberate second write path alongside the working-table `SetLocalization`, because the
working table's FK requires the item to exist in the same context's own `dictionary_items`,
which structurally cannot express "override just this locale of an inherited item."

### BR-D01 — Item codes are unique per `{type, context}`
Registering an item whose code already exists for the same dictionary type and context is rejected. The same code is allowed again in a *different* context.

- **Error:** `ErrDuplicateItemCode` — "item code already registered for this type and context"
- **Enforced in:** `commands.ItemHandler.RegisterItem()` — checks `ItemRepository.Exists()` before insert
- **Test:** `Dictionary Item Domain Rules / BR-D01`

---

### BR-D03 — Locale resolution follows the fallback chain: requested locale → language → default locale → code; the response marks whether it was an exact match
Resolving an item's label for a locale never fails outright. It tries the exact requested locale, then the bare language (`de-DE` → `de`), then the context's registered default locale, and finally falls back to the item's code itself as the label. The response carries `isFallback` so a caller never has to infer what happened by comparing `label == code` (unreliable — a type's code can coincidentally equal its label) or by comparing the echoed `locale` to what it requested:

- `isFallback: false` — the exact requested locale or its bare language matched.
- `isFallback: true` — either a default-locale substitution (nothing requested matched, but the context's default locale had real data — `label` is real, `locale` in the response reports the *default* locale, not the one requested) or the terminal code-echo (nothing matched at all, `label` degraded to the code itself). Both count as "not what was asked for," so they collapse to the same flag; the response's `locale` field still distinguishes them (default locale vs. the literal input echoed back).

No validation rejects a malformed/unregistered `requestedLocale` (e.g. `"e"`) — it's simply never matched at the exact/language tier, so a context with a real default-locale localization returns that as a fallback (`isFallback: true`, `locale: "en"`) rather than either erroring or misreporting it as an exact match.

- **Enforced in:** `domain.ResolveLabel()`, called from `commands.LocalizationHandler.ResolveItem()`; surfaced as `"isFallback"` in both transports — REST's `resolvedItemResponse` (`*bool`, nil/omitted entirely when no `?locale=` resolution was attempted at all — see `getItem`/`listItems`) and the `rpc.*.refdata.item.get.v1` `ItemGetResponse` (plain `bool`, since that call always resolves a locale)
- **Test:** `Dictionary Localization Domain Rules / BR-D03`, `NATS RPC Adapter (Phase 12.10) / BR-D25`

---

### BR-D04 — Every mutation to a type's items, localizations, or references bumps that type's set version atomically with the write
Registering/deprecating/deleting an item, setting a localization, or creating a reference each bump `{context, type}`'s set version — a single-statement Postgres `UPSERT ... RETURNING`, so a concurrent bump can never be lost or torn. The bump then rebuilds the affected item's KV cache entry and the type's `_meta` entry (both stamped with the new version) and publishes a bounded change-event pointer on the `REFDATA` stream. This is the write side of the Q5 versioned-read protocol (see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md).

- **Enforced in:** `postgres.VersionRepository.Bump()` (the atomic increment) + `kvcache.Projector.NotifyItemChanged()` (cache rebuild + event publish), wired into `ItemHandler`/`ReferenceHandler`/`LocalizationHandler` via the `domain.ChangeNotifier` port
- **Test:** `KV cache + versioned-read protocol (Phase 11.3) / bumps the set version atomically`, plus the cache-rebuild, `_meta`, and change-event specs in the same file

---

### BR-D02 — An unreferenced item may be hard-deleted; a referenced item must be deprecated instead
Deleting an item that is the target of another item's typed reference would silently break that reference. Referenced items can only be deprecated (status flip), never hard-deleted.

- **Error:** `ErrItemReferenced` — "item is referenced and cannot be deleted; deprecate instead"
- **Enforced in:** `commands.ItemHandler.DeleteItem()` — checks `ReferenceRepository.IsReferenced()` before delete
- **Test:** `Dictionary Item Domain Rules / BR-D02`

---

### BR-D05 — A reference must target an active item of the relation's declared type
Creating a typed reference (e.g. country → defaultCurrency → currency) is rejected if the target doesn't exist, isn't active, or isn't of the type the relation declares as its target.

- **Errors:** `ErrReferenceTargetWrongType`, `ErrReferenceTargetNotFound`, `ErrReferenceTargetNotActive`
- **Enforced in:** `commands.ReferenceHandler.CreateReference()`
- **Test:** `Dictionary Reference Domain Rules / BR-D05`

---

### BR-D06 — Deprecated items still resolve on read; excluded from assignable-value listings by default
A deprecated item must remain resolvable so historic data stays renderable, but it should not appear as a choice when a user is picking a new value.

- **Enforced in:** `commands.ItemHandler.Get()` (no status filter) vs `ListAssignable()` (filters via `domain.FilterAssignable()`)
- **Test:** `Dictionary Item Domain Rules / BR-D06`

---

### BR-D07 — AI-drafted translations are never persisted without explicit human save; persisted localizations record their `source`

Phase 11.12. A steward can request an AI-drafted label/description for a missing locale instead of typing one by hand. The draft call (`POST /api/refdata/admin/{type}/{code}/translate`) only returns candidate text — it never writes to Postgres, never bumps the type's set version, and never publishes a change event. A draft becomes real only through the existing, separate `SetLocalization` save step, which now records `source: "ai"` on that row when the caller marks it as an accepted AI draft, and `source: "manual"` for a hand-typed or hand-edited-then-saved translation (this is the caller's explicit assertion, not something the domain infers from the text). Bulk drafting (a whole type × locale gap) issues one model call at a time, never concurrently — see BR-D24.

- **Enforced in:** `commands.TranslationHandler.DraftTranslations()` (drafts only, no repository writes) / `commands.LocalizationHandler.SetLocalization()` (persists `in.Source`, defaulting to `"manual"` when unset, via the `domain.ValidateSource` guard)
- **Error:** `ErrInvalidSource` — `source` must be `"manual"` or `"ai"`
- **Test:** `Dictionary Translation Domain Rules / BR-D07`

---

### BR-D08 — A consumer resolves reference-data labels KV-first, applying the BR-D03 fallback chain; a miss or stale entry re-fetches via `rpc.*` exclusively (no REST fallback, per BR-D28)
Phase 11.6, amended Phase 12.10, superseded by BR-D28 (Phase 12.11, IMPLEMENTED). When the shipping backend resolves a reference-data label for display, it reads the `refdata-{context}` KV cache directly and resolves the requested locale's label from the cached localizations map, applying the same fallback chain as BR-D03 (requested locale → bare language → default locale → the code itself). A KV miss or a stale (version-mismatched) entry — the Q5 read protocol's miss case — calls `rpc.{context}.refdata.item.get.v1` (`fetchViaRPC`) via a bounded number of retries with backoff (BR-D28); the reply resolves the label server-side via the authoritative `ResolveLabel` and backfills the cache. The consumer reimplements the ~10-line fallback rather than importing refdata-service (the two services share only a wire shape); the default locale is a constant mirroring the context's seeded default. Enforced on the *consuming* side (the shipping backend), so it lives here alongside the producer rules it depends on.

- **Enforced in:** `backend/shipping-service/internal/refdataconsumer/Consumer.Lookup()` / `ResolveType()` (`resolveLabel()` implements the fallback chain; `fetchViaRPC()` makes the call via `requestRPC()`, BR-D28's bounded retry helper)
- **Test:** `backend/shipping-service/internal/refdataconsumer` — `TestLookupResolvesLabelFromKV`, `TestLookupLabelFallsBackToBareLanguage`, `TestLookupLabelFallsBackToDefaultThenCode`, `TestLookupMissForwardsLocaleToRPC`, `TestResolveTypeReturnsAllCodesFromKV`, `TestLookupMissUsesRPC`

---

### BR-D09 — Dictionary types are categorized into a small controlled vocabulary
Phase 11.7. Every `DictionaryType` carries a `category` — one of `standards`, `domain-enum`, `ui-copy`, or (reserved for later) `config` — set at type-registration time. Registering a type with any other value is rejected. Category is orthogonal to `context` (tenant/region): it groups *types* by who owns and edits them (see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md § "Type Categories & Governance"), not by tenant.

- **Error:** `ErrInvalidCategory` — "dictionary type category is not a recognized category"
- **Enforced in:** `domain.ValidateCategory()`, called from `commands.TypeHandler.RegisterType()`
- **Test:** `Dictionary Type Domain Rules / BR-D09`

---

### BR-D10 — `ui-copy` items are exempt from typed-reference targeting
No relation ever declares `ui-copy` as its target type, so BR-D05's target-type validation never applies to a `ui-copy` item, and BR-D02's referenced-item-must-deprecate constraint never triggers for one either — nothing can reference a UI-copy key. This is a structural consequence of no relation declaring that target, not a special-cased exemption in the reference/delete code paths; an unreferenced `ui-copy` item hard-deletes exactly like any other unreferenced item under BR-D02.

- **Enforced in:** (no dedicated enforcement — the absence of any `ui-copy`-targeting relation in the seed/domain is what the test guards against regressing)
- **Test:** `Dictionary Type Domain Rules / BR-D10`

---

### BR-D11 — The frontend falls back to a bundled UI-copy catalog when refdata is unreachable, and visibly indicates it
Phase 11.7. UI chrome strings (button labels, filter options) are sourced from the `ui-copy` dictionary type at runtime via vue-i18n, the same KV-cached pipeline as domain labels (BR-D08). If the fetch fails (refdata-service unreachable) or a key has no `ui-copy` entry, the frontend serves a bundled default-locale (`en`) catalog shipped in the build instead — chrome must still render — but the UI must clearly indicate fallback is active (a visible banner/badge). Never silently serve stale or English copy without surfacing that the live catalog isn't being used.

- **Enforced in:** `shared/refdata/useUiCopy.js` (`usingFallback` flag), rendered by each shipping frontend's `App.vue`
- **Test:** manual — verified in browser by stopping refdata-service and confirming the fallback banner appears (frontend build has no JS test harness; see CLAUDE.md's UI-testing guidance)

---

### BR-D12 — A deprecated item can be reactivated back to active
Phase 11.8. Deprecation (BR-D02, BR-D06) is not permanent: an admin can flip a deprecated item back to `active` at any time, with no restrictions (e.g. no check for a code collision with a newer item, no reason/audit requirement). This is a plain status reversal, symmetric with `DeprecateItem` — reactivating an item that's already active is a no-op that succeeds.

- **Enforced in:** `commands.ItemHandler.ReactivateItem()` — checks the item exists (`ErrItemNotFound` otherwise), then flips status and bumps the type's version (BR-D04) same as deprecate
- **Test:** `Dictionary Item Domain Rules / BR-D12`

---

### BR-D13 — The dictionary admin UI's locale selector defaults to `en`, not raw codes
Phase 11.8. `frontend/refdata`'s locale dropdown previously defaulted to an empty selection, which resolves items by their raw code instead of a localized label (BR-D03). It now defaults to `en` on load, matching the precedent already established in `shared/refdata/useRefdataLabels.js` for the other two frontends. A user can still explicitly pick `(code)` from the dropdown to see raw codes.

- **Enforced in:** `frontend/refdata/src/stores/dictionary.js` — `selectedLocale` initial state
- **Test:** manual — verified in browser (frontend build has no JS test harness; see BR-D11's note)

---

### BR-D14 — At most one default locale per context
Phase 11.9. The default locale is the BR-D03 fallback target, so it must be unambiguous: registering a locale with `isDefault=true` *moves* the default — the previously-default locale is unmarked in the same transaction. There is no separate "set default" operation; re-registering an existing locale is an upsert, so `POST /api/refdata/admin/locales` with `isDefault=true` is both "add as default" and "make this the default". The admin UI renders this as a radio column (exactly-one semantics): the current default cannot be un-set, only replaced.

- **Enforced in:** `postgres.LocaleRepository.Add()` — clears `is_default` for the context and sets the new default in one transaction
- **Test:** `Localization Domain Rules / Locale management` — "moves the default when another locale is marked default"

---

### BR-D16 — All Port-UI copy resolves through the i18n/refdata layer
Phase 11.10. Every user-facing string in `frontend/seafreight-app` — including headings, controls,
form labels, validation and error feedback, empty states, accessibility labels, derived
status labels, and notifications — is addressed by a `ui-copy` code through vue-i18n.
The `ui-copy` seed is the sole authored source for both English and Spanish. A committed,
generated English catalog provides the BR-D11 cold-paint fallback when refdata is
unreachable; live refdata overlays that catalog once loaded.

- **Enforced in:** `frontend/seafreight-app` Vue components via `t()` plus the generated
  `shared/refdata/uiCopyFallback.en.js` catalog
- **Test:** `frontend/seafreight-app/scripts/check-i18n.mjs` rejects bare user-facing literals;
  `npm run check:i18n` regenerates the fallback and rejects drift;
  `frontend/seafreight-app/src/App.spec.js` mounts the real UI with vue-i18n and verifies locale
  switching, interpolation, pluralization, and mutually-exclusive Fleet/Port rendering

---

### BR-D18 — An item's attrs can be replaced after creation, independent of status or localization
`RegisterItem` is insert-only, so an item's `attrs` map was previously frozen at whatever
was passed at creation — a real gap: `attrs.name` is the bootstrap display name shown before
an admin-created item has any localization (see `frontend/refdata`'s `ItemGrid.vue` `labelFor()`
fallback chain), but nothing could ever correct it afterward. `UpdateItemAttrs` replaces the
entire `attrs` map in one call — a full replace, not a per-key merge, matching `RegisterItem`'s
Attrs semantics — and works regardless of item status, mirroring BR-D06's
read-regardless-of-status stance rather than gating writes on deprecation.

- **Errors:** `ErrItemNotFound` if the item doesn't exist
- **Enforced in:** `commands.ItemHandler.UpdateItemAttrs()`
- **Test:** `Dictionary Item Domain Rules / BR-D18`

---

### BR-D19 — Cold paint renders the persisted locale's last-known catalog, not the bundled default
`frontend/seafreight-app`'s selected locale persists across reloads (`localStorage`). Once that's true,
every reload into a non-`en` locale would otherwise cold-paint in the bundled English catalog
(BR-D11 only ever bundles `en`) for the length of the live refetch — visibly mismatching the
locale shown as selected. This is distinct from BR-D11: BR-D11 covers refdata being
*unreachable*; here refdata is reachable, it just hasn't answered *yet*. The last
successfully-fetched ui-copy catalog and ship-status label map for each locale are cached
(`localStorage`) and applied synchronously — before the live refetch — the moment a component
connects (ui-copy) or at module load (ship-status labels, since that state doesn't wait for a
component to call `connect()`). A locale visited for the very first time still cold-paints in
`en` until its first successful fetch; there's nothing to prime from yet.

- **Enforced in:** `shared/refdata/useUiCopy.js` (`primeFromCache()`, called from `connect()`),
  `shared/refdata/useRefdataLabels.js` (`labels` seeded from cache at module load)
- **Test:** `frontend/seafreight-app/src/useUiCopy.spec.js` — "useUiCopy BR-D19 catalog cache";
  `frontend/seafreight-app/src/useRefdataLabels.spec.js` — "useRefdataLabels ship-status label cache"

---

### BR-D20 — A locale code must be lower case

A locale code (a context's registered locale, or the `locale` on a per-item localization) must
be entirely lower case — `af-za`, not the BCP-47-conventional `af-ZA`. BCP-47 upper-cases the
region subtag by convention, but this system standardizes on lower case throughout so locale-code
equality is a plain string comparison everywhere it's checked (Postgres `TEXT` columns, NATS KV
keys, frontend cache keys) without a canonicalization step first. This is a format rule at the
point of entry, not a read-side normalization — a locale is validated when it's registered
(`AddLocale`) or when a localization is written (`SetLocalization`); nothing downstream lowercases
on the admin's behalf.

- **Errors:** `ErrInvalidLocaleFormat` if the locale contains any upper-case character
- **Enforced in:** `domain.ValidateLocale()`, called from
  `commands.LocalizationHandler.AddLocale()` and `commands.LocalizationHandler.SetLocalization()`
- **Test:** `Dictionary Localization Domain Rules / Locale management / BR-D20`

---

### BR-D21 — Clicking a docked ship's port in Fleet Management jumps to Port Management scoped to that port

Phase 13. In `frontend/seafreight-app`'s Fleet Management view, a docked ship's Port cell is a
navigation shortcut, not just a display field: clicking it (or activating it with Enter)
switches the active view to Port Management and sets the selected port to that ship's
`currentPort` — equivalent to picking the port from the Port Management dropdown manually.
A ship at sea (`currentPort === ''`) has no port to jump to, so its cell renders as plain
text, not a link. This is a frontend navigation/UX rule, not a domain rule — no backend
state changes, and it has no Ginkgo coverage; it's exercised via the Vue component test below.

- **Enforced in:** `frontend/seafreight-app/src/components/FleetPanel.vue` (`goToPort()`, emits
  `navigate-port`), `frontend/seafreight-app/src/App.vue` (`handleNavigatePort()` sets `store.port`
  and `activeView`)
- **Test:** `frontend/seafreight-app/src/App.spec.js` — "BR-D21: clicking a docked ship's port in Fleet
  Management jumps to Port Management scoped to that port"; "BR-D21: a ship at sea has no
  clickable port link"

---

### BR-D22 — TypeKey, Code, and Context must be valid subject/KV-key tokens

`typeKey` and `context` are threaded into a NATS subject (`evt.{context}.refdata.{typeKey}.changed`) and KV bucket name (`refdata-{context}`) — both must match the subject-token charset `^[A-Za-z0-9_-]+$`. `code` only ever becomes part of a KV key (`{typeKey}.{code}`), never a subject, so it uses the more permissive KV-key-legal charset already documented in `CLAUDE.md`: `^[-/_=.a-zA-Z0-9]+$` (`:` is illegal per NATS KV key rules). All three reject the empty string.

- **Error:** `ErrInvalidToken` (typeKey, context) / `ErrInvalidKVKeyComponent` (code)
- **Enforced in:** `TypeHandler.RegisterType()` (typeKey), `ItemHandler.RegisterItem()` (code), `ContextHandler.Register()` (context)
- **Test:** `Dictionary Type Domain Rules / BR-D22` (typeKey), `Dictionary Item Domain Rules / BR-D22` (code), `Context Domain Rules / BR-D22` (context)

---

### BR-D23 — Setting a localization only publishes a change event when the label or description actually changed

`SetLocalization` reads the existing localization for the (typeKey, context, code, locale) before upserting; if the new label and description are identical to what's already stored, the upsert still runs (harmless) but `NotifyItemChanged` is skipped — no version bump, no `evt.{context}.refdata.{typeKey}.changed` publish. This matters because `Seed()` runs unconditionally on every service startup and calls `SetLocalization` for every seeded item/locale (en/es/af-za) — without this guard, every restart or `docker compose up --build` re-published a full duplicate batch of change events onto the REFDATA stream even though nothing changed. Item creation (`RegisterItem`) already had an equivalent guard via `ErrDuplicateItemCode`; this closes the same gap on the localization-set path. A genuine label/description edit still notifies exactly as before.

- **Enforced in:** `commands.LocalizationHandler.SetLocalization()` — via `domain.LocalizationRepository.Get()` (new), comparing `existing.Label`/`existing.Description` against the input before deciding whether to notify
- **Test:** `Dictionary Localization Domain Rules / SetLocalization change-event notification`

---

### BR-D24 — Bulk AI translation drafting calls the model sequentially, never concurrently

Phase 11.12. Two layers, same guard. (1) `DraftTranslations` drafts one item's several missing locales one at a time — a plain loop over target locales, not fanned out. (2) The Translation Matrix's "Draft missing (AI)" bulk action can span an entire type × locale gap across many items; since the `translate` endpoint is per-item (`POST /api/refdata/admin/{type}/{code}/translate`), the bulk case is a frontend-orchestrated loop that `await`s each item's translate call before issuing the next, never `Promise.all`. Both bound cost/load against the (external, rate-limited, billed) model API with the simplest possible implementation, at the cost of wall-clock time for large gaps. There is no separate concurrency limit to configure because there is no concurrency — a future bounded worker-pool is an explicit, separate change if this ever proves too slow in practice.

- **Enforced in:** `commands.TranslationHandler.DraftTranslations()` (per-item, a plain `for` loop over target locales, no goroutines) and `frontend/refdata`'s bulk "Draft missing (AI)" action (a sequential `for...of` + `await`, never `Promise.all`, over items missing the target locale)
- **Test:** `Dictionary Translation Domain Rules / BR-D24` (backend loop); `frontend/refdata` Vitest coverage for the bulk action's sequential await

---

### BR-D25 — An `rpc.*` operation must exist as a `commands`/`queries` method already exposed via REST

Phase 12.10. The `natsrpc/` adapter is a second transport onto the *same* application-layer method the `rest/` adapter already calls — never a place for new business logic or a shortcut around it. Concretely: `natsrpc.Adapter`'s `item.get` endpoint calls `commands.LocalizationHandler.ResolveItem()`, the identical method backing `GET /api/refdata/{context}/{type}/{code}`. This keeps REST behavior as a working isolation tool for RPC bugs (§5 of `ARCHITECTURE-COMMUNICATIONS.md`): if a `rpc.*` call misbehaves but the equivalent REST call succeeds with the same input, the bug is in the `natsrpc/` adapter, not the domain. BR-D28 (Phase 12.11) extends this same parity requirement to `type.list`, `item.get-versioned`, and `locales.list`.

- **Enforced in:** `natsrpc.Adapter` handlers call the exported `commands.*Handler` methods directly — no adapter-local reimplementation
- **Test:** `NATS RPC Adapter / BR-D25` — asserts the RPC and REST paths return byte-identical results for the same input

---

### BR-D26 — An `obs.rpc.*` publish must never block or fail the real RPC reply

Phase 12.10. Each `natsrpc/` handler fire-and-forget publishes a request and reply observability event (`obs.rpc.{context}.refdata.{entity}.{action}`) for the Admin UI's live view. This is a best-effort side-channel: a publish failure, a full/slow subscriber, or no subscriber at all must never add latency to, delay, or prevent the actual RPC reply reaching the caller. The reply-side `obs.rpc.*` publish fires even when the real call itself errored, so a failed call is still visible in the observability view.

- **Enforced in:** `natsrpc.Adapter.publishObs()` — plain core NATS `Publish` (never `Request`), called without waiting for or checking delivery, wrapped so a panic/error from the publish itself is recovered/logged and never propagated to the caller's reply
- **Test:** `NATS RPC Adapter / BR-D26` — a closed/absent `obs.rpc.*` subscriber does not delay or fail a concurrent RPC round-trip

---

### BR-D27 — The Q5 cache backfill on a successful item read must happen on both transports, not just REST

The producer-side half of the Q5 versioned-read protocol (see `ARCHITECTURE-DICTIONARY.md`) is: whichever transport served a successful item read also rewrites that item's KV cache entry from Postgres, so a cache miss or stale entry self-heals for the *next* reader regardless of which transport hits it. REST's `getItem` handler already did this; the `natsrpc/` `item.get` endpoint (BR-D25) initially didn't, so an RPC-only consumer (e.g. `shipping-service`'s RPC-first `refdataconsumer`, Phase 12.10) could keep re-missing the cache and would only warm it if something else happened to also call REST for the same item. This is a dual-transport parity gap in the same spirit as BR-D25/BR-D26: an operation's *side effects*, not just its return value, must be transport-symmetric.

- **Enforced in:** `natsrpc.Adapter.handleItemGet()` calls `kvcache.Projector.Backfill()` — the identical call REST's `getItem` makes — after every successful `ResolveItem()`, before replying; `projector` is an optional dependency (nil-safe, mirroring REST's own `Projector` nil check) so tests and any future JetStream-less deployment don't need to wire it
- **Test:** `NATS RPC Adapter / BR-D27` — an `rpc.*` lookup against a cold cache leaves a fresh, readable KV entry behind

---

### BR-D28 (IMPLEMENTED, Phase 12.11, 2026-07-24) — `rpc.*` is the sole transport for backend-to-backend synchronous calls; no REST fallback
An audit of actual `shipping-service` → `refdata-service` traffic (2026-07-24) found `rpc.*` was a minority transport despite Phase 12.10: only `Lookup`/`item.get` had any RPC path, and even that was the third tier behind a KV cache hit and an unconditional REST fallback on any RPC error — `ResolveType`, `LookupAtVersion`, and `Locales` had no `rpc.*` path at all and always called REST. **The requirement (superseding two earlier drafts of this rule — RPC-primary-with-REST-fallback, then RPC-primary-with-circuit-breaker) is: `rpc.*` is the only transport for backend-to-backend synchronous calls, full stop.** Every operation one backend service calls synchronously on another has an `rpc.*` counterpart. On a cache miss/refetch, the consumer retries `rpc.*` a bounded number of times (with backoff); if every retry fails, it returns `ErrRPCUnavailable` to its caller — there is no REST fallback to fall through to, in any form. Backend services are only aware of NATS for inter-service calls: no HTTP client, base URL, or hostname/port config pointing at a peer backend service. This does **not** change REST's role for frontend/edge clients (`frontend/admin`, `frontend/refdata`, `frontend/seafreight-app`, Swagger, third parties) — REST stays as each service's inbound surface for those callers and for human/test-suite debugging (§5 of `ARCHITECTURE-COMMUNICATIONS.md`) — or the KV-first cache-read pattern of BR-D08 — a cache hit still never calls either transport.

- **Scope:** `rpc.*` coverage extends beyond `item.get` (BR-D25) to `type.list` (`ResolveType`), `item.get-versioned` (`LookupAtVersion`, corpus version travels in the request body), and `locales.list` (`Locales`) — all four served by `refdata-service`'s `internal/natsrpc/adapter.go` via a `natsrpc.Deps` struct. See `ARCHITECTURE-COMMUNICATIONS.md` § 7 for the full design record.
- **Location transparency is a hard invariant, not a resilience trade-off:** `internal/refdataconsumer` has no `REFDATA_SERVICE_URL`, `refdataServiceURL()`, `baseURL`/`httpc`, or any REST-calling method — all deleted, along with the env var from `docker-compose.yml`. `Consumer` holds a `*kvstore.Store` and a `*nats.Conn` and nothing else; `New(kv, nc, ...)` takes `nc` as a required constructor argument (no more `WithNATS` option).
- **Bounded retry:** `requestRPC()` makes 1 initial attempt + `rpcRetries` retries (default 2, so 3 total) with linear backoff (`rpcBackoff × attempt`, default 150ms) and a per-attempt timeout (default 3s) — overridable via `WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout` (tests use these to stay fast). Exhausting every attempt returns `ErrRPCUnavailable`, wrapping the last underlying NATS error.
- **Not-found vs. other business errors:** every `natsrpc` endpoint's error reply carries `notFound bool` alongside `error string` (`isNotFoundErr()` mirrors the same domain-sentinel set REST's own status-code switch checks). The consumer's `checkRPCError()` maps `notFound: true` to this package's `ErrNotFound`; anything else becomes a generic wrapped error. This restores, at the wire level, the not-found categorization the old design got "for free" by falling through to REST's own HTTP-status handling.
- **Superseded decisions, kept here for history:** REST-as-secondary-interface and circuit-breaker/backoff were both confirmed in earlier passes over this design (2026-07-24) before being explicitly reversed the same day in favor of NATS-only + bounded-retry-then-error. Neither survives into this version of the rule.
- **Consequence, resolved:** a sustained NATS outage on a KV miss now produces `ErrRPCUnavailable` where REST previously always eventually succeeded. `dictionary/internal/rest`'s `writeRefdataError()` maps this to HTTP 503 (distinct from the generic 500) for the Phase 11.3/11.6 demo endpoints that call `refdataconsumer` — a REST-layer error-handling decision, not a Ship/Container domain rule, so it is not tracked as a separate `BUSINESS_RULES-SHIPPING.md` entry.

- **Enforced in:** `refdata-service`: `internal/natsrpc/adapter.go` (`handleTypeList`, `handleItemGetVersioned`, `handleLocalesList`, `isNotFoundErr`); `shipping-service`: `internal/refdataconsumer/consumer.go` (`requestRPC`, `checkRPCError`, `fetchViaRPC`, `fetchTypeViaRPC`, `fetchVersionedViaRPC`, `Locales`); `dictionary/internal/rest/handlers.go` (`writeRefdataError`)
- **Test:** `refdata/natsrpc_test.go` — `BR-D25/BR-D28: type.list …`, `BR-D25/BR-D28: locales.list …`, and the separate `BR-D25/BR-D28: item.get-versioned …` Describe block; `backend/shipping-service/internal/refdataconsumer` — `TestLookupReturnsErrRPCUnavailableWhenNoResponder`, `TestLookupRetriesBeforeSucceeding`, `TestResolveTypeUsesRPCWhenBucketEmpty`, `TestLookupAtVersionMissUsesRPC`, `TestLocalesUsesRPC`, `TestLocalesReturnsErrRPCUnavailableWhenNoResponder`; `dictionary/internal/rest` — `TestGetRefdataDemoReturns503WhenRPCUnavailable`, `TestListRefdataTypeReturns503WhenRPCUnavailable`, `TestListRefdataLocalesReturns503WhenRPCUnavailable`
