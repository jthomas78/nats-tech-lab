# Business Rules — Reference Data Service (`backend/refdata-service/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping domain rules (BR-001 through BR-019).

Phase 11, [Dictionary-Service-Plan.md](../../.claude/plans/Dictionary-Service-Plan.md). A
separate service, separate Postgres schema (`refdata`) — plain CRUD, not
event-sourced (nothing ever replays a lookup value; see "Event Sourcing vs
Plain CRUD" in `ARCHITECTURE.md`). Rules live in `backend/refdata-service/refdata/internal/domain/dictionary.go`
and are enforced by the command handlers in
`backend/refdata-service/refdata/internal/application/commands/`. BR-D07 (AI-translation
review gate) is parked — not in this pass's scope.

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

### BR-D03 — Locale resolution follows the fallback chain: requested locale → language → default locale → code
Resolving an item's label for a locale never fails outright. It tries the exact requested locale, then the bare language (`de-DE` → `de`), then the context's registered default locale, and finally falls back to the item's code itself as the label.

- **Enforced in:** `domain.ResolveLabel()`, called from `commands.LocalizationHandler.ResolveItem()`
- **Test:** `Dictionary Localization Domain Rules / BR-D03`

---

### BR-D04 — Every mutation to a type's items, localizations, or references bumps that type's set version atomically with the write
Registering/deprecating/deleting an item, setting a localization, or creating a reference each bump `{context, type}`'s set version — a single-statement Postgres `UPSERT ... RETURNING`, so a concurrent bump can never be lost or torn. The bump then rebuilds the affected item's KV cache entry and the type's `_meta` entry (both stamped with the new version) and publishes a bounded change-event pointer on the `REFDATA` stream. This is the write side of the Q5 versioned-read protocol (see ARCHITECTURE.md).

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

### BR-D08 — A consumer resolves reference-data labels KV-first, applying the BR-D03 fallback chain; a miss or stale entry falls back to REST
Phase 11.6. When the shipping backend resolves a reference-data label for display, it reads the `refdata-{context}` KV cache directly and resolves the requested locale's label from the cached localizations map, applying the same fallback chain as BR-D03 (requested locale → bare language → default locale → the code itself). A KV miss or a stale (version-mismatched) entry — the Q5 read protocol's miss case — falls back to the refdata-service REST API with `?locale=`, which resolves the label server-side via the authoritative `ResolveLabel` and backfills the cache. The consumer reimplements the ~10-line fallback rather than importing refdata-service (the two services share only a wire shape); the default locale is a constant mirroring the context's seeded default. Enforced on the *consuming* side (the shipping backend), so it lives here alongside the producer rules it depends on.

- **Enforced in:** `backend/shipping-service/internal/refdataconsumer/Consumer.Lookup()` / `ResolveType()` (`resolveLabel()` implements the fallback; `fetchViaAPI()` forwards `?locale=`)
- **Test:** `backend/shipping-service/internal/refdataconsumer` — `TestLookupResolvesLabelFromKV`, `TestLookupLabelFallsBackToBareLanguage`, `TestLookupLabelFallsBackToDefaultThenCode`, `TestLookupMissForwardsLocaleToAPI`, `TestResolveTypeReturnsAllCodesFromKV`

---

### BR-D09 — Dictionary types are categorized into a small controlled vocabulary
Phase 11.7. Every `DictionaryType` carries a `category` — one of `standards`, `domain-enum`, `ui-copy`, or (reserved for later) `config` — set at type-registration time. Registering a type with any other value is rejected. Category is orthogonal to `context` (tenant/region): it groups *types* by who owns and edits them (see ARCHITECTURE-DICTIONARY.md § "Type Categories & Governance"), not by tenant.

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
