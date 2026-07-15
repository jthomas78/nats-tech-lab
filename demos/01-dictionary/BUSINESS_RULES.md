# Business Rules — Shipping Domain

Domain rules enforced before any event is published to JetStream. A rule
violation returns an error to the caller; no event is written.

Two aggregates share the single `SHIPPING` stream (Phase 8):

- **Ship** rules live in `dictionary/internal/domain/ship.go`
- **Container** rules live in `dictionary/internal/domain/container.go`

Cross-aggregate rules (BR-008, BR-012, BR-014) need both aggregates' state.
Both hydrate from **one atomic replay** of the `SHIPPING` stream
(`commands.hydratePair`), so these checks are strongly consistent. Phase 14
splits the stream and turns exactly these rules into the
invariant-spanning-two-aggregates problem.

All rules must have a corresponding test: ship rules in
`dictionary/integration_test.go`, container rules in
`dictionary/container_test.go`.

---

## Ship Rules

### BR-001 — Cannot arrive at a port already docked at
A ship that is currently docked at port X cannot arrive at port X again.

- **Error:** `ErrAlreadyDocked` — "ship is already docked at this port"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-001`

---

### BR-002 — Must depart before arriving at a new port
A ship that is currently docked at port X cannot arrive at port Y without first departing port X.

- **Error:** `ErrMustDepart` — "ship must depart current port first (X)"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-002`

---

### BR-003 — Cannot depart a port the ship is not at
A ship can only depart the port it is currently docked at. Attempting to depart a different port, or departing while already at sea, is rejected.

- **Error:** `ErrNotDocked` — "ship is not docked at this port (currently: X)"
- **Enforced in:** `ShipAggregate.Depart()`
- **Test:** `Domain Rules / BR-003`

---

### BR-017 — A ship can only arrive at a registered port
Ports are reference data (a Postgres `ports` table, not an event-sourced aggregate — registered via `POST /api/ports`). Arriving at a port that isn't registered is rejected.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ShipAggregate.Arrive()` — the application layer (`ShipHandler.ArrivePort`) resolves `portKnown` via `domain.PortRepository.Exists()` and passes it in as a parameter, the same pattern used for the cross-aggregate checks in `container.go`.
- **Test:** `Domain Rules / BR-017`

---

## Retired Rules (Phase 8)

Cargo moved off the ship aggregate: a ship's manifest is now the container
join (`onShipID == shipID`). The ship-cargo rules were retired and replaced:

| Retired | Was | Replaced by |
|---|---|---|
| BR-004 | Cannot load cargo unless docked | BR-012 |
| BR-005 | Cannot unload cargo unless docked | BR-012 |
| BR-006 | Cannot unload cargo not in the manifest | BR-011 + BR-013 |
| BR-007 | Cargo payload required (input validation) | container input validation in `commands/container.go` |

---

## Container Rules

### BR-008 — Cannot load a container already at its destination
A container whose destination port matches the ship's current port cannot be loaded — it has already been delivered.

- **Error:** `ErrContainerAtDestination` — "container destination matches the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-008`

---

### BR-009 — A container can only be unloaded at its destination port
Unloading anywhere other than the container's registered destination is rejected.

- **Error:** `ErrWrongDestination` — "container can only be unloaded at its destination port"
- **Enforced in:** `ContainerAggregate.Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-009`

---

### BR-010 — A container must be in-terminal to be loaded
Only a container sitting in a terminal yard can be crane-loaded. A container already on a ship cannot be loaded again.

- **Error:** `ErrContainerNotInTerminal` — "container must be in a terminal to be loaded"
- **Enforced in:** `ContainerAggregate.Load()`
- **Test:** `Container Domain Rules / BR-010`

---

### BR-011 — A container must be on-ship to be unloaded
Only a container currently on a ship can be unloaded. A container in a yard cannot be unloaded.

- **Error:** `ErrContainerNotOnShip` — "container must be on a ship to be unloaded"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-011`

---

### BR-012 — A ship must be docked to load or unload containers
Container operations require the ship to be in port; a ship at sea can do neither.

- **Error:** `ErrNotInPort` (defined in ship.go, reused) — "ship must be docked to load or unload containers"
- **Enforced in:** `ContainerAggregate.Load()` / `Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-012` (load + unload variants)

---

### BR-013 — A container can only be unloaded from the ship it is actually on
Unloading a container from a ship that is not carrying it (`onShipID != shipID`) is rejected.

- **Error:** `ErrWrongShip` — "container is not on this ship"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-013`

---

### BR-014 — A container can only be loaded when the ship is docked at the container's terminal port
Loading pulls a container from a specific yard: the ship must be docked at that yard's port (`terminalPort == ship.currentPort`). Without this rule a ship docked in Singapore could load a container sitting in Rotterdam.

- **Error:** `ErrContainerNotAtPort` — "container is not in a terminal at the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-014`

---

### BR-015 — A container ID can only be registered once
Re-registering an existing container ID would silently reset its state (e.g. teleporting an on-ship container back into a yard).

- **Error:** `ErrContainerExists` — "container is already registered"
- **Enforced in:** `ContainerAggregate.Register()` — the rule decision stays in the domain (`c.registered`), but since Phase 8.3 the container's identity is a surrogate key (UUID), so uniqueness is a **natural-key** constraint. The application (`RegisterContainer`) resolves the natural key against the event stream (`hydrateByNaturalKey`) and folds any existing `.registered` event in, so the domain still sees `c.registered == true` and rejects the duplicate. Resolution is against the authoritative event log, not an eventually-consistent read projection.
- **Test:** `Container Domain Rules / BR-015` and `Container Domain Rules / surrogate key …`

---

### BR-016 — A container ID must be in ISO 6346 format (TCKU + 7 digits)
Every container ID must start with the fixed owner prefix `TCKU` (case-sensitive) followed by exactly 7 digits, e.g. `TCKU1234567`. This lab fixes the owner code rather than validating the full ISO 6346 owner-code space.

- **Error:** `ErrInvalidContainerID` — "container ID must be in ISO 6346 format: TCKU followed by 7 digits"
- **Enforced in:** `ContainerAggregate.Register()`
- **Test:** `Container Domain Rules / BR-016`

---

### BR-018 — A container's origin and destination ports must both be registered
Registering a container with an origin or destination port that isn't in the ports registry is rejected. Reuses `ErrUnknownPort` (BR-017's error), since it's the same underlying rule applied to the container's two port fields instead of a ship's arrival port.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ContainerAggregate.Register()` — checked after BR-016 (format) and before BR-015 (duplicate registration). The application layer (`ContainerHandler.RegisterContainer`) resolves `originKnown`/`destKnown` via `domain.PortRepository.Exists()`.
- **Test:** `Container Domain Rules / BR-018`

---

## Guards (not numbered rules)

- **Unregistered container** — load/unload of a container with no `.registered`
  event returns `ErrContainerNotFound` (entity existence, mapped to HTTP 404).
- **Input validation** — required-field checks (`containerID is required`,
  `shipID is required`, …) live in the application layer
  (`commands/container.go`), fire before the aggregate is consulted, and are
  deliberately **not** domain rules — same classification as the retired BR-007.

---

## AIS Navigational Status

Ships carry an AIS-aligned status derived from their current state. This is a read-model concern (set in `ShipAggregate.State()`) and not a domain rule, but it is documented here for reference.

| Status constant | JSON value | Condition | UI colour |
|---|---|---|---|
| `StatusDocked` | `"docked"` | `CurrentPort != ""` | Green |
| `StatusInTransit` | `"in-transit"` | `CurrentPort == ""` | Blue |
| `StatusAtAnchor` | `"at-anchor"` | _(future domain event)_ | Amber |
| `StatusNotUnderCommand` | `"not-under-command"` | _(future domain event)_ | Red |
| `StatusRestrictedManoeuvrability` | `"restricted-manoeuvrability"` | _(future domain event)_ | Orange |

## Container Status

| Status constant | JSON value | Condition |
|---|---|---|
| `ContainerInTerminal` | `"in-terminal"` | `terminalPort` set, `onShipID` nil |
| `ContainerOnShip` | `"on-ship"` | `onShipID` set, `terminalPort` nil |

---

# Business Rules — Reference Data Service (`refdata-service/`)

Phase 11, [Dictionary-Service-Plan.md](../../.claude/plans/Dictionary-Service-Plan.md). A
separate service, separate Postgres schema (`refdata`) — plain CRUD, not
event-sourced (nothing ever replays a lookup value; see "Event Sourcing vs
Plain CRUD" above). Rules live in `refdata-service/refdata/internal/domain/dictionary.go`
and are enforced by the command handlers in
`refdata-service/refdata/internal/application/commands/`. BR-D07 (AI-translation
review gate) is parked — not in this pass's scope.

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

- **Enforced in:** `backend/internal/refdataconsumer/Consumer.Lookup()` / `ResolveType()` (`resolveLabel()` implements the fallback; `fetchViaAPI()` forwards `?locale=`)
- **Test:** `backend/internal/refdataconsumer` — `TestLookupResolvesLabelFromKV`, `TestLookupLabelFallsBackToBareLanguage`, `TestLookupLabelFallsBackToDefaultThenCode`, `TestLookupMissForwardsLocaleToAPI`, `TestResolveTypeReturnsAllCodesFromKV`

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
Phase 11.8. `frontend-dict`'s locale dropdown previously defaulted to an empty selection, which resolves items by their raw code instead of a localized label (BR-D03). It now defaults to `en` on load, matching the precedent already established in `shared/refdata/useRefdataLabels.js` for the other two frontends. A user can still explicitly pick `(code)` from the dropdown to see raw codes.

- **Enforced in:** `frontend-dict/src/stores/dictionary.js` — `selectedLocale` initial state
- **Test:** manual — verified in browser (frontend build has no JS test harness; see BR-D11's note)

---

### BR-D14 — At most one default locale per context
Phase 11.9. The default locale is the BR-D03 fallback target, so it must be unambiguous: registering a locale with `isDefault=true` *moves* the default — the previously-default locale is unmarked in the same transaction. There is no separate "set default" operation; re-registering an existing locale is an upsert, so `POST /api/refdata/admin/locales` with `isDefault=true` is both "add as default" and "make this the default". The admin UI renders this as a radio column (exactly-one semantics): the current default cannot be un-set, only replaced.

- **Enforced in:** `postgres.LocaleRepository.Add()` — clears `is_default` for the context and sets the new default in one transaction
- **Test:** `Localization Domain Rules / Locale management` — "moves the default when another locale is marked default"

---

### BR-D16 — All Port-UI copy resolves through the i18n/refdata layer
Phase 11.10. Every user-facing string in `frontend-port` — including headings, controls,
form labels, validation and error feedback, empty states, accessibility labels, derived
status labels, and notifications — is addressed by a `ui-copy` code through vue-i18n.
The `ui-copy` seed is the sole authored source for both English and Spanish. A committed,
generated English catalog provides the BR-D11 cold-paint fallback when refdata is
unreachable; live refdata overlays that catalog once loaded.

- **Enforced in:** `frontend-port` Vue components via `t()` plus the generated
  `shared/refdata/uiCopyFallback.en.js` catalog
- **Test:** `frontend-port/scripts/check-i18n.mjs` rejects bare user-facing literals;
  `npm run check:i18n` regenerates the fallback and rejects drift
