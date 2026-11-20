# Generic Node Plugin Loading in Scalprum

**Status:** Design recorded, consolidated 2026-10-22; ready for ticket planning. Implementation qualification is pending. No implementation is claimed; only the one-time bootstrap release in C28 is planned, while later production releases require separate approval.

## Purpose and scope

Enable generic Node plugin loading through Scalprum core and a separate opt-in Node loader package. Consumers own application semantics, packaging, distribution, and renderer bindings. The initial producer plugin will be implemented in OCM Fleet; Fleet CLI discovery, cache, and application integration remain a later milestone. This document retains its original Fleet location for review continuity.

**In scope:** SDK-compatible loader injection, Node-safe initialization, SDK-shaped manifests and extensions, executable entry/chunk acquisition, and Node/browser/package contract tests. The initial producer plugin will be a separate Nx project in the OCM Fleet SDK workspace, implementing a Rspack Node producer plugin that mirrors the web plugin SDK API and emits Node-targeted standard remotes for the Scalprum adapter. OCM Fleet will not publish this project; its boundary should allow a future move into Scalprum or the Dynamic Plugins SDK.

**Out of scope:** Fleet discovery/authentication, CLI/TUI/Ink features, filesystem caching/offline policy, artifact distribution/signing, React-core renderer changes/provider forwarding, hot replacement, and multiple plugin versions in one process. Existing browser bundles are not assumed executable in Node.

Uppercase normative keywords follow BCP 14 ([RFC 2119](https://www.rfc-editor.org/rfc/rfc2119), [RFC 8174](https://www.rfc-editor.org/rfc/rfc8174)). This specification prefers `MUST` / `MUST NOT` for clarity.

### Reading guide and label legend

| Label | Meaning |
| --- | --- |
| `REQ-###` | Stable requirement ID. Each active requirement uses EARS (Easy Approach to Requirements Syntax) and includes a title, observable behavior, and verification criterion. |
| `C#` | Conversation decision source. The settled-decisions table records the outcome; several source labels can support one decision. |

Requirement IDs are non-consecutive because consolidated, withdrawn, and deferred requirements retain their original IDs; the consolidation table records their disposition. Verification entries describe future acceptance tests, not passed tests. Inspected source evidence is identified separately below.

## Settled decisions

Source labels retain traceability to the earlier conversation; these decisions are not open questions.

| Sources | Decision |
| --- | --- |
| C1, C3, C5 | Scalprum distinguishes Node/browser runtimes, not CLI/TUI application categories. Consumers define extension semantics and asset distribution policy. Fleet adoption follows separately. |
| C2 | Use a custom SDK-compatible loader now. Consumers depend on its structural SDK interface and core injection surface, allowing implementation replacement later. Upstream acceptance is not promised. |
| C4, C7, C9 | Separate web/Node artifacts and manifests; retain SDK-shaped public manifests, exposed keys, CodeRef notation, and lookup arguments. Enhanced `mf-manifest.json` is not the public contract. |
| C8, C12, C13 | Preserve SDK extension, script-set, and error contracts. Derive runtime extensions from immutable metadata. The runtime loads the producer-identified container entry and its required runtime dependencies before readiness; the earlier strategy of manually loading every declared script before initialization is superseded by C23/C25. |
| C10, C11 | Preserve embedded/on-demand manifest acquisition and applicable SDK loader options. Scalprum-owned manifest fetch need not pass through loader `fetchImpl`. |
| C14 | Host owns `transformPluginManifest` and runtime asset-base decisions. For `baseURL: "auto"`, host transform may resolve `loadScripts` to absolute runtime paths (for example, a CRD path). Node loader consumes transformed values; it does not guess a default URL or derive one from the manifest URL. |
| C15, C17 | The opt-in package is named `@scalprum/node-loader`; Node-only runtime dependencies stay out of core/browser-only installs. Preserve core exports, including `Container`. No separate types package or Node-loader runtime dependency on core is needed; see the [core package-boundary evidence](#core-package-boundary-evidence). A one-time `0.0.1` bootstrap release is planned for provenance verification under C28; later production releases require separate approval. |
| C18 | `@scalprum/react-core` remains DOM-oriented with its existing `react-dom` peer. Other-renderer consumers can build bindings on core; no renderer-neutral rewrite or Ink prerequisite. |
| C19 | Initial producer plugin lives in OCM Fleet, mirrors the web plugin SDK API, and builds Node remotes with Rspack. The CLI host and plugins are Rspack-bundled and execute on native Node. Remotes expose standard Module Federation `init`/`get`; a Scalprum adapter loads them. Runtime support floor is Node 22+. Rspack producer/host configurations own artifact format; the emitted pairing must be qualified. |
| C20 | Dynamic Plugin SDK 9.1.0 is the compatibility baseline, matching current Scalprum core; core declares `^9.1.0` and currently resolves 9.1.0. No SDK 10.x release was found in the npm registry check on 2026-10-22. V1 targets standard `custom` registration; callback mode is not included unless a Node use case is later established. |
| C21 | Shared modules are configured through the Rspack build plugin. Its Module Federation runtime initializes remotes and uses the host's configured share scope (default name `default`); the adapter should not call remote `init` or register shares directly. |
| C22 | If configuration or a supported-mode request requires behavior the Node loader does not implement, report an explicit “not supported yet” error naming that behavior; do not silently ignore it. C24/REQ-067 specifies the specialized configuration diagnostic for raw `sharedScope`. Configuration errors may throw; plugin-load errors preserve SDK `loadPlugin()` failure-result semantics. |
| C23 | Prefer Module Federation Enhanced runtime APIs, including `getInstance`, `registerRemotes`, and `loadRemote`, for runtime setup, remote registration, exposed modules, and lazy chunks. Shared modules come from Rspack build configuration. Keep custom code to the SDK-loader adapter and demonstrated compatibility gaps; do not create a parallel asset-fetch/chunk-loading layer. |
| C24 | Node v1 does not implement a runtime override via SDK `PluginLoaderOptions.sharedScope`; configure shared modules through Rspack. If the raw option is explicitly supplied, throw an error naming `sharedScope`, stating “not implemented yet”, and directing the consumer to configure sharing in Rspack; reconsider only if a consumer needs it. |
| C25 | `registerRemotes` accepts a remote name and direct JavaScript entry URL; it does not require `mf-manifest.json`. The SDK manifest's name and host-transformed entry path provide those values; `loadRemote` delegates expose and chunk loading to the MF runtime. |
| C26 | The opt-in Node loader package declares `@module-federation/enhanced` as a peer dependency so it uses the Rspack host's Module Federation runtime instance rather than bundling a private copy. |
| C27 | Core `getModule()` supports embedded `AppMetadata.pluginManifest` as well as `manifestLocation`, matching React's existing source selection: `manifestLocation ?? pluginManifest`. The embedded manifest is used when the URL is absent, not as a fallback after a URL fetch failure. |
| C28 | Use a minimal, manually initiated `0.0.1` release to validate Nx release automation, GitHub OIDC trusted publishing, npm provenance, and expected release tags. After verification, check dependents and npm policy; unpublish the bootstrap version only if safe and permitted, otherwise deprecate it. |
| C29 | The OCM Fleet Node producer plugin is its own Nx project in the SDK workspace. Do not publish it from OCM Fleet. Keep its project boundary suitable for a future move into Scalprum or the Dynamic Plugins SDK. |

**Packaging guidance:** Host code composes core `initialize()` with the injected SDK-compatible loader; package-local Node options/types stay in the loader package. Core may be a dev dependency for integration tests only. Using `@openshift/dynamic-plugin-sdk` as a peer plus dev dependency is a recommendation, not a requirement. Core currently declares SDK as a normal dependency; SDK uses a module-local `Symbol('CodeRef')`, so duplicate runtime instances can break CodeRef recognition. REQ-061 governs runtime identity regardless of dependency declaration. Public export spellings, SDK dependency declaration, and dependency ranges are ticket-level packaging metadata to verify against the confirmed contract, not open design questions.

## Requirements

"SDK" means the Dynamic Plugin SDK 9.1.0 API baseline in v1 `custom` registration mode, with applicable fields/options and documented exclusions covered by implementation qualification. A *supported artifact* belongs to a Node/bundler/container/module-format tuple qualified through the checklist below. The host transform supplies the remote entry location; the Module Federation runtime owns executable acquisition. Requirements do not claim compatibility with every Node artifact, future Node release, or SDK version.

### Core runtime and injection

| ID and title | EARS requirement | Verification |
| --- | --- | --- |
| REQ-025: Application independence | The Node loader MUST NOT require a consumer application category to load a plugin. | Load the same fixture from differently categorized hosts without CLI/TUI flags. |
| REQ-026: Opaque consumer extensions | The Node loader MUST preserve consumer-defined extension properties without interpreting them as application commands or screens. | Opaque nested metadata survives SDK extension loading without application registration side effects. |
| REQ-027: SDK interface | The Node loader MUST implement the selected SDK `PluginLoaderInterface`. | Type-check structural assignment and exercise the interface through `PluginStore`. |
| REQ-028: Core injection | WHEN a consumer supplies an SDK-compatible loader during initialization, Scalprum MUST use that loader for plugin loading. | The same host initialization accepts two unrelated structural implementations. |
| REQ-030: Node-safe initialization | WHEN a consumer supplies a compatible loader in Node, Scalprum core MUST initialize without requiring browser globals or eagerly initializing browser-default sharing. | Initialize with a structural test loader in a fresh subprocess without browser globals. Node Rspack sharing may be initialized by the selected loader path. |
| REQ-031: No browser shims | WHILE operating in Node, the Node loader MUST NOT create simulated browser globals. | Globals including `window`, `document`, and `self` remain absent across initialization, entry execution, and dynamic import. |
| REQ-003: Module resolution | WHEN a Node host requests a declared exposed module/export from a supported plugin, Scalprum MUST resolve that module/export in Node. | Real producer output resolves default and named exports. |
| REQ-005: Host API | WHEN a plugin accesses the API configured by its Scalprum host, Scalprum MUST provide that configured API. | Renderer-independent fixture observes the host-supplied API. |
| REQ-041: Browser regression | WHEN an existing browser consumer initializes without a Node loader, Scalprum MUST retain its supported browser plugin-loading behavior. | Browser fixture covers existing defaults, transforms, React bindings, and core exports. |

### Manifests, extensions, and executable assets

| ID and title | EARS requirement | Verification |
| --- | --- | --- |
| REQ-032: SDK-shaped manifest | WHEN supplied a supported SDK-shaped Node manifest, the Node loader MUST accept it without another target's manifest or an enhanced federation manifest. | Load with only the Node SDK-shaped manifest available. |
| REQ-033: Manifest semantics | WHEN processing supported SDK manifest fields, the Node loader MUST preserve their documented SDK semantics. | Versioned compatibility tests cover identity, build hash, entry registration, extension metadata, and dependencies. |
| REQ-006: Immutable metadata | WHEN a manifest is submitted for loading, the Node loader MUST preserve its contents. | Deep-freeze nested input; load and resolve extensions; compare input before/after. |
| REQ-044: Loaded extensions | WHEN plugin loading succeeds, the Node loader MUST return SDK `LoadedExtension` objects in `PluginLoadResult`. | Existing SDK discovery observes identity metadata and recognized lazy CodeRefs; no eager reference resolution is needed to load extensions. |
| REQ-045: Resolved extensions | WHEN existing SDK APIs resolve extensions from the Node loader, the results MUST conform to `LoadedAndResolvedExtension`. | Unchanged extension consumer resolves nested CodeRefs through existing SDK APIs. |
| REQ-048: CodeRef notation | The Node loader MUST interpret encoded CodeRefs using the supported web SDK's module/export notation. | Identical `Example` and `Example.namedExport` references resolve in separate web/Node fixtures. |
| REQ-049: Public lookup names | WHEN a consumer requests corresponding web/Node exports, Scalprum MUST accept the same public exposed keys and scope/module/import-name conventions. | Both fixtures accept `getModule(scope, 'Example', 'namedExport')`; internal `./Example` adaptation does not leak into public keys. |
| REQ-050: Embedded metadata | WHEN a scope has `pluginManifest` but no `manifestLocation`, core `getModule()` MUST load the embedded manifest rather than fail for a missing URL. | Configure only `AppMetadata.pluginManifest`; call `getModule()` and verify module load with no manifest fetch. |
| REQ-051: On-demand metadata | WHEN a module request requires an unprocessed configured manifest URL, Scalprum MUST acquire that manifest on demand. | Without explicit prefetch, initialization makes no manifest request; requesting a module triggers acquisition and loading. |
| REQ-069: Manifest source precedence | WHEN a scope configures both `manifestLocation` and `pluginManifest`, core `getModule()` MUST select the source using `manifestLocation ?? pluginManifest`, matching the existing React component behavior. | Configure both sources with distinguishable manifests; verify the URL source is used and a URL fetch failure does not trigger an embedded-manifest fallback. |
| REQ-054: Container entry selection | WHEN an SDK manifest lists multiple `loadScripts`, the Node adapter MUST register the producer-identified container entry with the MF runtime rather than assume the first script is the entry. | Build a remote with separate runtime/entry chunks; verify adapter uses the producer-identified entry and MF runtime loads its runtime dependencies. |
| REQ-008: Lazy chunks | WHEN a supported Node plugin requests a generated lazy chunk, the Node integration MUST resolve that chunk. | Invoke a real dynamic import after entry initialization. |
| REQ-035: Federation runtime acquisition | The Node adapter MUST delegate remote registration, initialization, expose loading, and lazy-chunk acquisition to the selected Module Federation Enhanced runtime using the Rspack artifact's runtime configuration instead of implementing a parallel asset loader. | Instrument `getInstance`, `registerRemotes`, `loadRemote`, and runtime chunk loading; verify no separate Node fetch/file/chunk loader duplicates those behaviors. Trigger a dynamic import with `baseURL: "auto"`; verify the runtime uses its configured chunk location without adapter inference from the manifest URL or base. |
| REQ-036: Distribution independence | The Node loader MUST NOT require a particular hosting service or artifact distribution package format. | Configure a runtime-supported remote entry location without Fleet/RM/archive-specific distribution conventions. |
| REQ-058: Auto entry paths | WHEN the host's `transformPluginManifest` returns a manifest with `baseURL: "auto"`, the Node adapter MUST use the transformed remote-entry path when registering the remote, without guessing a base or applying a second URL rewrite. | Transform `loadScripts` paths to absolute CRD paths; verify `registerRemotes` uses the resolved entry path without prepending the manifest URL or another base. |

The extension pipeline remains: immutable encoded metadata → separate loaded extensions containing lazy SDK CodeRefs → existing SDK resolution → resolved extension values. No additional manifest field is justified by the PoC's frozen-object mutation bug.

### Options, failures, and packaging

| ID and title | EARS requirement | Verification |
| --- | --- | --- |
| REQ-052: SDK option parity | WHEN an existing SDK loader option applies to the supported Node mechanism and is not a documented v1 exclusion under REQ-053 or REQ-067, the Node integration MUST honor its SDK-defined behavior. | Contract tests cover applicable load policy, transforms, dependency resolution, fetch ownership, and custom-registration controls. Identify callback-only settings as mode-inapplicable and test the specialized raw `sharedScope` exclusion separately. `canReloadScript` coverage does not imply hot replacement. |
| REQ-053: Document exclusions | WHERE a behavior in the SDK 9.1 compatibility baseline is excluded from the supported Node modes, compatibility documentation MUST identify the behavior and reason. | Review the matrix against SDK 9.1; every excluded behavior has a concrete mechanism-specific rationale. |
| REQ-038: Unsupported behavior diagnostic | IF a request in a supported Node mode requires an SDK behavior the Node loader does not implement and has no specialized configuration diagnostic under REQ-067, the loader MUST report an explicit “not supported yet” failure naming that behavior. | Exercise a documented unsupported behavior; observe a named failure, never silent discard. Configuration errors may throw. For `loadPlugin()`, return `PluginLoadResult.success: false` rather than rejecting its promise. |
| REQ-055: Load failure result | IF a required script or container entry cannot be acquired or initialized, `loadPlugin()` MUST resolve to SDK `PluginLoadResult` with `success: false`. | Failure-inject runtime/entry/init; promise does not reject and store records failed-plugin state. |
| REQ-056: Load readiness | WHEN `loadPlugin()` reports success for a remote plugin, the Node loader MUST provide its initialized usable entry module. | Declared exposed module is immediately retrievable; remote registration alone cannot report success. |
| REQ-023: Missing exposed module | IF a requested exposed module is unavailable, Scalprum MUST report module-resolution failure through its existing API error path. | SDK `getExposedModule()` and core `getModule()` reject missing-module lookup. |
| REQ-057: CodeRef failure | IF an encoded CodeRef is malformed or references a missing module/export, its resolution MUST follow the SDK error contract. | Resolver rejects contextually; SDK extension-resolution APIs aggregate errors according to existing behavior rather than treating undefined as successful resolution. |
| REQ-060: Opt-in package | The Scalprum Node integration MUST be available as a separate opt-in package without adding Node-only runtime dependencies to core/browser-only installs. | Isolated consumers install built artifacts and declared dependencies only; inspect both graphs and verify retained core exports including `Container`. |
| REQ-061: SDK runtime identity | WHERE the Node package creates SDK-marked runtime values, it MUST use the SDK runtime instance used by the associated `PluginStore`. | Isolated package fixture proves CodeRef recognition and resolution across package boundaries. |
| REQ-063: Rspack Node remote | WHEN configured for Node output, the initial OCM Fleet Rspack producer plugin MUST emit a standard Module Federation remote exposing `init` and `get` for the Scalprum Node adapter. | Build a remote through the Fleet plugin, load its emitted container through the adapter, initialize sharing, and resolve a declared expose. |
| REQ-064: Node runtime floor | WHEN the bundled CLI host and generated Node plugin run on Node.js 22 or a later supported release, they MUST initialize the remote and resolve exposed modules and lazy chunks. | Run the Rspack-built host/remote fixture on Node 22 and the later Node releases included in the support matrix; verify entry, expose, and lazy chunk. |
| REQ-065: Rspack runtime instance | WHEN the Rspack build plugin creates the host's default Module Federation instance, the Node adapter MUST use that instance for remote registration and loading rather than create a parallel instance. | Verify `getInstance()` returns the host instance and that `registerRemotes` plus `loadRemote` use it. |
| REQ-067: Raw shared-scope exclusion | WHEN the Node loader is initialized with an explicit SDK `PluginLoaderOptions.sharedScope`, it MUST throw a configuration error naming `sharedScope`, stating “not implemented yet”, and directing the consumer to configure sharing in Rspack. | Initialize with an explicit raw scope and assert those diagnostic details; omit it and verify Rspack-configured sharing works. This specializes REQ-038 rather than changing its generic diagnostic. |
| REQ-068: Federation runtime peer | The Node loader package MUST declare `@module-federation/enhanced` as a peer dependency. | Inspect package metadata and an isolated consumer; verify Node loader and Rspack host resolve the same Enhanced runtime instance. |
| REQ-066: Auto init-script diagnostic | IF a required init script fails to load while the manifest declares `baseURL: "auto"`, the Node loader MUST return an explicit failure diagnostic directing the host to verify its `transformPluginManifest` and transformed `loadScripts` paths. | Inject an init-script acquisition failure for an auto-base manifest; assert unsuccessful `PluginLoadResult` includes actionable auto-base/transform guidance. |
| REQ-070: Bootstrap release provenance | WHEN the Node loader is prepared for its initial release, release automation MUST publish version `0.0.1` through a controlled GitHub Actions run using npm trusted publishing and produce verifiable npm provenance with the repository's Nx version/tag conventions. | Run the controlled workflow; verify published version, npm provenance, package release tag, and `last-release` marker. |
| REQ-071: Bootstrap release retirement | WHEN release maintainers retire the bootstrap version after provenance verification, they MUST check package dependents and npm policy first; unpublish only when safe and permitted, otherwise deprecate it with a clear message and retire it from the normal release path. | Verify dependents and policy before action; record unpublish or deprecation and resulting package tags. |
| REQ-072: Producer Nx project boundary | The initial OCM Fleet Node producer plugin MUST be a distinct Nx project in the SDK workspace and MUST NOT be published from OCM Fleet. Keep its project boundary suitable for a future move to Scalprum or the Dynamic Plugins SDK. | Nx discovers the project and its applicable targets; inspect release/publish configuration for no OCM Fleet publication; review dependencies for avoidable Fleet-only coupling. |

SDK option baseline: `canLoadPlugin`, `transformPluginManifest`, `customDependencyResolutions`, `isDependencyResolvable`, `sharedScope`, `fetchImpl`, `getPluginEntryModule`, `entryCallbackSettings`, and `canReloadScript`. The qualification matrix records applicability and exclusion rationale under REQ-052/053/067. `getPluginEntryModule` concerns custom registration; callback-only `entryCallbackSettings` is mode-inapplicable in v1. Rspack build configuration initializes the default MF instance and defines shared modules; explicit raw SDK `sharedScope` gets the specialized REQ-067 diagnostic. `canReloadScript` policy compatibility does not add a hot-replacement feature.

Manifest acquisition/transformation may fail before `loadPlugin()`; preserve that existing failure path. Direct `getModule()` currently indexes `Module[importName]` without missing-export validation: REQ-023 concerns missing exposed modules, while REQ-057 covers CodeRef export validation. React boundaries/hooks retain existing contracts under browser regression tests; no Node-specific UI error contract is added.

## Evidence and implementation constraints

- Inspected Scalprum main `0cf177e10e5e52dc551a55053c0105e5b91adbca`, SDK runtime 9.1.0, and SDK webpack builder 5.3.0 on 2026-10-22. These are observations, not an approved support matrix.
- [David's PoC comparison](https://github.com/scalprum/scaffolding/compare/main...davidaznaur:scaffolding:feat/custom-loader), head `20dd716f0dad1b3125ddf9b08ca370df87b6d03d`, was one commit ahead/36 behind main. Reconcile before reuse. Its useful loader seam needs structural typing; gaps include dropped options/sharing, nested frozen-object mutation, exposed-name translation, first-script-only loading, premature success, undeclared dependencies, and removed `Container` export.
- Core initialization currently eagerly accesses webpack sharing and defaults to `window[name]`. Use environment-neutral global access where needed; substituting `globalThis` alone does not remove browser default-loader/runtime dependencies.
- An earlier memory-only SDK Webpack probe emitted Node container/chunk output, but Webpack is not the selected producer. It does not qualify the OCM Fleet Rspack plugin, emitted format, bundled CLI host, or Node 22+ support. No experimental files or production implementation were created.
- Inspected Module Federation runtime 2.9.0 `RemoteHandler.registerRemote` and `loadRemote`: a `RemoteWithEntry` uses remote `name` plus direct entry URL; no MF manifest file is required. `loadRemote` resolves the remote entry, initializes its container, and retrieves the expose. The Node entry path uses `loadScriptNode`; Node runtime plugin supplies chunk loading. This verifies API shape, not the selected Rspack/Node 22+ end-to-end pairing. Runtime defaults remote type to `global`; qualify any different Rspack output type.

### Core package-boundary evidence

The inspected Scalprum core package exposes `Container`, while the loader interface comes from the SDK. The host composes core initialization with an injected loader. These boundaries support preserving core exports and implementing the Node package directly against the SDK without a runtime dependency on core or a separate types package.

**Security boundary:** Node plugins run with host OS permissions, including filesystem, credentials, and network access. Neither Scalprum, Module Federation, nor `node:vm` provides a hostile-code sandbox. Consumers own trust policy; secure distribution remains outside this design's scope.

## Implementation qualification checklist

There are no open product or architecture questions in the agreed scope. Ticket planning can proceed; the following unrun qualification checks remain implementation work, not evidence of completed support.

- [ ] Qualify the actual Rspack-bundled Fleet host/remote artifact tuple on Node 22 and each later release included in the support matrix. Record emitted module format and remote type, including any difference from the runtime's `global` default. Verify standard `init`/`get`, Rspack-configured default sharing, and use of the host's default Enhanced instance. Register the SDK manifest name and host-transformed, producer-identified entry directly, without `mf-manifest.json`; verify entry, exposes, required runtime dependencies, and lazy chunks through the runtime. Preserve a multi-script fixture and verify `baseURL: "auto"` paths are consumed exactly once, without manifest-URL/base inference or a new asset-source hook.
- [ ] Cover SDK 9.1.0 manifest fields and applicable options in v1 `custom` mode. Record concrete exclusion rationale, including callback-only settings and raw `sharedScope`. Verify immutable metadata, lazy CodeRefs, preserved public lookup names, SDK extension/error contracts, and the specialized configuration diagnostic. Configuration errors may throw; `loadPlugin()` failures return `success: false` without rejecting. Cover URL-first manifest selection and embedded-only acquisition through core.
- [ ] Validate built-package installation, the public interface, SDK runtime identity, and dependency metadata in isolated consumers. Verify retained core exports, Node/browser dependency separation, the Enhanced peer, and shared SDK CodeRef recognition. Check public export spellings, SDK dependency declaration, and dependency ranges as implementation metadata governed by the settled contract; the SDK peer-plus-dev recommendation is not mandatory.
- [ ] Exercise the controlled `0.0.1` bootstrap release; verify npm provenance and Nx/GitHub tags, then check dependents and npm policy before unpublishing or deprecating the version.
- [ ] Verify the OCM Fleet producer Nx project is isolated in the SDK workspace, excluded from OCM Fleet publishing, and has a move-friendly dependency boundary.
- [ ] Run browser regression and the remaining acceptance checks in the requirement tables. No numerical performance target or built-in disk-cache acceptance condition is agreed.

## Consolidation and deferred work

Requirement IDs are stable and not reused. The following mappings replace duplicate text; other active IDs retain their original meanings.

| Previous IDs | Disposition |
| --- | --- |
| REQ-007 | Module/export success merged into REQ-003; cross-target lookup into REQ-049. |
| REQ-034 | Independent target manifests merged into REQ-032. |
| REQ-037 | Transform parity merged into REQ-052. |
| REQ-042 | Replacement-loader acceptance merged into REQ-028. |
| REQ-046 | Unchanged loaded/resolved extension consumption verified by REQ-044–045. |
| REQ-047 | Public exposed-key parity merged into REQ-049. |
| REQ-059 | Runtime-owned auto chunk sourcing merged into REQ-035, including verification against manifest-URL/base inference. REQ-008 retains the lazy-chunk success outcome. |
| REQ-062 | Mandatory SDK peer declaration withdrawn: an earlier draft promoted an assistant recommendation into a requirement. SDK runtime identity remains REQ-061; dependency declaration and ranges are ticket-level packaging metadata governed by that contract. |
| REQ-001–002, REQ-004, REQ-009–022, REQ-024 | Deferred Fleet discovery, Ink bindings, SDK build authoring, transport/registration, and persistent-cache behavior. |
| REQ-029 | React-provider forwarding deferred; current injection scope is core. |
| REQ-039–040, REQ-043 | Superseded by renderer decision C18; presentation belongs to consumer bindings. |

Initial Fleet goals remain deferred, not rejected: shared API/business source, target-specific rendering, resource-manager manifest discovery, and disk asset reuse. The initial Fleet Rspack producer plugin is in scope; a future upstream Scalprum builder integration is not. See [Client Source Sharing](client-source-sharing.md), [Client Asset Separation](client-asset-separation.md), and [CLI instructions](../../client/cli/AGENTS.md); browser distribution design does not define Node distribution.
