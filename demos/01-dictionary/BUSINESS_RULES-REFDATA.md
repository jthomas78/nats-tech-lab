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

> **Phase 16 amendments (DONE 2026-07-31, Phase 16d).** The rules below are
> unchanged, but three properties of the context hierarchy they operate on
> were re-decided in Phase 16 (see `.claude/plans/Main-POC-Plan.md` § Phase 16
> and `ARCHITECTURE-COMMUNICATIONS.md` § 2.3) and are now live:
> 1. **Root renamed `global` → `_platform`** (BR-D33/BR-AC07 enforce the
>    reservation — see below). Seeded via `ContextHandler.RegisterPlatformRoot`,
>    the one sanctioned exception to BR-D33.
> 2. **Region is removed as a context node.** The real tree is two business-unit
>    siblings under the platform root: `_platform → acme-pacific-fleet` and
>    `_platform → acme-atlantic-fleet`. Tenant "acme" owns both; neither is the
>    parent of the other. This replaces the retired `_platform → emea → emea-acme`
>    shape and the earlier `_platform → acme → acme-atlantic-fleet` (company layer)
>    shape. Region is a deployment concern (its own regional stack and NATS
>    instance) and never appears in a context value or subject token. Tenant name
>    is never a context name — contexts are business-unit identifiers, not
>    company identifiers.
> 3. **A context may be linked to a tenant** — `refdata.contexts.tenant`
>    (nullable, added by `migrate.go`'s `ALTER TABLE`). `acme-pacific-fleet` and
>    `acme-atlantic-fleet` are both seeded with `tenant: "acme"`; `_platform`
>    has none. This is **ownership/governance metadata and query scoping
>    only — not a security boundary**: refdata-service runs on a single
>    shared NATS account, so it has no server-supplied caller identity to
>    enforce it against. Making it enforceable remains an open item (see the
>    design doc) — only the metadata field itself is implemented.
>
> Arbitrary-depth inheritance is **retained** as already implemented
> (`context_repository.go`'s recursive ancestor CTE) — it is not restricted to
> a fixed number of hops. **Also now demonstrated by real seed data**, not
> just unit-tested in isolation: `hazard-class` code `3` is overridden at
> `acme-pacific-fleet` (BR-V07) and code `X1` is an addition that exists only at
> `acme-atlantic-fleet` (BR-V06) — see BR-D34 below and `seed.go`'s
> `publishInitialCorpus`, which idempotently drafts and publishes an initial
> corpus version per context, parent-first, so the chain actually has
> something to inherit.

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

### BR-D08 — A consumer resolves reference-data labels exclusively via `rpc.*`; refdata-service's KV cache is internal to that service, never read directly by another service
Phase 11.6, amended Phase 12.10, superseded by BR-D28 (Phase 12.11), amended again Phase 12.12 (IMPLEMENTED) to close a bounded-context violation: the shipping backend previously read the `refdata-{context}` KV cache bucket directly for its hot path, coupling it to refdata-service's internal cache shape (a KV entry field rename — e.g. `attrs` → `label`/`description`, Phase 12.5.1 — required a coordinated mirror-struct update in the consumer, which is a symptom of the same bounded-context violation this rule closes). Cross-service reads now go exclusively through `rpc.{context}.refdata.item.get.v1` / `type.list.v1` / `item.get-versioned.v1` (BR-D28's transport rule); refdata-service resolves the requested locale's label server-side (via the authoritative `ResolveLabel`, BR-D03's chain) and returns it pre-resolved for the plain (non-versioned) protocol. The versioned protocol (`item.get-versioned`) is the one exception — it always returns every locale rather than a pre-resolved label, so the consumer still applies the BR-D03 fallback chain locally against that response (`resolveLocalization()`, reimplemented rather than importing refdata-service — the two services share only a wire shape).

On refdata-service's side, the RPC handler itself now serves warm reads from its own KV cache first (`Projector.ReadEntry`/`ReadType`), falling through to Postgres (`ResolveItem`/`ListAssignable`) and backfilling the cache on a miss — the KV bucket is refdata-service's own read-through cache behind the RPC boundary, not a store any consumer reaches into.

- **Enforced in:** `backend/shipping-service/internal/refdataconsumer/Consumer.Lookup()` / `ResolveType()` / `LookupAtVersion()` (RPC-only; `resolveLocalization()` still applies the fallback chain to both label and description, but only for the versioned response — see BR-D30 for why the item-level fallback fields this used to also read from are gone); `backend/refdata-service/refdata/internal/natsrpc/Adapter.resolveItemKVFirst()` / `resolveTypeKVFirst()` (KV-first inside the RPC handler); `kvcache.Projector.ReadEntry()` / `ReadType()`
- **Test:** `backend/shipping-service/internal/refdataconsumer` — `TestLookupUsesRPC`, `TestLookupForwardsLocaleToRPC`, `TestResolveTypeUsesRPC`, `TestLookupAtVersionLabelFallsBackToBareLanguage`, `TestLookupAtVersionLabelFallsBackToDefaultThenCode`, `TestLookupAtVersionLabelFallsBackToCodeWhenNoLocalizations`, `TestLookupAtVersionResolvesDescriptionPerLocale`; `refdata/natsrpc_test.go` — `BR-D08: item.get serves from a warm KV cache without querying Postgres`

---

### BR-D09 — Dictionary types are categorized into a small controlled vocabulary
Phase 11.7. Every `DictionaryType` carries a `category` — one of `standards`, `domain-enum`, `domain-string`, or (reserved for later) `config` — set at type-registration time. Registering a type with any other value is rejected. Category is orthogonal to `context` (the company / business-unit scope — **not** the tenant, which is the NATS account, and **not** the region, which is a deployment concern; see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md § 2.3): it groups *types* by who owns and edits them (see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md § "Type Categories & Governance"), not by context.

- **Error:** `ErrInvalidCategory` — "dictionary type category is not a recognized category"
- **Enforced in:** `domain.ValidateCategory()`, called from `commands.TypeHandler.RegisterType()`
- **Test:** `Dictionary Type Domain Rules / BR-D09`

---

### BR-D10 — `string` items are exempt from typed-reference targeting
No relation ever declares `string` as its target type, so BR-D05's target-type validation never applies to a `string` item, and BR-D02's referenced-item-must-deprecate constraint never triggers for one either — nothing can reference a string key. This is a structural consequence of no relation declaring that target, not a special-cased exemption in the reference/delete code paths; an unreferenced `string` item hard-deletes exactly like any other unreferenced item under BR-D02.

- **Enforced in:** (no dedicated enforcement — the absence of any `string`-targeting relation in the seed/domain is what the test guards against regressing)
- **Test:** `Dictionary Type Domain Rules / BR-D10`

---

### BR-D11 — The frontend falls back to a bundled string catalog when refdata is unreachable, and visibly indicates it
Phase 11.7. UI chrome strings (button labels, filter options) are sourced from the `string` dictionary type at runtime via vue-i18n, resolved through the same `rpc.*` path as domain labels (BR-D08). If the fetch fails (refdata-service unreachable) or a key has no `string` entry, the frontend serves a bundled default-locale (`en`) catalog shipped in the build instead — chrome must still render — but the UI must clearly indicate fallback is active (a visible banner/badge). Never silently serve stale or English copy without surfacing that the live catalog isn't being used.

- **Enforced in:** `shared/refdata/useL10nCopy.js` (`usingFallback` flag), rendered by each shipping frontend's `App.vue`
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

### BR-D15 (backfill — implemented well before this write-up) — `en` is the implicit default locale for a context that has never marked one
A context is never left without an effective default: `domain.EffectiveDefaultLocale` returns whichever locale was explicitly marked (BR-D14), or `domain.ImplicitDefaultLocale` (`"en"`) when none has been. This is what lets a brand-new context start accepting localizations immediately — nobody has to remember to register `en` as a locale first for BR-D30's default-locale-first gate below to have a well-defined target.

- **Enforced in:** `domain.EffectiveDefaultLocale()`, `domain.ImplicitDefaultLocale`; called from `LocalizationHandler.DefaultLocale()`/`ResolveItem()` and `TranslationHandler`
- **Test:** `Dictionary Localization Domain Rules / BR-D03` — "treats en as the implicit default in the fallback chain when no locale is marked default (BR-D15)"

---

### BR-D16 — All Port-UI copy resolves through the string/refdata layer
Phase 11.10. Every user-facing string in `frontend/seafreight-app` — including headings, controls,
form labels, validation and error feedback, empty states, accessibility labels, derived
status labels, and notifications — is addressed by a `string` code through vue-i18n.
The `l10n` seed is the sole authored source for both English and Spanish. A committed,
generated English catalog provides the BR-D11 cold-paint fallback when refdata is
unreachable; live refdata overlays that catalog once loaded.

- **Enforced in:** `frontend/seafreight-app` Vue components via `t()` plus the generated
  `shared/refdata/l10nFallback.en.js` catalog
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
successfully-fetched string catalog and ship-status label map for each locale are cached
(`localStorage`) and applied synchronously — before the live refetch — the moment a component
connects (string) or at module load (ship-status labels, since that state doesn't wait for a
component to call `connect()`). A locale visited for the very first time still cold-paints in
`en` until its first successful fetch; there's nothing to prime from yet.

- **Enforced in:** `shared/refdata/useL10nCopy.js` (`primeFromCache()`, called from `connect()`),
  `shared/refdata/useRefdataLabels.js` (`labels` seeded from cache at module load)
- **Test:** `frontend/seafreight-app/src/useL10nCopy.spec.js` — "useL10nCopy BR-D19 catalog cache";
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

Phase 12.10. The `natsrpc/` adapter is a second transport onto the *same* application-layer method the `rest/` adapter already calls — never a place for new business logic or a shortcut around it. Concretely: `natsrpc.Adapter`'s `item.get` endpoint calls `commands.LocalizationHandler.ResolveItem()`, the identical method backing `GET /api/refdata/{context}/{type}/{code}`. This keeps REST behavior as a working isolation tool for RPC bugs (§5 of `ARCHITECTURE-COMMUNICATIONS.md`): if a `rpc.*` call misbehaves but the equivalent REST call succeeds with the same input, the bug is in the `natsrpc/` adapter, not the domain. BR-D28 (Phase 12.11) extends this same parity requirement to `type.list`, `item.get-versioned`, and `locales.list`. **Phase 21:** shipping's four call sites publish context-free local `refdata.*.v1` subjects; the tenant's NATS import maps them to the exported `rpc.{tenant-account-key}.refdata.*.v1` subject, so server-enforced account identity replaces caller-supplied context routing.

- **Enforced in:** `natsrpc.Adapter` handlers call the exported `commands.*Handler` methods directly — no adapter-local reimplementation
- **Test:** `NATS RPC Adapter / BR-D25` — asserts the RPC and REST paths return byte-identical results for the same input

---

### BR-D26 — An `obs.rpc.*` publish must never block or fail the real RPC reply

Phase 12.10. Each `natsrpc/` handler fire-and-forget publishes a request and reply observability event (`obs.rpc.{context}.refdata.{entity}.{action}`) for the Admin UI's live view. This is a best-effort side-channel: a publish failure, a full/slow subscriber, or no subscriber at all must never add latency to, delay, or prevent the actual RPC reply reaching the caller. The reply-side `obs.rpc.*` publish fires even when the real call itself errored, so a failed call is still visible in the observability view.

- **Enforced in:** `natsrpc.Adapter.publishObs()` — plain core NATS `Publish` (never `Request`), called without waiting for or checking delivery, wrapped so a panic/error from the publish itself is recovered/logged and never propagated to the caller's reply
- **Test:** `NATS RPC Adapter / BR-D26` — a closed/absent `obs.rpc.*` subscriber does not delay or fail a concurrent RPC round-trip

---

### BR-D27 — The Q5 cache backfill on a successful item read must happen on both transports, not just REST

The producer-side half of the Q5 versioned-read protocol (see `ARCHITECTURE-DICTIONARY.md`) is: whichever transport served a successful item read also rewrites that item's KV cache entry from Postgres, so a cache miss or stale entry self-heals for the *next* reader regardless of which transport hits it. REST's `getItem` handler already did this; the `natsrpc/` `item.get` endpoint (BR-D25) initially didn't, so an RPC-only consumer (e.g. `shipping-service`'s `refdataconsumer`, which since Phase 12.12/BR-D08 is RPC-only with no KV tier of its own) could keep re-missing the cache and would only warm it if something else happened to also call REST for the same item. This is a dual-transport parity gap in the same spirit as BR-D25/BR-D26: an operation's *side effects*, not just its return value, must be transport-symmetric.

- **Enforced in:** `natsrpc.Adapter.handleItemGet()` calls `kvcache.Projector.Backfill()` — the identical call REST's `getItem` makes — after every successful `ResolveItem()`, before replying; `projector` is an optional dependency (nil-safe, mirroring REST's own `Projector` nil check) so tests and any future JetStream-less deployment don't need to wire it
- **Test:** `NATS RPC Adapter / BR-D27` — an `rpc.*` lookup against a cold cache leaves a fresh, readable KV entry behind

---

### BR-D28 (IMPLEMENTED, Phase 12.11, 2026-07-24) — `rpc.*` is the sole transport for backend-to-backend synchronous calls; no REST fallback
An audit of actual `shipping-service` → `refdata-service` traffic (2026-07-24) found `rpc.*` was a minority transport despite Phase 12.10: only `Lookup`/`item.get` had any RPC path, and even that was the third tier behind a KV cache hit and an unconditional REST fallback on any RPC error — `ResolveType`, `LookupAtVersion`, and `Locales` had no `rpc.*` path at all and always called REST. **The requirement (superseding two earlier drafts of this rule — RPC-primary-with-REST-fallback, then RPC-primary-with-circuit-breaker) is: `rpc.*` is the only transport for backend-to-backend synchronous calls, full stop.** Every operation one backend service calls synchronously on another has an `rpc.*` counterpart. On a cache miss/refetch, the consumer retries `rpc.*` a bounded number of times (with backoff); if every retry fails, it returns `ErrRPCUnavailable` to its caller — there is no REST fallback to fall through to, in any form. Backend services are only aware of NATS for inter-service calls: no HTTP client, base URL, or hostname/port config pointing at a peer backend service. This does **not** change REST's role for frontend/edge clients (`frontend/admin`, `frontend/refdata`, `frontend/seafreight-app`, Swagger, third parties) — REST stays as each service's inbound surface for those callers and for human/test-suite debugging (§5 of `ARCHITECTURE-COMMUNICATIONS.md`). Since Phase 12.12 (BR-D08), the KV-first cache-read pattern lives entirely inside refdata-service's own `rpc.*` handler (`resolveItemKVFirst`/`resolveTypeKVFirst`) — a consumer's `rpc.*` call may still be served from a warm cache without touching Postgres, but that cache tier is invisible to the consumer and never a second transport it reaches for directly.

- **Scope:** `rpc.*` coverage extends beyond `item.get` (BR-D25) to `type.list` (`ResolveType`), `item.get-versioned` (`LookupAtVersion`, corpus version travels in the request body), and `locales.list` (`Locales`) — all four served by `refdata-service`'s `internal/natsrpc/adapter.go` via a `natsrpc.Deps` struct. See `ARCHITECTURE-COMMUNICATIONS.md` § 7 for the full design record.
- **Location transparency is a hard invariant, not a resilience trade-off:** `internal/refdataconsumer` has no `REFDATA_SERVICE_URL`, `refdataServiceURL()`, `baseURL`/`httpc`, or any REST-calling method — all deleted, along with the env var from `docker-compose.yml`. Since Phase 12.12 (BR-D08), `Consumer` holds only a `*nats.Conn` — no `*kvstore.Store` either, since the KV cache is refdata-service-internal. `New(nc, ...)` takes `nc` as its sole required constructor argument (no more `WithNATS` option, no KV bucket handle).
- **Bounded retry:** `requestRPC()` makes 1 initial attempt + `rpcRetries` retries (default 2, so 3 total) with linear backoff (`rpcBackoff × attempt`, default 150ms) and a per-attempt timeout (default 3s) — overridable via `WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout` (tests use these to stay fast). Exhausting every attempt returns `ErrRPCUnavailable`, wrapping the last underlying NATS error.
- **Not-found vs. other business errors:** every `natsrpc` endpoint's error reply carries `notFound bool` alongside `error string` (`isNotFoundErr()` mirrors the same domain-sentinel set REST's own status-code switch checks). The consumer's `checkRPCError()` maps `notFound: true` to this package's `ErrNotFound`; anything else becomes a generic wrapped error. This restores, at the wire level, the not-found categorization the old design got "for free" by falling through to REST's own HTTP-status handling.
- **Superseded decisions, kept here for history:** REST-as-secondary-interface and circuit-breaker/backoff were both confirmed in earlier passes over this design (2026-07-24) before being explicitly reversed the same day in favor of NATS-only + bounded-retry-then-error. Neither survives into this version of the rule.
- **Consequence, resolved:** a sustained NATS outage on a KV miss now produces `ErrRPCUnavailable` where REST previously always eventually succeeded. `dictionary/internal/rest`'s `writeRefdataError()` maps this to HTTP 503 (distinct from the generic 500) for the Phase 11.3/11.6 demo endpoints that call `refdataconsumer` — a REST-layer error-handling decision, not a Ship/Container domain rule, so it is not tracked as a separate `BUSINESS_RULES-SHIPPING.md` entry.

- **Enforced in:** `refdata-service`: `internal/natsrpc/adapter.go` (`handleTypeList`, `handleItemGetVersioned`, `handleLocalesList`, `isNotFoundErr`); `shipping-service`: `internal/refdataconsumer/consumer.go` (`requestRPC`, `checkRPCError`, `fetchViaRPC`, `fetchTypeViaRPC`, `fetchVersionedViaRPC`, `Locales`); `dictionary/internal/rest/handlers.go` (`writeRefdataError`)
- **Test:** `refdata/natsrpc_test.go` — `BR-D25/BR-D28: type.list …`, `BR-D25/BR-D28: locales.list …`, and the separate `BR-D25/BR-D28: item.get-versioned …` Describe block; `backend/shipping-service/internal/refdataconsumer` — `TestLookupReturnsErrRPCUnavailableWhenNoResponder`, `TestLookupRetriesBeforeSucceeding`, `TestResolveTypeUsesRPC`, `TestLookupAtVersionUsesRPC`, `TestLocalesUsesRPC`, `TestLocalesReturnsErrRPCUnavailableWhenNoResponder`; `dictionary/internal/rest` — `TestGetRefdataDemoReturns503WhenRPCUnavailable`, `TestListRefdataTypeReturns503WhenRPCUnavailable`, `TestListRefdataLocalesReturns503WhenRPCUnavailable`

---

### BR-D29 (Phase 12.10, 2026-07-24; retired Phase 28g) — `obs.rpc.*` is retained on a bounded `RPCTRACE` stream so a reconnecting Admin UI tab can catch up on the last 10 minutes

Superseded ARCHITECTURE-COMMUNICATIONS.md §6's original "no dedicated stream" decision once the actual requirement narrowed to a concrete one: not just "show live while the panel is open" (satisfied by plain core NATS pub/sub, BR-D26) but "show whatever happened in the last 10 minutes, even if the tab wasn't open for it." `publishObs()` now publishes onto a `RPCTRACE` JetStream stream (`LimitsPolicy`, `MaxAge: 10m`, subject filter `natsrpc.ObsSubjectWildcard` = `obs.rpc.>`) via `PublishAsync` instead of plain `nc.Publish`, when JetStream is configured. This does not weaken BR-D26: `PublishAsync` only blocks for the send itself, never for the server's ack, so the "must never block or fail the real RPC reply" contract is unchanged — and a JetStream publish is still an ordinary NATS message on the wire, so existing live subscribers (BR-D26) see it identically either way.

> **Phase 28b amendment (should have been recorded at the time, added retroactively in Phase 28g):** `publishObs()` itself was removed from `natsrpc.Adapter` in Phase 28b — `internal/natstrace`'s `Tracer` replaced it with a reply-side `obs.trace.*` span (BR-D39), the same change BR-D36 documents for the header/timestamp/payloadBytes fields. From that point on, nothing published to `obs.rpc.>`, so `RPCTRACE`'s retention held a permanently-growing backlog of nothing — nsc's stream still existed and the consumer/replay machinery still ran, but there was no traffic left to catch up on. This was not amended into this rule until Phase 28g, which is itself corrected below.

> **Phase 28g amendment — retired.** `RPCTRACE`'s stream provisioning (`refdata/composition.go`'s `RPCTraceStreamName`/`RPCTraceMaxAge`, this rule's own `Enforced in`) and its consumer-side bridge (shipping-service's `eventhandler.RegisterRPCTraceNotify` and `GET /api/rpctrace/replay`) are both removed outright, not just left dead. The Admin UI's `[messages]` tab this stream ultimately fed now derives from `obs.trace.*`/the `traces` KV bucket instead — see `BUSINESS_RULES-SHIPPING.md`'s BR-026 Phase 28g amendment for the full retirement, which is symmetric across both services.

- **Enforced in (historical, pre-28b):** `refdata/composition.go` `Startup()` provisioned `RPCTRACE` (`RPCTraceStreamName`/`RPCTraceMaxAge` = 10m) alongside the `REFDATA` change stream; `natsrpc.Adapter.publishObs()` published via `a.js.PublishAsync` when configured
- **Enforced now:** N/A — retired Phase 28g. The wire mechanism it once described lives on as BR-D39 (`obs.trace.*`); the presentation surface it fed (`[messages]`) is governed by `BUSINESS_RULES-SHIPPING.md`'s BR-035 instead.
- **Test (historical):** `NATS RPC Adapter / BR-D29` — removed in Phase 28b once `publishObs` itself was gone (see `refdata/natsrpc_test.go`'s removal note); the stream/consumer provisioning this rule described was removed in Phase 28g with no replacement test needed (there is nothing left to assert on).

---

### BR-D30 (Phase 12.13, IMPLEMENTED 2026-07-27) — An item's default-locale localization must be set before any other locale can be set for it

Removes an implicit, unenforced assumption: the KV cache used to compute a "fallback label" for an item by picking the default locale's localization, or — if that was absent — simply the first localization found in whatever order the repository happened to return (`fallbackLoc()`/an inlined duplicate in `versioned.go`'s `Materialize`). That silent "first available" branch meant an item could end up with, say, only a `fr` localization and have it become the item's fallback label with no one having decided that should be the default. `SetLocalization` now rejects setting any locale other than the context's effective default (BR-D15: explicit, or implicit `en`) until that default locale's localization already exists for the same item; setting the default locale itself is always allowed — it's necessarily the first entry. The gate is per-item, not per-context: one item having its default locale set does not unlock a different item.

This makes the default locale's entry in `localizations` a **guarantee**, not a hope — whenever an item has *any* localizations, the default locale's is among them. That guarantee is what let the KV cache's `CacheItem` drop its `Label`/`Description` fields entirely (in both `kvcache.Entry` and `kvcache.VersionedEntry` — see BR-D08): a reader resolves the default-locale label straight from `Entry.Localizations[defaultLocale]` instead of trusting a value the write path pre-computed and cached redundantly on the item. `shipping-service`'s `refdataconsumer` follows the same logic for the versioned protocol, which already had to resolve `Label` per-locale locally (BR-D08) — `Description` is now resolved the identical way (`resolveLocalization()`) rather than read off the now-removed `Item.Description`.

This is also a REST wire-format change: `GET /api/refdata/{context}/{type}/versions/{version}/items/{code}` (and its list variant) marshal `kvcache.VersionedEntry` straight to the response body, so `item.label`/`item.description` no longer appear there either. No frontend reads those sub-fields today.

- **Error:** `domain.ErrDefaultLocaleNotSet`
- **Enforced in:** `commands.LocalizationHandler.SetLocalization()` — checks `h.locs.Get(ctx, ..., defaultLocale)` before allowing a non-default `in.Locale`
- **Test:** `Dictionary Localization Domain Rules / BR-D30` — rejects a non-default locale before the default exists; allows the default first then a non-default; always allows re-setting the default; respects an explicitly marked non-`en` default; gates each item independently

---

### BR-D31 (Phase 12.14, IMPLEMENTED 2026-07-27) — A `domain-enum` type's KV entries are keyed under the `enum.` namespace

Every KV key belonging to a type whose BR-D09 category is `domain-enum` is prefixed with `enum.` — `enum.ship-status.in-transit`, not `ship-status.in-transit` — and so is that type's set-version stamp: `enum.ship-status._meta`. Types in every other category (`standards`, `domain-string`, `config`) stay unnamespaced at `{typeKey}.{code}` — e.g. the concretely seeded `domain-string`-category type's own KV keys use `string.{code}` (its `TypeKey` is `string`), not `domain-string.{code}`. The namespace covers the whole type, items and `_meta` alike, so a type occupies exactly one addressable subtree.

The point is that **NATS KV keys are subject tokens**. A key namespace is therefore the mechanism that makes "watch every enum in a context" (`enum.>`), "grant write access to enums only" (`$KV.refdata-{context}.enum.>`), and "list just the enums" expressible — without splitting the bucket per type or per category. Evaluated and rejected as the alternative: separate buckets (`refdata-{context}-enums`, `refdata-{context}-currency`, …). A bucket is a JetStream stream, so per-type buckets would multiply to `contexts × types × versions` streams, turn type registration into a stream-admin operation, and make whole-context reads (corpus materialization) an N-bucket scan — all to obtain filtering that key prefixes already provide. Separate buckets remain the right answer only for genuinely divergent *bucket-level* config (history depth, TTL, `MaxValueSize`, storage, replicas, placement); nothing in the current requirements diverges that way.

The namespace is a **projection of the type's existing category**, not new per-item state — `DictionaryItem` gains no field, and no caller passes a namespace in. `kvcache.TypeNamespaces` resolves `typeKey → namespace` through `TypeRepository` and memoizes it, because the KV-first read paths (BR-D08) exist to serve a warm read without touching Postgres and a per-lookup category query would put a Postgres round-trip back in front of every cache hit. Memoizing is safe: `TypeRepository` has no category-update path, and changing a live type's category would move its keys and require a cache rebuild regardless. A type absent from the registry resolves unnamespaced and is deliberately *not* memoized, so a type registered later still picks up its namespace.

- **Migration note:** entries written under the old unnamespaced keys before this rule are not rewritten in place. They simply stop matching (`ReadType` filters on the namespaced prefix), so the affected type reads as a cache miss and self-heals via `Backfill` into the new keys; the stale keys linger as harmless orphans until the bucket is purged (`nats kv del refdata-{context} <key>`, or a `docker compose down -v`). Item counts cannot be corrupted by the orphans, since the namespaced `_meta` is written fresh alongside the namespaced entries.
- **Enforced in:** `domain.KeyNamespace()` / `domain.EnumKeyNamespace` (the rule); `kvcache.ItemKey()`/`MetaKey()`/`TypeKeyPrefix()` (the key layout) and `kvcache.TypeNamespaces` (the resolver), applied by `kvcache.Projector` (`ReadEntry`, `ReadType`, `rebuildEntry`, `rebuildMeta`) and, for the versioned corpus buckets, `kvcache.VersionMaterializer.Materialize()` and `VersionReader.Get()`/`List()`
- **Test:** `BR-D31: a domain-enum type's KV entries are keyed under the enum. namespace` — writes an enum item to `enum.{type}.{code}` and never the unnamespaced key; keeps `_meta` in the same namespace; confirms the whole type sits under one `enum.{type}.>` subtree; leaves a `standards` type unnamespaced; round-trips `ReadEntry`/`ReadType` through the same namespace the write path used; deletes the namespaced key; falls back to unnamespaced for an unregistered type

---

### BR-D32 (Phase 12.14, IMPLEMENTED 2026-07-27) — The default locale is always listed first in the UI, and marked as the default

Every locale list a user sees — dropdown, registry table, completeness/translation matrix columns, filter select — orders the context's default locale first, and labels it `{locale} (default)` (e.g. `en (default)`) wherever the locale renders as text. Non-default locales keep the backend's own ordering behind it. A blank "no locale / raw codes" option is not a locale, so it stays pinned above the default rather than being sorted into the list.

This is the presentation counterpart to BR-D15/BR-D30: the default locale is the anchor of the whole localization model — it's the last resort of BR-D03's fallback chain and, since BR-D30, the one localization guaranteed to exist for any localized item. A user picking or auditing locales needs to see which one that is without inferring it from a radio button somewhere else on the page, and the one they'll want most often should not be somewhere in the middle of an alphabetical list.

Ordering and labelling live in one shared, dependency-free module (`demos/01-dictionary/shared/refdata/locales.js`) rather than being re-derived per component, so all four frontends present locales identically and the wording of the marker is a one-line change. `orderLocales` is non-mutating — call sites pass reactive store arrays straight in.

- **Required a backend change to be expressible at all:** `shipping-service`'s `refdataconsumer.Locales()` was unmarshalling refdata-service's `{locales, defaultLocale}` RPC reply and then returning only `resp.Locales`, so `GET /api/refdata/locales` served `{"locales": [...]}` and the two shipping frontends had no way to know which locale was default. `Locales()` now returns a `LocalesResult{Locales, DefaultLocale}` and the handler serializes both. refdata-service's own REST `GET /api/refdata/{context}/locales` already carried `defaultLocale`, which is why the refdata admin UI needed no API change.
- **Enforced in:** `shared/refdata/locales.js` (`orderLocales`, `localeLabel`, `isDefaultLocale`, `localeSelectOptions`, `DEFAULT_SUFFIX`), consumed by — `shared/refdata/useRefdataLabels.js` (exposes a ready-made `localeOptions` for the shipping apps' topbar switchers), `frontend/refdata/src/localization.js` (`buildTranslationRows` returns default-first rows carrying a marked `label`), `frontend/refdata/src/components/LocalizationView.vue` (locale registry table + completeness matrix columns), `TranslationMatrix.vue` (matrix columns), `ItemDetailPanel.vue` (per-item translations table), `ItemGrid.vue` (locale filter), `frontend/admin/src/App.vue` and `frontend/seafreight-app/src/App.vue` (topbar locale switchers)
- **Test:** `frontend/refdata/src/localization.spec.js` — `orderLocales (BR-D32)` (default moved to front, order of the rest preserved, no-op when already first / no default / unregistered default, non-mutating), `localeLabel (BR-D32)`, `localeSelectOptions (BR-D32)` (ordered+marked options; blank option pinned above the default), `buildTranslationRows ordering (BR-D32)`; `backend/shipping-service/internal/refdataconsumer` — `TestLocalesUsesRPC` asserts `DefaultLocale` survives the RPC hop

### BR-D33 (Phase 16c) — A context name beginning with `_` is reserved for platform use

A context value starting with `_` is rejected at registration (`ValidateContextName`, `domain.ErrReservedContextPrefix`) — that prefix is reserved for the platform inheritance root (`_platform`, Phase 16d) and may never be claimed by a company or business-unit context registered through the ordinary path (`POST /api/refdata/admin/contexts`). Only a *leading* underscore is rejected; `acme_northdiv` is unaffected.

This is the primary enforcement point for the `_`-reserved namespace defined in `ARCHITECTURE-COMMUNICATIONS.md` § 2.3, since a context can be registered independently of any NATS account — `accounts-service`'s own BR-AC07 (`BUSINESS_RULES-ACCOUNTS.md`) closes the same gap one level up, at tenant-identity minting, but only refdata-service can guarantee the invariant for context values themselves, because contexts are its own resource.

**Resolved (Phase 16d):** `Register` still unconditionally rejects any `_`-prefixed value, including the literal `"_platform"` — the public endpoint has no exception. The platform root is instead seeded via `ContextHandler.RegisterPlatformRoot`, a separate method that applies only `ValidateSubjectToken` (the charset check, BR-D22) and skips the reserved-prefix rejection — deliberately not exposed to any REST route, so nothing outside `seed.go` can create or overwrite the reserved root.

- **Enforced in:** `internal/domain/validation.go`'s `ValidateContextName`, called from `commands.ContextHandler.Register`; the sanctioned exception is `RegisterPlatformRoot`, called only by `seed.go`
- **Test:** `Context Domain Rules / BR-D33` and `.../Phase 16d: RegisterPlatformRoot is the one sanctioned exception to BR-D33` (`refdata/context_test.go`)

### BR-D34 (Phase 16d) — A context may record which tenant owns it, as governance metadata only

`refdata.contexts.tenant` (nullable) records which NATS-account tenant owns a context — `acme` and `acme-atlantic-fleet` are both seeded with `tenant: "acme"`; the reserved `_platform` root has none, since no tenant owns platform-wide standards data. This enables ownership queries and scoping (e.g. "list the contexts belonging to acme" — now a real server-side query, `ListByTenant`, not just a client-side filter over `List`; see BR-D35), or `Ancestors`/`Descendants` for a specific subtree.

**This is explicitly not a security boundary and must never be documented or relied on as one.** refdata-service runs on a single shared NATS account (its `platform.creds` connection) — NATS supplies it with no caller identity, so a caller simply asserts which context it wants and there is nothing server-verified to check that assertion's tenant against. Making the link enforceable is a genuinely open item, not a gap to silently paper over — see `.claude/plans/Refdata-Versioning-Tenancy-Design.md` § 2.1 for the candidates (a signed claim per call, moving refdata-service to per-tenant accounts, or accepting metadata-only as the permanent answer while reference data stays non-sensitive and shared by design).

> **Phase 32 amendment:** the "per-tenant accounts" candidate above is now implemented (BR-D40), but only for the new `api.*` browser surface — a per-tenant connection means a browser's `context` assertion is at least account-isolated to the tenant it authenticated into, closing the cross-tenant half of this gap for browser callers. The single-shared-account statement above remains true and unchanged for `rpc.*` and for the `Context.Tenant` governance field itself: `internal/natsrpc` still runs on the one PLATFORM connection this paragraph describes, and nothing server-verifies a `Context.Tenant` value against the caller within a tenant's own account (a browser in ACME's account can still assert any `{context}` value ACME's account can see — BR-D40/BR-D41 isolate accounts from each other, not contexts from each other within one account).

- **Enforced in:** `internal/postgres/migrate.go` (`ALTER TABLE ... ADD COLUMN IF NOT EXISTS tenant`), `domain.Context.Tenant`, `internal/postgres/context_repository.go` (all five methods thread it through), `internal/rest/handlers.go`'s `contextRequest.Tenant`
- **Test:** `Seed / registers acme as a child of _platform, owned by the acme tenant` and `.../registers acme-atlantic-fleet ...` (`refdata/seed_test.go`) — no dedicated domain-rule test beyond that, since there is no rule to enforce yet, only a field to persist and thread through

### BR-D35 (Phase 16f) — Listing contexts can be scoped to a tenant, returning its own contexts plus the shared platform roots

`ContextRepository.ListByTenant(ctx, tenant)` returns every context whose `tenant` column equals the given value, **plus** every context with no tenant link at all (i.e. the `_`-reserved platform roots, BR-D33) — a tenant sees its own contexts and the shared standards it inherits from, never another tenant's. `List` (unfiltered, the pre-existing admin-UI behavior) is unchanged and still the default when no tenant is given.

Exposed on both transports, mirroring every other refdata-service capability (BR-D25): `GET /api/refdata/admin/contexts?tenant=` (REST, optional query param) and `rpc._platform.refdata.context.list.v1` (natsrpc, `ContextListRequest.Tenant` in the body). The natsrpc subject's `{context}` token is the fixed literal `_platform`, not a wildcard resolved per-request like every other `rpc.*.refdata.*` endpoint — "list the contexts I can see" has no single company context to route on, so this reuses the same `rpc._platform.refdata.*` precedent already established for steward/tooling-style, corpus-wide operations (see `ARCHITECTURE-COMMUNICATIONS.md` § 2.3's fully-qualified-context discussion). The tenant to filter by travels in the request body, not the subject, since refdata-service has no server-supplied caller identity to read it from otherwise (BR-D34).

This is what backs `shipping-service`'s dynamic context list (`BUSINESS_RULES-SHIPPING.md`'s new context-listing rule, Phase 16f) — replacing that service's previously hardcoded fleet-context and refdata-company-context literals.

- **Enforced in:** `internal/postgres/context_repository.go`'s `ListByTenant`, `commands.ContextHandler.ListByTenant`, `internal/rest/handlers.go`'s `listContexts` (`?tenant=` branch), `internal/natsrpc/adapter.go`'s `handleContextList`/`ContextListSubject`
- **Test:** `Corpus and context repositories (Postgres integration) / scopes ListByTenant to the requested tenant plus every untenanted (platform) context` (`refdata/corpus_repository_integration_test.go`); `BR-D25/BR-D28: context.list is the rpc.* counterpart of listContexts` (`refdata/natsrpc_test.go`)

---

### BR-D36 (Phase 17a; superseded for `natsrpc.Adapter` by BR-D39, Phase 28b; retired Phase 28g) — Every `obs.rpc.*`/`obs.api.*` event carries its headers, a publisher-side timestamp, and its payload size

The observability envelope (`obsEnvelope`, BR-D26) gains three fields on both the request-side and reply-side event: `headers` (the real NATS headers sent with that message — for the reply side, this includes any error headers the framework attaches, e.g. micro's `Nats-Service-Error`/`Nats-Service-Error-Code`), `timestamp` (set by the publishing adapter at the moment of publish — not inferred from SSE arrival time, which is wrong for `RPCTRACE`-replayed backlog, BR-D29), and `payloadBytes` (`len()` of the marshaled payload). All three fields are additive and optional in the JSON envelope, so events published before this rule shipped (already retained on `RPCTRACE`) still decode — a consuming UI must treat their absence as "unknown," not an error.

The reply-side error headers are not fabricated for the observability channel alone: `respondError` in both adapters now attaches the real `Nats-Service-Error`/`Nats-Service-Error-Code` headers to the actual wire reply too, via `micro.WithHeaders` — additive to the existing JSON error body (`errorResponse{Error, NotFound}`), so no existing client that reads the body needs to change. This is what lets the Admin UI's Request/Reply panel (Phase 17b) show headers that genuinely traveled on the wire, not values invented for display.

> **Phase 28b amendment (should have been recorded at the time, added retroactively in Phase 28g):** `natsrpc.Adapter.publishObs()` was removed in Phase 28b — `internal/natstrace`'s `Tracer` replaced the old two-event `publishObs`/`obsEnvelope` mechanism with one reply-side `obs.trace.*` span per call (BR-D39), a strict superset of this shape that still decodes under it. The **request-side event has no replacement** — `natstrace.Span` publishes only once, at `End`/`Fail`. The real wire headers this rule also covers (`Nats-Service-Error`/`Nats-Service-Error-Code` on `respondError`'s actual reply) are unchanged; only the observability-copy half is superseded.

> **Phase 28g amendment — retired.** `obs.rpc.*`/`obs.api.*` is now fully retired across every service, not just superseded for `natsrpc.Adapter` — see `BUSINESS_RULES-SHIPPING.md`'s BR-026 Phase 28g amendment for the full retirement (the Admin UI's `[messages]` tab now derives from `obs.trace.*`/the `traces` KV bucket) and BR-D29's Phase 28g amendment for the corresponding `RPCTRACE` stream removal.

- **Enforced in (historical, pre-28b):** `natsrpc.Adapter.publishObs()` (refdata) and `browserrpc.Adapter.publishObs()` (shipping) — both populated the three fields identically, mirroring the existing dual-adapter parity pattern (BR-D26/BR-D27)
- **Enforced now:** N/A — retired Phase 28g. The wire mechanism it once described lives on as BR-D39 (`obs.trace.*`); the presentation surface it fed (`[messages]`) is governed by `BUSINESS_RULES-SHIPPING.md`'s BR-035 instead.
- **Test:** `NATS RPC Adapter (Phase 12.10) / obs.trace.* side-channel (BR-036/BR-D39)` (`refdata/natsrpc_test.go`) — an old-shape envelope with none of BR-D36's three fields still decodes without error under the new `traceSpan` shape.

---

### BR-D37 (Phase 18) — Every `rpc.*`/`api.*` request carries a `Nats-Requestor` header identifying its caller; every reply carries a `Nats-Responder` header identifying the answering service instance

NATS doesn't propagate caller or responder identity onto a message by itself — auth identity lives at the connection level and never reaches a handler's `Msg`, and a reply's subject alone doesn't distinguish which replica of a horizontally-scaled service actually answered. Without an explicit header, neither the receiving handler nor the Admin UI's Request/Reply panel (Phase 17b) can say who sent or answered a call.

Both headers share one **instance-qualified format**: `"<name>/<instance ID>"` — the same `service.name`/`service.instance.id` split OpenTelemetry's resource semantic conventions use, so replicas of the same service (or two tabs of the same browser app) stay distinguishable, and a future OTel integration maps the two halves directly onto those attributes. `Nats-Requestor` is set by the caller: `refdataconsumer.Consumer` (shipping-service's `rpc.*` caller) combines the calling connection's own `nats.Name(...)` with a NUID generated once at `New()` (e.g. `shipping-service/AbC…`); the browser's `useNatsConnection.js` `request()` (the `api.*` caller) combines `"seafreight-app"` with a random ID generated once per module load — i.e. per tab, so concurrent tabs are tellable apart. Tenant identity is deliberately excluded: that's already the NATS account boundary and doesn't need repeating in a header. `Nats-Responder` is set by the answering adapter on every reply (success and error alike) as `"<service's own nats.Name>/<micro.Service instance ID>"` — that instance ID is generated fresh per process by `micro.AddService`, with no config of its own. Both services' `micro.Config.Name` is set to match their connection's `nats.Name` exactly (`refdata-service`, `shipping-service` — not a family-derived name like `refdata-rpc`/`shipping-api`) so both headers agree on one name per service; a mismatch there would make the panel's request and reply sides look like they belong to different entities. Both headers are attached to the real wire message, not fabricated for the observability channel alone — same convention BR-D36 established for the error headers. Instance IDs are random per process/tab today; a stable infra identity (e.g. a Kubernetes pod name) can seed the instance half later without changing the header format.

> **Phase 28g amendment:** BR-D36's `obs.rpc.*`/`obs.api.*` channel this rule parity-checks against (the last sentence above) is now retired outright — see BR-D36's Phase 28g amendment. This rule's own subject (the real wire headers on the actual `rpc.*`/`api.*` request/reply) is unaffected either way; it was never about `obs.*` traffic itself, only about what an `obs.*` copy could or couldn't parity-check against.

- **Enforced in:** `internal/refdataconsumer/consumer.go`'s `requestRPC` (sets `Nats-Requestor`); `frontend/seafreight-app/src/nats/useNatsConnection.js`'s `request()` (sets `Nats-Requestor`); `natsrpc.Adapter.respondOK()`/`respondError()` (refdata) and `browserrpc.Adapter.respond()`/`respondError()` (shipping) (both set `Nats-Responder`); both adapters' `micro.AddService` `Config.Name`.
- **Test:** `NATS RPC Adapter (Phase 12.10) / BR-D37` (`refdata/natsrpc_test.go`) — a caller's instance-qualified `Nats-Requestor` header is forwarded into the obs event; a successful reply carries `Nats-Responder` (prefixed `refdata-service/`) on both the real wire reply and the obs event; a failed reply carries it too. The requestor side's format itself is asserted in shipping-service's `TestLookupCarriesInstanceQualifiedRequestorHeader` (`internal/refdataconsumer/consumer_test.go`).

---

### BR-D38 (Phase 22) — `_default_bu` is the second sanctioned exception to BR-D33's reserved-prefix rule

The shared reserved context `_default_bu` is seeded once by `refdata/seed.go`
via `ContextHandler.RegisterDefaultBu` — the second, and currently last,
method that bypasses `ValidateContextName`'s leading-underscore rejection
(the first is `RegisterPlatformRoot` for `_platform`, Phase 16d). All three
of the following are true:

1. Only `seed.go` ever calls `RegisterDefaultBu`.
2. `RegisterDefaultBu` applies `ValidateSubjectToken` (the charset check)
   but NOT `ValidateContextName` (which would reject the `_` prefix).
3. The public `POST /api/refdata/admin/contexts` endpoint always routes
   through `ContextHandler.Register`, which always runs the full
   `ValidateContextName` — so no external caller can register a
   `_`-prefixed context.

`_default_bu` is **untenanted** (`tenant = NULL` in Postgres).

**Revised 2026-08-13 (Phase 22b, accounts-service's BR-AC28/BR-AC29):**
through Phase 22, `_default_bu` was assigned directly to any account with no
business units of its own — which meant two tenants both resolving to it
wrote to the exact same refdata-service `(context, type_key, code)` rows the
moment both had zero real BUs, a real cross-tenant collision. `_default_bu`
is no longer any tenant's own context. It is now the **platform-owned
template** every tenant's own default business unit inherits from:
`_platform` → `_default_bu` → `{tenant}-default`. accounts-service registers
each tenant's default (e.g. `acme-default`, `globex-default`) with `parent:
"_default_bu"`, so `ListByTenant`'s old `tenant IS NULL` fallback path no
longer needs to surface `_default_bu` itself to every tenant — each tenant
now has its own real, tenanted default row instead. `_default_bu`'s role is
purely to hold the demo hazard-class override data (BR-V06/V07) that every
tenant default should inherit alongside `_platform`'s full corpus, via the
ancestor-chain flattening `CorpusRepository.CreateDraft` performs (see
`ARCHITECTURE-DICTIONARY.md`'s inheritance note) — **this is corpus-path
inheritance only**; the live, non-versioned read path does not traverse the
chain at all (deferred to `Main-POC-Plan.md`'s Phase 106).

Per-tenant business unit contexts (e.g. `acme-pacific-fleet`, `acme-default`)
are registered as plain contexts (via the normal `Register` path) by
accounts-service at startup (`seedPreexistingAccounts`,
`seedDemoBusinessUnits`) and via `POST /api/accounts/{name}/business-units`.
The two sanctioned underscore-prefixed contexts remain `_platform` and
`_default_bu`; no further additions are expected — critically, a tenant's
default is *not* a third exception, since it carries an ordinary
tenant-prefixed slug with no leading `_` at all.

> **See also:** BR-D34 and BR-D35 (per-tenant context registration) — as of
> Phase 22 the expectation is that real business-unit contexts (e.g.
> `acme-pacific-fleet`, `acme-atlantic-fleet`) are authored by accounts-service
> (via its `RefdataClient`) and not seeded by refdata's own `seed.go`.
> refdata-service's `seed.go` only seeds the two platform-level roots
> (`_platform`, `_default_bu`).

- **Enforced in:** `refdata/internal/application/commands/context.go`
  (`RegisterDefaultBu`) — charset check only, no `_` rejection.
  `accounts-service`'s `accounts/refdata.go` (`ProvisionDefaultContext`) is
  the caller that now parents every tenant default to this context instead of
  assigning tenants to it directly.
- **Test:** `refdata/context_test.go` `Phase 22: RegisterDefaultBu is the
  second sanctioned exception to BR-D33 (BR-D38)`. The per-tenant parenting
  behavior itself is covered by `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC29 (live
  verification only — no dedicated refdata-service-side automated test yet).

### BR-D39 (Phase 28) — The same `obs.trace.*` wire contract as `BUSINESS_RULES-SHIPPING.md`'s BR-036, on refdata-service's `natsrpc` publisher side

Mirrors `BUSINESS_RULES-SHIPPING.md`'s BR-036 for this service's own tracing publisher, exactly as BR-D36 mirrors BR-026. `natsrpc.Adapter`'s `traceSpan` is a strict superset of its existing `obsEnvelope` (BR-D26) — no field renamed or retyped, every addition (`traceId`, `spanId`, `parentSpanId`, `service`/`entity`/`action`, `statusCode`/`statusMessage`, `attributes`, `redacted`, `truncated`) `omitempty` — and every `obs.trace.{context}.refdata.{entity}.{action}` publish goes to the PLATFORM account only, with the same redact-before-truncate ordering and 4 KiB cap BR-036 establishes. Never blocks or fails a business path (inherits BR-D26).

- **Enforced in:** `refdata/internal/natstrace` (new package, Phase 28b) — mirrors `dictionary/internal/natstrace`'s `Tracer.publish()` redaction-then-truncate ordering and `traceSpan` struct field-for-field.
- **Test:** `refdata/internal/natstrace/natstrace_test.go` — the shared cross-service contract test (BR-036's clone) asserting the `traceSpan` JSON shape decodes identically to shipping-service's, and that an old-shape `obsEnvelope` with none of the Phase 28 fields still decodes.

> **Phase 28d amendment — the inbound span rides down through the command layer to the `evt.*` change-pointer publish that follows a write.** `refdata/internal/natsrpc/adapter.go`'s handlers previously called `context.Background()` when invoking a query/command, discarding whatever span `Tracer.Middleware` had already attached to the inbound request. Each of the 7 call sites (`handleItemGet`, `handleTypeList`, `handleItemGetVersioned`, `handleLocalesList` ×2, `handleContextList` ×2) now calls `natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))` instead — same `context.Context` return type, no signature changes, so the span rides `ctx` down through the application layer exactly the way BR-037's `natstrace.ContextWithSpan`/`SpanFromContext` were designed to (Phase 28c, this codebase's only sanctioned `ctx.Value` use). `internal/kvcache/kvcache.go`'s `Projector.NotifyItemChanged` — the sole place a mutation's `evt.{context}.refdata.{typeKey}.changed` change-pointer is published (BR-D04) — recovers that span via `natstrace.SpanFromContext(ctx)` and calls the widened `Publisher.PublishWithTrace(ctx, sp, subject, event)` instead of the old headerless `Publish`, so the pointer carries a `Traceparent` header when a span is present and none when it isn't (nil-safe, matching `jstream.Publisher.PublishWithTrace`'s own nil-sp behavior). No fake/mock `Publisher` existed in this package's test suite to update — every test constructs the real `jstream.NewPublisher(js)` against an embedded NATS server, which already satisfies the widened interface unchanged. No consumer of this `evt.*` feed exists yet (Phase 28d's scope for refdata-service is publish-side only), but a future one — or the OTLP bridge (Phase 28g) — can recover `(context, service, entity, action)` for `natstrace.StartFromHeaders` from the fixed `evt.{context}.refdata.{typeKey}.changed` shape (`service` = `kvcache.Domain`, `entity` = `typeKey`, `action` = the literal `"changed"`).

- **Enforced in (28d):** `refdata/internal/natsrpc/adapter.go` (7 call sites listed above); `refdata/internal/kvcache/kvcache.go`'s `Publisher` interface (widened with `PublishWithTrace`) and `Projector.NotifyItemChanged`.
- **Test (28d):** `refdata/kvcache_test.go` — `"BR-037/BR-D39: the change-event pointer carries the traceparent of the span attached to the mutation's ctx"` (asserts the published message's `Traceparent` header equals the ctx-attached span's `Traceparent()`) and `"BR-037/BR-D39: a mutation with no span on ctx publishes cleanly with no traceparent header"` (asserts no header and no error when `ctx` carries no span).

---

### BR-D40 (Phase 32) — refdata-service opens one additional NATS connection per provisioned tenant, alongside its single, unchanged PLATFORM connection

Before this phase, refdata-service ran exactly one NATS connection (`cmd/main.go`'s `waitForNATS`, `nats.Name("refdata-service")`, PLATFORM creds or none) and BR-D34 named "moving refdata-service to per-tenant accounts" as an open, unresolved candidate for closing its no-caller-identity gap. This rule implements that candidate — but only for the new browser-facing `api.*` surface (BR-D41). The existing `rpc.*` adapter (`internal/natsrpc`) keeps running on the single PLATFORM connection exactly as before; nothing about `rpc.*`'s trust model, subjects, or callers changes.

`internal/tenants.Manager` (new package) copies `pricing-service`'s `internal/tenants` pattern verbatim rather than inventing a new one: `Discover(credsDir)` re-scans `NATS_CREDS_DIR` for `*.creds` on every call, excluding `nonTenantCredsFiles` (`platform`, `shipping-admin`, `sys`, `observability`) — the same exclusion list `pricing-service` and `trading-partner-service` each carry, included from this rule's first commit rather than left to be independently rediscovered a fourth time. `EnsureAll` connects every tenant visible at Startup; `EnsureByName`/`TeardownByName` react to `notify.accounts.account.{created,reactivated}`/`{suspended}`, subscribed per-tenant-connection (mirroring `pricing-service`'s `subscribeLifecycle`) rather than via a separate PLATFORM-only creds file. Every per-tenant connection sets `nats.Name("refdata-service")` — identical to the PLATFORM connection's name, since both are the same service; a caller distinguishes them by which account answered, not by name.

**This is knowingly the fourth copy of this connection-manager shape** (after `pricing-service`, `trading-partner-service`, and — per `trading-partner-service`'s own doc comment — a documented divergence between those two already, not a clean two-copy precedent). Extraction into a shared `natstenants` package remains blocked on this repo's lack of a `go.work` across its 7 Go modules; this phase pastes the fourth copy rather than blocking on that unrelated migration, and records that the duplication is a known, deliberate deferral, not an oversight.

- **Error:** connection/subscription failures are logged and skipped per-tenant (`EnsureAll`), never fatal to Startup — one tenant's bad creds file must not prevent refdata-service, or any other tenant, from coming up.
- **Enforced in:** `refdata/internal/tenants/tenants.go` (`Manager`, `Discover`, `nonTenantCredsFiles`, `EnsureAll`/`EnsureByName`/`TeardownByName`/`subscribeLifecycle`/`Close`); `cmd/main.go` (`NATS_CREDS_DIR` env var, `nats.Name("refdata-service")` on every per-tenant connection).
- **Test:** `refdata/internal/tenants/tenants_test.go` — asserts every connection `Manager` opens (PLATFORM and per-tenant) has `nc.Opts.Name == "refdata-service"`; `nonTenantCredsFiles` excludes `observability`/`sys`/`shipping-admin`/`platform` from `Discover`'s result; `EnsureByName` is a no-op (not an error) when the tenant isn't yet visible in `credsDir`; `TeardownByName` stops the adapter and closes the connection, and is idempotent on a tenant never provisioned.

### BR-D41 (Phase 32) — `api.*.refdata.admin.*` is permission-isolated from `api.*.refdata.{item,type,locales}.*`; a browser token must never carry the admin prefix

refdata-service's new `internal/browserrpc` adapter (mounted per tenant connection, BR-D40) splits its `api.*` surface into two namespaces, not just two conventionally-named handler groups:

- **Business** — `api.{context}.refdata.item.get.v1`, `api.{context}.refdata.item.get-versioned.v1`, `api.{context}.refdata.type.list.v1`, `api.{context}.refdata.locales.list.v1`, `api.{context}.refdata.completeness.v1`, `api.{context}.refdata.cache-status.v1`, `api.{context}.refdata.context.list.v1`, `api.{context}.refdata.context.get.v1` — the `api.*` counterparts of every context/corpus **read**, calling the same query methods `internal/natsrpc`'s `rpc.*` handlers already call. `context.list`/`context.get` are grouped here, not under the admin namespace below, even though REST nests both under `/api/refdata/admin/contexts` — that nesting exists only to dodge a Go `ServeMux` path-ambiguity (ARCHITECTURE-COMMUNICATIONS.md's REST layer, handlers.go's own comment on the route), not because browsing the context hierarchy is a privileged operation. A browser reading its own tenant's context tree for a dropdown is exactly the shape BR-D40's account isolation is meant to allow.
- **Admin** — `api.{context}.refdata.admin.*`, covering corpus draft/publish/rollback/versions/diff and context **registration**/**visibility**, plus type/locale/item/reference/localization registration — every operation that mutates the corpus or its governance metadata, the `api.*` counterpart of the corresponding `/api/refdata/admin/*` REST route.

The split exists so a permission grant can scope by subject **prefix**, which NATS itself enforces server-side — the mechanism `BUSINESS_RULES-SHIPPING.md`'s Phase 34 requester-attribution work depends on being trustworthy ("omit all admin traffic" as a subject-prefix filter, not a self-declared header). `accounts-service`'s `auth/token.go` `MintBrowserToken` — which grants `api.>` broadly for both Pub and Sub — gains an explicit `Deny` on `api.*.refdata.admin.>` for both directions, added *in the same commit* as the admin subjects themselves: an admin subject that existed even briefly without this deny would be reachable by any already-minted browser token in that tenant's account. `MintAdminToken` (the Admin UI's own PLATFORM-account credential) is unaffected — it never held `api.>` in the first place and reaches refdata's admin operations over REST (Swagger-documented `/api/refdata/admin/*`) until Phase 33 gives it its own PLATFORM-scoped admin credential path.

`rpc.*` is untouched by this split: `internal/natsrpc`'s existing subjects stay exactly as they are (BR-D25), consumed only by other backend services (`shipping-service`'s `refdataconsumer`, `trading-partner-service`'s `refdataclient`) which were never subject to a browser-token permission concern in the first place.

- **Error:** N/A — this is a permission-grant rule, not a domain error; a denied browser attempt to reach `api.*.refdata.admin.>` fails at the NATS server (Permissions Violation), never reaching a handler.
- **Enforced in:** `refdata/internal/browserrpc/adapter.go` (subject constants, namespace split); `accounts-service/auth/token.go`'s `MintBrowserToken` (`Permissions.Pub.Deny.Add("api.*.refdata.admin.>")`, `Permissions.Sub.Deny.Add("api.*.refdata.admin.>")`).
- **Test:** `accounts-service/auth/token_test.go` — `"BR-D41: denies api.*.refdata.admin.> on both pub and sub while leaving the business subjects reachable"` asserts the minted JWT's actual `Pub.Deny`/`Sub.Deny` entries, and that the deny is not broadened to `api.*.refdata.>` (which would break every browser label/locale read). `refdata/internal/browserrpc/adapter_test.go` — `TestBrowserDenyCoversEveryAdminSubject` / `TestBrowserDenyCoversNoBusinessSubject` replay NATS's own `*`/`>` wildcard matching (`subjectMatches`, itself guarded by `TestSubjectMatchesHandlesWildcardForms`) over every registered subject, so a newly-added admin subject that escapes the deny prefix fails a test rather than shipping reachable. `TestEverySubjectIsClassified` compares those two lists against `Adapter.endpoints()` — the single registration table `New()` iterates — so the permission classification cannot drift from what is actually served.

> **Not yet covered:** an end-to-end live-server assertion (connect with a real `MintBrowserToken` JWT against an operator-mode server; expect a Permissions Violation on an admin subject). `accounts-service` has no embedded operator-mode test server today — `shipping-service`'s `internal/natsaccounts/shipping_testserver_test.go` is the only such harness in the repo, and it is scoped to that module. The tests above assert both halves of the contract (the grant that is minted, and the subjects it must/must not match) but not the server actually enforcing it; that remains a live-verification step.

> **Amendment — `frontend/refdata`'s own PLATFORM admin credential.** `frontend/refdata` (the dictionary/corpus editor) turned out to be a cross-tenant, platform-operator tool with no tenant/account concept of its own — it edits `_platform`'s shared standards and every tenant's contexts alike, exactly like the Admin UI, not like Sea Freight Flow. Neither existing credential fit: `MintBrowserToken` is denied from `admin.>` by this rule, and a tenant-scoped credential with that deny lifted would conflate refdata-admin rights with tenant membership (any tenant could then edit shared standards) while having no natural tenant to authenticate as. `accounts-service/auth/token.go` gained **`MintRefdataAdminToken`** — a third profile, under the same PLATFORM account `MintAdminToken` uses, but publish-capable and scoped to exactly `api.*.refdata.>` (both business and admin — never bare `api.>`), plus `Sub` on `notify._platform.refdata.>` for change notification (BR-D42's `frontend/refdata` leg, see BR-D42's own amendment below). `refdata/composition.go` gained `MountPlatformAPI`, registering `internal/browserrpc`'s `Adapter` a *second* time on refdata-service's single PLATFORM connection (alongside `internal/natsrpc`'s `rpc.*` adapter, BR-D40) — two independent `micro.Service` registrations sharing one connection and one `nats.Name`, which `nats.go/micro` supports natively (each gets its own instance ID; this is the same mechanism that lets multiple replicas of one named service coexist). Live-verified: refdata-service shows 4 `$SRV` instances in the Admin UI's Services panel (2 tenant `api.*` adapters + 1 PLATFORM `rpc.*` + this new PLATFORM `api.*`), and a full corpus create-draft → publish → rollback cycle succeeded end-to-end through `frontend/refdata` against the running stack.
> - **Enforced in:** `accounts-service/auth/token.go`'s `MintRefdataAdminToken`; `accounts-service/auth/handler.go`'s `GET /api/auth/refdataAdminConnectInfo`; `refdata/composition.go`'s `MountPlatformAPI`; `cmd/main.go`'s wiring of both `MountRPC` and `MountPlatformAPI` on the same `nc`.
> - **Test:** `accounts-service/auth/token_test.go`'s `MintRefdataAdminToken` describe block (scoped grant, no broadening to bare `api.>`/`notify.>`, expiry, invalid-seed error) and `auth/handler_test.go`'s `GET /api/auth/refdataAdminConnectInfo` describe block (mirrors `adminConnectInfo`'s three cases).

### BR-D42 (Phase 32) — `notify.*` replaces `/api/refdata-watch` SSE for corpus/item change notification

`refdata-service` already publishes `evt.{context}.refdata.{typeKey}.changed` on every corpus/item mutation (BR-D04) — a durable, replayable change-pointer feed, not a notification channel. The retired `/api/refdata-watch/{context}` SSE endpoint (`internal/rest`'s relay) is replaced by a `notify.{context}.refdata.{typeKey}.changed` bridge off that same feed, following Phase 23's precedent for the Admin UI's KV-bucket notify bridges (`internal/kvstore.Store.EnableNotify`): `notify.*` is fire-and-forget, at-most-once, browser-subscribable (`MintBrowserToken` already grants `Sub` on `notify.>`), and carries no replay guarantee — a client that needs the guaranteed feed still reads `evt.*` from the backend side, never from a browser.

The fan-out is what makes this a bridge rather than a second publish at mutation time: the mutation path (`kvcache.Projector`) runs on refdata-service's single PLATFORM connection, but a browser subscribes from inside its own tenant account — different NATS accounts, so a PLATFORM publish is simply not visible to a tenant connection. `notifybridge` consumes the durable `evt.*` feed once (ordered consumer, `DeliverNewPolicy`) and republishes over every per-tenant connection the `tenants.Manager` holds (BR-D40), reusing the existing published contract instead of adding a publish call to every mutation site.

- **Enforced in:** `refdata/internal/notifybridge` (`Run`, `parseChangedSubject`), fanned out via `tenants.Manager.PublishToAll`; started from `Handlers.MountAPI` (`refdata/composition.go`) with the PLATFORM JetStream handle.
- **Consumed by:** `shared/refdata/useRefdataLabels.js` — one `notify._platform.refdata.*.changed` subscription per browser tab, shared by `useL10nCopy` via `subscribeToChange`, replacing the `/api/refdata-watch` `EventSource`.
- **Test:** not yet automated — needs an integration test publishing a synthetic `evt.{context}.refdata.{typeKey}.changed` and asserting a tenant-connection subscriber receives the matching `notify.*` subject. Covered by live verification only at present.

### BR-D43 (Phase 33) — business (browser-facing) reads are reachable only over `api.*`; REST reduces to `/api/refdata/admin/*`, a documented exemption, not un-migrated business REST

Every business read `internal/rest` used to serve directly — item get, type/locales list, completeness, cache-status, an item's localizations/references, versioned reads, and (per BR-D42) the `/api/refdata-watch` SSE stream — is retired from REST. Each already had full `api.*` parity via `internal/browserrpc` (BR-D41's business subject group, Phase 32), and no live frontend depended on the REST version — every dictionary/refdata UI already called `api.*` exclusively by the time this phase ran. What remains mounted under `internal/rest` is `/api/refdata/admin/*` plus nothing else business-facing.

`/api/refdata/admin/*` is **not** leftover business REST that escaped migration — it is kept deliberately, for one reason: `accounts-service`'s `RefdataClient` (`accounts-service/accounts/refdata.go`) is a server-to-server HTTP client that calls it directly for account/business-unit provisioning (`RegisterContext`, `SetContextVisible`, `AddLocale`, `CreateDraft`, `PublishCorpus`, `HasPublishedCorpus`/`WaitForPublishedAncestor`), and refdata-service's NATS surface has no admin/write equivalent reachable from a server-to-server caller — `internal/natsrpc`'s `rpc.*` adapter is read-only (BR-D25), and `internal/browserrpc`'s `api.*.refdata.admin.*` group (BR-D41) is deliberately denied to every `MintBrowserToken` credential, so it isn't a fit for a backend-to-backend caller either. This mirrors the same "structurally exempt" bootstrap category already recorded for other services' operator/server-to-server REST in `Main-POC-Plan.md`'s Design decisions — it is not scheduled to move to NATS as part of this phase, and should not be mistaken for un-migrated business REST in a future pass.

- **Enforced in:** `refdata/internal/rest/handlers.go` (package doc comment records the retirement and the routes remaining); `refdata/internal/rest/sse.go` deleted entirely (the `/api/refdata-watch` handler it held). `docs/docs.go`/`docs/swagger.json`/`docs/swagger.yaml` regenerated (`swag init`) — the surface now documents only `/api/refdata/admin/*`.
- **Test:** no REST-handler-level test package existed for `internal/rest` before or after this change (coverage for these query paths lives at the domain/command layer and in `internal/browserrpc/adapter_test.go`'s BR-D41 classification tests); `go build ./...` and the full `ginkgo ./...` suite stay green with the routes removed, confirming nothing else in the module still called them.

### BR-D44 (Phase 34) — This service's mirror of `BUSINESS_RULES-SHIPPING.md`'s BR-040 mux allowlist rule

`refdata/internal/rest/handlers.go`'s `Mount` returns `[]string` — the 23
`/api/refdata/admin/*` routes (BR-D43's exemption) plus `/swagger/`, exactly.
No `GET /healthz` exists on this service today (confirmed by grepping
`cmd/main.go` for any `mux.HandleFunc`/`mux.Handle` outside `Mount` — none
found); BR-040 records this as a pre-existing gap this phase surfaces but
does not fix.

- **Enforced in:** `refdata/internal/rest/handlers.go`'s `Mount`.
- **Test:** `refdata/internal/rest/handlers_allowlist_test.go` —
  `TestMountRoutesMatchAdminAllowlist` asserts `Mount(mux)`'s returned route
  list `ConsistOf` the 24-entry allowlist above.
