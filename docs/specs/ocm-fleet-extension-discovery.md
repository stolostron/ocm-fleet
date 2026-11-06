# OCM Fleet On-Demand Extension Discovery

**Status:** Draft — decisions resolved; ready for review

**Scope:** OCM Fleet web console extension discovery and plugin loading

## Purpose

Define an OCM Fleet-owned higher-level interface over the Dynamic Plugin SDK extension hooks. The interface will discover plugin manifests declaring requested extension types and load the matching plugins on demand, allowing consumers to obtain extension candidates without requiring every plugin to be loaded first. Existing SDK behavior remains responsible for dynamic extension resolution. The interface is generic over extension type IDs: it matches requested IDs against extension type declarations in the packaged plugin catalog and defines no per-type contracts.

## Scope

### In scope

- OCM Fleet-specific extension discovery over the single packaged plugin catalog, generic over extension type IDs.
- Manifest-based identification of plugins that declare requested extension type IDs, with plugin scope inferred from the matching catalog entry.
- On-demand plugin loading for extension consumers and route/page consumers.
- Replacement of startup-wide eager loading with loading when a plugin is needed.

### Out of scope

- Runtime addon catalogs, per-target availability, authorization, or user-presentation policy.
- Changes to upstream Dynamic Plugin SDK contracts.
- Runtime extension validation.
- Per-extension-type contracts, metadata, versioning, and validation. Related CLI discovery design is separate work.
- Dynamic extension CodeRef resolution; existing SDK resolution remains unchanged.

## Actors and terminology

- **Consumer:** OCM Fleet UI code requesting extensions of one or more types.
- **Application session:** One shell bootstrap in a browser tab, ending when that shell is reloaded or closed. A separate tab has its own session.
- **Plugin manifest:** Plugin metadata containing its declared extensions and the information needed by the plugin loader.
- **Plugin catalog:** The packaged, build-generated `plugin-registry.json`, surfaced to the browser through the existing startup configuration (`scalprumConfig`/`pluginEntries` on `/api/ui/config`). One unit of plugin metadata serving bootstrap, navigation, and discovery.
- **Extension type ID:** A string, such as `ocm-fleet.cluster-provider`, matched against declarations in plugin manifests.

## Repository context

- The SDK hooks `useExtensions` and `useResolvedExtensions` operate on loaded plugins. The current `PluginLoader` loads every configured plugin before rendering children; demand loading requires a change to that startup behavior.
- `tools/generate-plugin-registry.mjs` embeds emitted plugin manifests under their plugin names. Each manifest declares extension types. The resource manager includes this plugin metadata in `/api/ui/config`; entries can also reference a manifest location.
- Discovery matches manifest type IDs without a per-type registry. An index is an implementation detail. Registration, enablement, target capability, presentation, and authorization are distinct concerns.
- The shell uses startup catalog configuration for the session. A reload obtains new configuration; no catalog revision or runtime refresh is required. A location-only manifest can reflect what its URL serves when fetched. A reload notification is future work, not part of this feature.
- Asset delivery is expected to retain older plugin assets long enough for existing sessions to continue loading them. Retention duration and implementation are not defined here.

## Functional requirements

### REQ-001 — Provide OCM Fleet-specific extension discovery

The OCM Fleet web console MUST provide a higher-level extension-discovery interface over the existing extension hooks for consumers that request extension type IDs.

**Verification:** A consumer fixture requests an OCM Fleet extension type through the Fleet-owned interface and obtains extension candidates without directly orchestrating the SDK discovery hooks.

### REQ-002 — Discover candidate plugins from manifests

WHEN a consumer requests one or more extension type IDs, the OCM Fleet discovery interface MUST identify plugins in the packaged plugin catalog whose manifests declare at least one requested type.

**Verification:** Given packaged manifests with matching and non-matching extension declarations, request multiple types and assert that only plugins declaring at least one requested type are selected as candidates.

### REQ-003 — Load matching plugins on demand

WHEN a consumer requests one or more extension type IDs and a plugin manifest declares at least one requested type, the OCM Fleet discovery interface MUST load that plugin so its matching extensions can be consumed.

**Verification:** With a deferred plugin loader and an initially unloaded matching plugin, assert that the request causes the plugin to load and its declared extension becomes available to the consumer.

### REQ-004 — Do not load unrelated plugins for an extension request

WHEN a consumer requests one or more extension type IDs, the OCM Fleet discovery interface MUST NOT load a plugin whose manifest declares none of the requested types solely to satisfy that request.

**Verification:** Request one type from a fixture catalog containing matching and non-matching plugins; assert matching plugins load and non-matching plugins remain unloaded.

### REQ-005 — Defer plugin loading until a consumer needs a plugin

WHEN the console starts, the plugin loader MUST NOT load every packaged plugin solely because it appears in the plugin catalog.

**Verification:** Start the shell with multiple configured plugins and assert that plugin manifests/configuration are available while no remote plugin is loaded before a route or extension consumer requests it.

### REQ-006 — Load route plugins when their pages are needed

WHEN navigation selects a route provided by an unloaded plugin, the console MUST load that plugin before rendering its route module.

**Verification:** Navigate to a route provided by an initially unloaded plugin and assert its module renders after the plugin load completes.

### REQ-007 — Use the existing runtime plugin catalog

The OCM Fleet discovery interface MUST use the packaged plugin catalog — the same plugin metadata the shell already receives through startup configuration — as its single runtime source of plugin inventory and extension declarations, and MUST NOT require a separate runtime registry for discovery.

**Verification:** Configure the shell with packaged plugin metadata and assert the discovery interface reads that same data without requiring an additional registry, endpoint, or duplicated configuration.

**Note:** Discovery does not define or validate per-type contracts.

### REQ-008 — Apply optional predicates after type discovery

WHERE a consumer supplies a predicate, the OCM Fleet discovery interface MUST apply that predicate to extensions discovered through the requested type IDs.

**Verification:** Request a type ID with a predicate that accepts only one of multiple matching extensions; assert plugin discovery is based on the type ID and returned extensions satisfy the predicate.

### REQ-009 — Prefer embedded manifest declarations

WHEN a packaged plugin catalog entry contains embedded extension declarations, the discovery interface MUST use those declarations to match requested type IDs without fetching that plugin's manifest location.

**Verification:** Provide an entry with embedded extension declarations and a manifest URL; assert type matching uses embedded metadata and makes no manifest request.

### REQ-010 — Fetch missing manifest declarations

IF a packaged plugin catalog entry lacks embedded extension declarations and provides a manifest location, the discovery interface MUST fetch that manifest location to determine its declared extension types.

**Verification:** Provide an entry with only `manifestLocation`; assert discovery fetches and matches its manifest before attempting to load plugin code.

### REQ-011 — Preserve successful matches when another plugin fails

IF loading one candidate plugin fails while another candidate plugin succeeds, the discovery interface MUST return extensions from the successful plugin.

**Verification:** Request a type declared by two plugins, make one load fail and the other succeed, and assert the successful plugin's extension candidates remain in the result.

### REQ-012 — Report candidate plugin failures

IF manifest discovery or plugin loading fails for a catalog entry, the discovery interface MUST report the catalog-entry identity, failure stage, stable diagnostic code, and safe message.

**Verification:** Cause a manifest-fetch and plugin-load failure in separate fixtures; assert each diagnostic includes entry identity, stage, code, and safe message separately from successful extension candidates.

### REQ-013 — Share plugin loads across consumers

WHEN multiple consumers request extensions from the same plugin while its load is in progress, the plugin-loading system MUST coalesce those requests into one plugin load operation.

**Verification:** Mount two consumers requesting extension types from the same unloaded plugin concurrently and assert the loader is invoked once.

### REQ-014 — Reuse successfully loaded plugins

WHEN a plugin has loaded successfully, the plugin-loading system MUST reuse that loaded plugin for subsequent extension or route/page requests in the same application runtime.

**Verification:** Request two extension types from one plugin sequentially and assert the second request does not invoke another plugin load.

### REQ-015 — Bound explicit retries

WHEN a consumer calls `retry(pluginKey)` for a failed manifest or plugin-load operation, the system MUST permit no more than two retries after that operation's initial attempt in the current application session.

**Verification:** Fail a manifest fetch twice, then succeed; fail the subsequent plugin load and assert it has its own two retries. A fourth attempt for either failed operation does not run.

### REQ-017 — Use packaged catalog membership as load eligibility

WHEN a route or extension consumer needs a plugin present in the packaged plugin catalog, the plugin-loading system MUST permit that plugin to load without requiring a separate addon-enablement signal.

**Verification:** Request a plugin present in the packaged catalog with no enablement record and assert it loads; verify that authorization checks remain outside this plugin-loading decision.

### REQ-018 — Return structured discovery results

The OCM Fleet discovery interface MUST return a structured result containing matching extensions, aggregate request status, and diagnostics identifying relevant plugins.

**Verification:** Complete a request with one successful and one failed candidate plugin; assert the result exposes extensions, status, and diagnostics as distinct structured fields.

### REQ-019 — Continue discovery after a manifest-fetch failure

IF fetching one plugin manifest for discovery fails, the discovery interface MUST continue processing other packaged catalog entries.

**Verification:** Provide two entries where the first manifest fetch fails and the second declares a requested type; assert the second plugin is discovered and the first failure is reported.

### REQ-020 — Use the startup catalog for the whole session

WHILE an application session is running, the discovery system MUST use the plugin catalog configuration received at application startup rather than fetching a new catalog.

**Verification:** Start a session, change the served catalog data mid-session, and assert discovery keeps using the startup data and makes no additional catalog request.

### REQ-021 — Do not reuse in-memory results across application sessions

WHEN a new application session starts, the discovery system MUST NOT reuse in-memory discovery results from a prior application session.

**Verification:** Complete a request, start a fresh application session, and assert manifest discovery runs against the current packaged catalog.

### REQ-023 — Report loading status while discovery is pending

WHILE manifest discovery or plugin loading remains pending for a request, the structured result MUST report status `loading`.

**Verification:** Hold a manifest fetch or plugin load pending and assert the consumer observes `loading` before the operation completes.

### REQ-024 — Report complete status for successful requests

WHEN all manifest and plugin operations for a request complete without errors, the structured result MUST report status `complete`, including when no extensions match.

**Verification:** Run one request with matching extensions and one request with no matches; assert both report `complete` when neither has errors.

### REQ-025 — Report partial status when results and errors coexist

IF all discovery and plugin-load operations have finished and a request has both a matching extension and a failed catalog entry, the structured result MUST report status `partial`.

**Verification:** Request a type declared by two plugins where one loads successfully and one fails, then assert results are returned with status `partial`.

### REQ-026 — Report failed status when no usable results remain

IF all discovery and plugin-load operations have finished and a request has no matching extensions and at least one failed catalog entry, the structured result MUST report status `failed`.

**Verification:** Make all candidate manifest fetches or plugin loads fail and assert the result reports `failed` with diagnostics.

### REQ-027 — Do not retry failed operations automatically

IF a manifest or plugin operation fails, the system MUST NOT start another attempt without an explicit consumer retry.

**Verification:** Fail a manifest fetch or plugin load and assert no second attempt occurs until a consumer invokes the retry operation.

### REQ-029 — Load new catalog configuration after reload

WHEN a new application session starts, the console MUST obtain the current startup plugin catalog configuration.

**Verification:** Start a session, change the served startup configuration, then start a new session and assert it receives the updated configuration.

### REQ-030 — Skip entries with no discoverable manifest metadata

IF a packaged plugin catalog entry has neither embedded extension declarations nor a usable manifest location, the discovery interface MUST report a diagnostic for that entry.

**Verification:** Provide an entry with no extension metadata or usable manifest location and assert its diagnostic identifies the entry.

### REQ-033 — Use stable diagnostic codes

WHEN the discovery interface reports a diagnostic, it MUST use a stage and stable code from the diagnostic code set defined in [Diagnostic codes](#diagnostic-codes).

**Verification:** Trigger each failure class in the diagnostic code table and assert the reported diagnostic uses the corresponding stage and code.

### REQ-034 — Expose retry availability per failed entry

The structured result MUST report, for each failed catalog entry, the number of attempts used for the failed operation and whether another retry remains available in the current application session.

**Verification:** Fail an entry, call `retry(pluginKey)` once, and assert the result reports two attempts used with one retry remaining; after the final attempt, assert the result reports no retry remaining.

### REQ-035 — Treat exhausted retries as no-ops

WHEN a consumer calls `retry(pluginKey)` for an entry whose attempt budget is exhausted, the discovery interface MUST NOT start a new attempt.

**Verification:** Exhaust an entry's three total attempts, call `retry(pluginKey)` again, and assert no fourth attempt runs.

### REQ-036 — Match type IDs without per-type registration

The OCM Fleet discovery interface MUST match requested extension type IDs against plugin manifest declarations without requiring per-type registration, contract metadata, or a Fleet-side type list.

**Verification:** Request a type ID declared only in a fixture manifest, with no Fleet-side type declaration, and assert the declaring plugin is discovered.

### REQ-037 — Use the discovered manifest for loading

WHEN discovery matches an extension type in a fetched manifest, the plugin-loading system MUST load the plugin using that same manifest.

**Verification:** Fetch a matching manifest, change the response at its URL, and assert the load receives the manifest used for matching rather than the changed response.

### REQ-038 — Clear recovered failures

WHEN a retry of a failed operation succeeds, the discovery interface MUST remove that operation's previous failure diagnostic from the current result.

**Verification:** Fail a plugin load, retry successfully, and assert the failed-load diagnostic is no longer in the result and the aggregate status reflects the remaining results and errors.

### REQ-039 — Identify the declaring plugin scope

WHEN a manifest declares a requested extension type, the discovery interface MUST identify the declaring plugin's scope from that manifest's plugin name.

**Verification:** Request a type declared in a plugin manifest and assert the load uses that manifest's plugin name as its scope.

### REQ-040 — Continue after missing manifest metadata

IF a catalog entry has neither embedded extension declarations nor a usable manifest location, the discovery interface MUST continue processing other catalog entries.

**Verification:** Provide one entry with missing manifest metadata and another valid matching entry; assert the valid entry is discovered.

### REQ-041 — Retain failure after an exhausted retry

WHEN a consumer calls `retry(pluginKey)` for an entry whose attempt budget is exhausted, the discovery interface MUST retain that operation's last failure diagnostic.

**Verification:** Exhaust an operation's attempt budget, call `retry(pluginKey)` again, and assert its last failure diagnostic is unchanged.

## Diagnostic codes

Diagnostics identify specific failure points. They are distinct from the aggregate request statuses (`loading`, `complete`, `partial`, `failed`), which summarize the whole request.

| Stage | Code | Failure |
|-------|------|---------|
| `catalog` | `missing-manifest` | Entry has neither embedded extension declarations nor a usable manifest location. |
| `manifest-fetch` | `client-error` | Manifest location fetch returned a client error (for example 404, 401, 403). |
| `manifest-fetch` | `network-error` | Manifest location fetch failed at transport or server level (5xx, timeout, connection failure). |
| `manifest-parse` | `malformed` | Fetched manifest content could not be read or parsed as a valid manifest. |
| `plugin-load` | `load-failed` | Module Federation remote failed to load or initialize. |

Errors thrown by loaded extension code during consumer execution are not discovery diagnostics; they remain the consumer's responsibility, consistent with the exclusion of dynamic extension resolution.

## Retry semantics

All failure codes in the diagnostic code set are equally eligible for retry. Retryability is governed by the per-plugin attempt budget, not by the failure code: a `manifest-fetch/client-error` is as retryable as a `plugin-load/load-failed`.

Retry exhaustion is entry state, not a diagnostic code. The last failure diagnostic remains the description of what went wrong; the structured result separately reports attempts used and remaining retry availability for that entry (REQ-034). Calling `retry(pluginKey)` on an exhausted entry is a no-op that changes neither the diagnostics nor the attempt count (REQ-035).

The attempt budget applies separately to each failed manifest or plugin-load operation in an application session. A new session starts with fresh budgets.

## Normative language

Uppercase normative keywords follow BCP 14 (RFC 2119 and RFC 8174). This specification prefers `MUST` and `MUST NOT` for clarity.

## Acceptance scenarios

1. **Demand loading:** Request one or more type IDs. Plugins declaring those types load and supply matching candidates; unrelated plugins remain unloaded. An unmatched type returns `complete` with no matches.
2. **Navigation:** Start the shell without loading every configured plugin. Navigating to a page loads its plugin before rendering the route module.
3. **Partial failure and recovery:** One matching plugin fails while another succeeds. The result includes successful candidates and the failure diagnostic. A successful explicit retry clears that diagnostic and updates the status.
4. **Retry limit:** Each failed manifest or plugin-load operation has at most three attempts in a session. Retry availability shows exhaustion; further retries do not start that operation. A new session starts with a fresh budget.
5. **Catalog and manifests:** A session keeps its startup catalog configuration. A manifest URL may return newer content when fetched, but discovery and loading use the same fetched manifest. A new session receives current startup configuration.

## Traceability

- User request: add an OCM Fleet-specific higher-level wrapper for extension hooks that discovers manifests and loads plugins on demand for requested extension types, using an opinionated registry.
- Sources: `tools/generate-plugin-registry.mjs`, `server/internal/transport/http/uiconfig.go`, `client/web/src/components/Root/PluginLoader.tsx`, `client/web/src/hooks/useSetupExtensions.ts`, and `docs/ui/plugin-system.md`.
- Superseded decisions: a separate extension-contract registry and catalog content digest are not needed. Requirements REQ-016, REQ-022, REQ-028, REQ-031, and REQ-032 were retired with the per-type contract proposal; their IDs are not reused.
