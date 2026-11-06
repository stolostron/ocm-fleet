# OCM Fleet CLI Plugin Contract

- **Status:** Draft — current-scope decisions recorded; follow-up topics are out of scope.
- **Tracking:** [FM-118](https://redhat.atlassian.net/browse/FM-118).
- **Scope:** Contract design only. No loader, registry endpoint, or plugin implementation is specified as complete.

## Purpose

Define how `fleetctl` discovers Node plugin groups and contributions without loading executable code, and how a selected contribution maps to a plugin module/export. This contract precedes runtime and resource manager server implementation.

## Scope

### In scope

- Resource-manager-supplied CLI Scalprum configuration, using embedded Node manifests or manifest locations.
- Group and slot discovery, plugin identity, module/export references, and lazy loading.
- A shared CLI error object aligned with the resource manager server error format.
- Build-time authoring types/factories in `@ocm-fleet/build-utils` as a follow-up implementation direction.

### Out of scope

- Implementing the Resource Manager endpoint, Node loader, or `fleetctl` plugin runtime.
- Runtime plugin discovery/reconciliation from addon lifecycle, cluster capabilities, or per-cluster enablement. This is future Resource Manager work.
- Duplicate group detection, conflict resolution, and reporting; separate resource manager work.
- Registry/config or artifact caching, refresh, offline behavior, and cache-related registry failure policy; follow-up work.
- Executable trust, artifact approval, integrity, and revocation; part of the catalog conversation.
- CLI/Node compatibility policy and version ranges; SDK build handling and any further compatibility work are follow-ups.
- Exact command names, flags, argument schemas, business logic, handler interfaces, group-specific mutations, and global flag inventory; separate feature work.
- Public/private group extensibility policy; any future policy concerns external contributions, not user visibility or authorization.
- Slot-usage validation implementation details, including recognized API forms, ownership resolution, dynamic names, and cross-plugin handling; separate work.
- Mapping individual local or resource-manager failures to specific shared error codes and details; separate implementation work.
- Deployment-detail extension contracts and CLI/TUI visual presentation; separate work after the generic contract.
- Defining a CLI-specific artifact distribution service or code sandbox.

## Actors and terminology

| Term | Meaning |
|---|---|
| Scope | Scalprum plugin scope, such as `kind-plugin`; identifies the plugin for module lookup and is not an authorization grant. |
| Import name | Public Module Federation exposed-module key, such as `Commands`; it is the module portion of a CodeRef. |
| CodeRef | Encoded reference `{ "$codeRef": "ModuleName.exportName" }`; identifies a named export within the declaring plugin's exposed module. |
| Command group | A logical container, not a command, declared as an extension of type `ocm-fleet-cli.command-group`. Its `id` is the CLI group name; `label` is its display label. It can declare named extension slots. |
| Command slot | A named extension point declared by a group owner. Its contribution type can be group-specific, for example `ocm-fleet.deployment`; other plugins can contribute extensions to it. |
| Group contribution | An extension accepted by a command group's slot to extend or modify that group. Contributions can be owned by a different plugin; supported mutations are defined separately. |
| Command extension | A group contribution that declares a runnable command, with discovery metadata and a handler reference. Its contribution type is matched against each group's declared slot type. |
| Registry snapshot | Explicit/generated CLI plugin metadata served by the resource manager server. Initial version is not dynamically reconciled. |

## Manifest and command mapping

The resource manager server supplies CLI Scalprum configuration directly from a CLI-specific endpoint. Its response uses the configuration shape accepted by Scalprum, so the CLI can pass it to Scalprum without converting a Web registry envelope. The resource manager decides whether each configuration supplies an embedded Node manifest or a manifest location. Browser bundles are not assumed executable in Node.

The current Web server separately generates `scalprumConfig` from its `{ assetsHost, plugins }` registry and the Web shell passes that configuration to Scalprum. The current Web Scalprum configuration contains `name`, `manifestLocation`, and `pluginManifest`; it does not include the registry entry's short `key`. Like Web module lookup, CLI loading uses scope, module, and export. A separate Fleet `pluginKey` is not required for this contract.

Illustrative direct CLI Scalprum configuration with an embedded manifest; asset paths are examples, not a distribution or trust decision:

```json
{
  "kind-plugin": {
    "name": "kind-plugin",
    "pluginManifest": {
      "name": "kind-plugin",
      "version": "1.0.0",
      "registrationMethod": "custom",
      "baseURL": "auto",
      "extensions": [],
      "loadScripts": ["remoteEntry.cjs"],
      "remoteEntry": "remoteEntry.cjs",
      "runtimeDependencies": []
    }
  }
}
```

The Node producer already emits the manifest fields shown above. Its generic `extensions` array contains group and contribution metadata; an empty array in this example is not a discoverable Kind command. This example illustrates an embedded manifest; the resource manager can instead configure `manifestLocation`. Location-only discovery requires obtaining manifest metadata, but not loading executable plugin code.

Illustrative, non-final manifest fragment. The slot contribution type and property names are examples; matching is by extension type. The two entries being in one manifest does not imply they must share an owner.

```json
{
  "name": "kind-plugin",
  "version": "1.0.0",
  "registrationMethod": "custom",
  "baseURL": "auto",
  "extensions": [
    {
      "type": "ocm-fleet-cli.command-group",
      "properties": {
        "id": "kind",
        "label": "Kind",
        "extensionPoints": {
          "commands": {
            "type": "ocm-fleet.kind",
            "description": "Kind CLI commands"
          }
        }
      }
    },
    {
      "type": "ocm-fleet.kind",
      "properties": {
        "id": "create",
        "label": "Create a local cluster",
        "description": "Illustrative Kind command",
        "handler": { "$codeRef": "Commands.create" }
      }
    }
  ],
  "loadScripts": ["remoteEntry.cjs"],
  "remoteEntry": "remoteEntry.cjs",
  "runtimeDependencies": []
}
```

The CLI derives command identity and loading information from group, slot, registry, and command metadata:

| Effective field | Source | Example |
|---|---|---|
| `scope` | Scalprum configuration's plugin scope | `kind-plugin` |
| command identity | Command contribution metadata, interpreted in each matching group | `create` |
| command group | Group extension/slot selected by matching its declared contribution type | Kind |
| `importName` | First component of `handler.$codeRef` | `Commands` |
| export name | Remaining component of `handler.$codeRef` | `create` |

The CodeRef resolves within the declaring plugin's scope; it cannot name an export from another plugin. Each matching group provides a distinct command path using the group's `id` and the contributed command name. The contribution does not need to declare a single parent group. The same discovery and module-loading approach applies to built-in and plugin groups; individual group behavior is outside this design. This contract adds no CLI-specific mismatch policy for configuration and manifest names.

### Group extensions

The hierarchy is explicit: a group is a logical container, not an executable command. Its extension has an `id` used as its CLI group name, a `label`, an optional `description`, and optional named `extensionPoints`. Each declared slot has a `type` and `description`. The slot's `type` identifies the extensions the group can consume; for example, a deployment group could declare `ocm-fleet.deployment`, and other plugins could contribute extensions of that type. A contribution matches every group slot declaring its type, including slots belonging to different groups. `ocm-fleet.deployment` and `ocm-fleet.kind` are illustrative slot types, not group types. The group and its command contributions are distinct extensions and can be owned by the same plugin or different plugins. A group with no discovered commands is not displayed.

The group owner's build configuration records its extension slot, and its build-generated manifest carries that declaration. Build-time validation of undeclared slot usage is being addressed separately; its implementation details are outside this design. UI today declares slot type and description; it does not establish CLI attachment, static scanning, or handler compatibility validation.

An earlier proposal to infer groups solely from command path prefixes is superseded by the review decision to use explicit group extensions and slots.

The registry merges built-in and plugin leaves into one tree:

```text
fleetctl
├── auth
│   ├── login
│   ├── setup
│   └── whoami
├── deployment
│   ├── get
│   ├── list
│   ├── delete
│   └── summarize   # plugin contribution is allowed initially
└── kind            # plugin-defined group
    └── create
```

Root `--help` and `fleetctl commands` render this hierarchy. Only a command contribution attached to a declared group resolves to a runnable command.

Command-specific flag schemas and the global flag inventory are out of scope. The previously discussed kebab-case long names and optional one-letter aliases are context for that later work, not plugin discovery requirements.

## Discovery and loading

### Initial Resource Manager behavior

FM-119 defines a separate authenticated CLI registry endpoint, `GET /api/cli/plugin-registry`, independent of Web UI registry/config routes. Its response is CLI Scalprum configuration using embedded SDK-shaped Node manifests or manifest locations, not executable plugin asset bytes. Initial source is an explicit/generated registry snapshot. Duplicate group handling and dynamic addon/cluster reconciliation belong to separate resource manager work.

### Client loading sequence

1. `fleetctl` obtains the CLI registry snapshot using the configured resource manager connection/authentication. Timing, caching, and failure policy are follow-up work.
2. It obtains manifest metadata from the resource-manager-selected embedded manifest or location, indexes group/slot declarations and command metadata, and associates contributions with matching group slots by extension type. This phase does not import remote modules.
3. Help, command listing, and completion read metadata only.
4. When a user invokes a plugin command, the CLI uses the command contributor's Scalprum scope to load its remote and resolve the CodeRef. The group itself is not invoked as a command.
5. Group-specific command behavior and presentation are defined separately from plugin discovery/loading.

For the Node loader handoff, [design PR #8](https://github.com/stolostron/ocm-fleet/pull/8) assigns runtime asset-path transformation to the host and remote entry/chunk loading to the Module Federation runtime through Scalprum's Node loader. The producer identifies `remoteEntry` in `loadScripts`; the loader does not assume the first script is the entry. The resource manager selects the manifest source in the CLI Scalprum configuration. The generic design specifies URL-first precedence for Scalprum `getModule()` if both `manifestLocation` and `pluginManifest` are provided.

The earlier process-local-only caching decision is superseded: cache scope, invalidation, offline behavior, and related failure behavior are outside this contract.

## Requirements

Each requirement has one observable behavior and a verification scenario. Uppercase normative keywords follow BCP 14 (RFC 2119 and RFC 8174). This specification prefers `MUST` / `MUST NOT` for clarity.

### Extension metadata and command discovery

| ID / title | EARS requirement | Verification |
|---|---|---|
| REQ-001 — Metadata-only discovery | The CLI MUST discover command contributions from manifest metadata without loading executable plugin code. | Provide fixtures for two groups and verify each command is discovered while a loader spy records no remote loads. |
| REQ-002 — Stable command identity | WHEN the CLI indexes a command extension, it MUST expose its stable command identity from metadata without loading the remote module. | Index a fixture manifest and assert the command identity is available while a loader spy records no remote loads. |
| REQ-003 — Plugin scope mapping | WHEN the CLI indexes a command extension, it MUST associate the extension with its declaring plugin's Scalprum scope. | Discover a command from the `kind-plugin` configuration entry; assert the normalized command scope is `kind-plugin`. |
| REQ-004 — Canonical CodeRef | WHEN command metadata identifies a handler, it MUST use a CodeRef to a module and export in the declaring plugin. | Provide two plugin fixtures exposing `Commands.create`; verify each contribution's CodeRef resolves in its own plugin scope. |
| REQ-005 — Import-name derivation | WHEN the CLI prepares a handler load, it MUST derive the Module Federation import name from the module component of the handler CodeRef. | For `Commands.create`, assert loader request uses exposed key `Commands` and export lookup uses `create`. |
| REQ-006 — Pre-load help metadata | The CLI MUST make declared group and command discovery metadata available to help without loading executable plugin code. | Discover an unloaded group and command; verify their discovery metadata is available to help while loader spy records no remote load. Exact command metadata is outside this design. |
| REQ-007 — Pre-load completion metadata | The CLI MUST make declared group and command discovery metadata available to completion without loading executable plugin code. | Discover an unloaded group and command; verify their discovery metadata is available to completion while loader spy records no remote load. |
| REQ-010 — Lazy module loading | WHEN a user invokes a plugin command, the CLI MUST NOT load unrelated plugin remotes solely because that command was invoked. | Index multiple plugin fixtures, invoke one, and assert an unrelated plugin remote is not loaded. |
| REQ-011 — Help/listing without execution | WHEN the user requests help, command listing, or completion, the CLI MUST use manifest metadata without executing command handlers. | Run each metadata-only path with handler/export execution spies and assert zero executions. |
| REQ-027 — Explicit group attachment | WHEN the CLI indexes a command extension, it MUST associate that command with matching declared group extension points. | Load group declarations and command contributions for `auth`, `deployment`, and `kind`; assert each leaf is associated with matching group slots and groups are not runnable leaves. |
| REQ-028 — Shared discovery contract | WHEN the CLI discovers built-in or plugin commands, it MUST apply the same discovery metadata contract. | Mix built-in and plugin group fixtures and verify the same discovery fields identify each command without loading remote code. Group-specific handler interfaces remain separate. |
| REQ-029 — Group rendering | WHEN multiple commands attach to the same group extension point, the CLI MUST render that group once with its child commands. | Attach `get` and `summarize` to one deployment group; assert one group with two leaves and no group handler load. |
| REQ-030 — Extendable groups | WHEN a command contribution is associated with a built-in or plugin-defined group, the CLI MUST include it in that group's command tree. | Associate plugin command `summarize` with `deployment` and `inspect` with `kind` by matching slot types; assert both appear in the respective groups. |
| REQ-032 — Hierarchical listing | WHEN the user requests root help or `fleetctl commands`, the CLI MUST render built-in and plugin commands as the same hierarchical tree. | Assert both views show the same groups, leaves, and labels independent of remote module loading. |
| REQ-036 — Declared group slots | WHEN a group-owning plugin build generates its manifest, it MUST include each group extension slot declared in that plugin's build configuration. | Build fixture with a declared `commands` slot; assert generated manifest records its declaration. |
| REQ-037 — Validate extension-slot use | IF plugin code uses an extension slot absent from that plugin's build configuration, the build-time validator MUST fail with a diagnostic naming the undeclared slot. | Build fixtures using a declared slot and an undeclared slot; assert the undeclared use fails and names the slot. The validator's supported usage forms are defined by separate work. |
| REQ-039 — Command-group extension type | The CLI plugin contract MUST identify command-group declarations using extension type `ocm-fleet-cli.command-group`. | Given declarations for `deployment` and `kind`, verify both use that group type while their slot contribution types can differ. |
| REQ-045 — Match all slots by type | WHEN a group contribution's extension type matches multiple declared group slots, the CLI MUST associate that contribution with every matching slot. | Declare matching slots on two different groups; assert one contribution of that type is associated with both groups without loading its executable code. |
| REQ-046 — Scalprum Node loading | WHEN the CLI loads a Node plugin, it MUST use the Scalprum Node loader through the Scalprum loader integration. | Invoke a plugin contribution with an instrumented `@scalprum/node-loader` integration; verify it receives the plugin load request. |
| REQ-047 — No duplicate import name | WHEN command metadata declares a handler CodeRef, it MUST NOT declare a separate authored import name for that handler. | Validate a command with `Commands.create` and no import-name field; reject a fixture that separately declares one for the same handler. |
| REQ-048 — Group identifier | The `id` of a command-group extension MUST identify its CLI group name. | Declare a group with `id: kind`; verify its discovery name is `kind`. |
| REQ-049 — Group label | The command-group extension MUST declare a `label`. | Validate a group fixture with a label and reject one that omits it. |
| REQ-050 — Group description | A command-group extension MAY declare a `description`. | Validate fixtures with and without a description. |
| REQ-051 — Optional extension points | A command-group extension MAY declare named `extensionPoints`. | Validate group fixtures with and without an `extensionPoints` map. |
| REQ-052 — Slot type | WHEN a command group declares an extension point, that extension point MUST declare the contribution `type` it accepts. | Validate a slot with a type and reject a slot that omits it. |
| REQ-053 — Slot description | WHEN a command group declares an extension point, that extension point MUST declare a `description`. | Validate a slot with a description and reject a slot that omits it. |
| REQ-057 — Hide empty groups | WHILE a command group has no discovered commands, the CLI MUST NOT display that group. | Provide a declared group with no commands; verify it is absent from help and command listings. Add a matching command and verify the group appears. |
| REQ-062 — Group-specific command path | WHEN a command contribution matches a group slot, the CLI MUST expose a command path using that group's `id` and the contributed command name. | Match one contribution to two groups; verify each resulting path uses its matching group `id` without loading executable plugin code. |

### Resource Manager registry and lifecycle

| ID / title | EARS requirement | Verification |
|---|---|---|
| REQ-012 — Separate CLI endpoint | The resource manager server MUST serve CLI Scalprum configuration independently from the Web UI registry and Web Scalprum configuration. | Contract/API test retrieves CLI configuration and confirms existing Web routes and formats are unchanged. |
| REQ-013 — Authenticated registry access | WHEN a CLI client requests the CLI registry, the Resource Manager MUST apply the existing CLI authentication/access policy. | Authenticated request receives registry; unauthenticated/unauthorized request is rejected according to that policy. |
| REQ-014 — Metadata-only endpoint | The CLI registry endpoint MUST NOT return executable plugin asset bytes. | Inspect response and assert no remote entry or chunk contents are embedded. |
| REQ-015 — Explicit snapshot source | The initial CLI registry MUST represent an explicit/generated registry snapshot rather than runtime cluster-specific plugin discovery. | With no runtime addon/cluster discovery configured, the endpoint serves the configured/generated snapshot; no cluster queries occur. |
| REQ-058 — Embedded manifest discovery | WHEN the CLI Scalprum configuration supplies an embedded Node manifest as its selected source, the CLI MUST use that manifest to discover its group and contribution metadata. | Configure an embedded manifest without a manifest location; discover its group and contributions without fetching another manifest or loading executable code. |
| REQ-059 — CLI Scalprum configuration | The resource manager server MUST return CLI Scalprum configuration in the shape accepted by Scalprum. | Retrieve the CLI endpoint response and pass it directly to the Scalprum provider/integration without converting a Web registry envelope; verify initialization for embedded and location-only manifests. |
| REQ-060 — Location-only manifest discovery | WHEN the CLI Scalprum configuration supplies only a manifest location for a plugin, the CLI MUST obtain its manifest metadata from that location for discovery. | Configure a location-only manifest and verify discovery fetches metadata without loading executable plugin code. |
| REQ-063 — Consistent manifest selection | WHEN CLI Scalprum configuration supplies both a manifest location and an embedded manifest for a plugin, the CLI MUST use the manifest location for discovery. | Supply distinguishable location and embedded manifests; verify discovery uses the location, matching the generic Scalprum `getModule()` source precedence. |

### Shared CLI errors

The resource manager's gRPC-Gateway API uses the `google.rpc.Status` JSON envelope with `code`, `message`, and `details`; see [Client Source Sharing](../design/client-source-sharing.md#validation-and-errors). Some server errors lack structured details, and Web bootstrap routes can use plain HTTP errors. Mapping individual failures to shared error codes and details is separate implementation work, not a decision required by this specification.

| ID / title | EARS requirement | Verification |
|---|---|---|
| REQ-042 — Shared CLI error object | The CLI and TUI MUST use the same error object contract across the CLI. | Produce representative plugin-discovery and other CLI failures and verify both CLI and TUI consumers use the same error object contract. |
| REQ-043 — Resource manager error format | WHEN the CLI represents a resource manager `google.rpc.Status` error in its shared error object, it MUST preserve the status `code`, `message`, and provided `details`. | Inject a status with code, message, and details and verify those values remain available to both CLI and TUI error consumers. |
| REQ-044 — Local error format | WHEN the CLI represents a local plugin-discovery or loading failure, its shared error object MUST follow the `google.rpc.Status` error format. | Inject a local loader failure and verify the resulting object uses the status `code`, `message`, and applicable `details` representation; individual failure mappings are separate work. |

CLI and TUI can share Ink elements for human-readable output while using different layouts. Rendering details and any effect on the plugin contract will be explored separately.

## Acceptance scenarios

1. **Metadata-only discovery:** Start `fleetctl` with built-in commands and plugin leaves under both a built-in group and a plugin-owned group. Root help, `fleetctl commands`, and completion expose a hierarchical tree without loading remote code.
2. **Cross-plugin lazy invocation:** A group owner declares a slot and a different plugin contributes a command. CLI associates them from metadata, resolves the contributor's scope and CodeRef, and invokes the command without treating the group as a command.
3. **CLI configuration:** Resource manager supplies Scalprum configuration with an embedded manifest or a location. CLI discovers group and command metadata from the selected source without loading executable code.
4. **Shared error object:** A resource manager `google.rpc.Status` failure retains its code, message, and provided details in the shared CLI/TUI error object.
5. **Declared slot validation:** A group owner's declared slot appears in its manifest. A supported use of an undeclared slot fails the plugin build with the slot identified.
6. **Empty group:** A declared group with no discovered commands does not appear in CLI help or command listings. It appears when a command contribution is discovered.

## Assumptions and constraints

- Existing `client/cli` command runner/result and host formatter separation is the baseline; no production plugin loading currently exists.
- Web registry `pluginKey` and Scalprum `scope` are distinct; this discovery/loading contract uses the scope and does not require the Web registry's short key.
- Node producer manifest fields and Node remote entry identification are owned by `@ocm-fleet/node-plugin`; this specification adds Fleet CLI extension meaning only.
- The Resource Manager registry snapshot is metadata/reference data. The registry does not itself establish executable-code trust.
- Node plugins execute with the user's local process permissions. A trust/approval policy is required before broad third-party plugin use; this spec does not claim sandboxing.
- The SDK handles compatibility builds. Exact CLI/Node compatibility policy and runtime qualification are follow-up work, not established by this contract.
- The current Web registry projects `scope` from entry `name` and `pluginKey` from entry `key`; the Scalprum configuration uses plugin scopes for module lookup.

## Open decisions

None in the current discovery/loading scope. Deferred topics are listed under out of scope.

### Requirement history

`REQ-008`, `REQ-009`, `REQ-017`–`REQ-026`, `REQ-033`–`REQ-035`, and `REQ-038` are retired: they prescribed feature-specific behavior, caching, rendering, or deployment-detail contracts now deferred. Earlier process-local caching and deployment-detail decisions remain context for follow-up discussions, not requirements of this discovery/loading specification. `REQ-016` is superseded by the snapshot requirement and out-of-scope reconciliation boundary. `REQ-031` and `REQ-054`–`REQ-056` are retired because duplicate handling belongs to separate resource manager work. `REQ-040` is superseded by direct Scalprum configuration; `REQ-041` is superseded by resource-manager-selected embedded or location-only manifests. `REQ-061` is withdrawn: Scalprum scope is sufficient for plugin identity in this contract. Retired IDs are not reused.

## Traceability

- **FM-118:** Original issue includes handler behavior, compatibility, and a synthetic Kind deployment-detail extension. This conversation narrows the specification to generic discovery/loading and defers those feature contracts; the Jira issue may need a follow-up scope update.
- **FM-119:** Separate authenticated `GET /api/cli/plugin-registry`; metadata and entry references without executable bytes; explicit/generated snapshot source; cluster-specific discovery/activation out of initial scope. This conversation adds resource-manager-supplied CLI Scalprum configuration with embedded or location-only manifests.
- **FM-121:** Build-utils factories/types implement only after this contract is approved.
- **Existing CLI source:** `client/cli/src/commands/types.ts` separates command runner data from optional presenter; `client/cli/src/app.tsx` owns JSON/YAML/component formatting; `client/cli/src/commands/registry.ts` holds built-in command registration; `client/cli/src/argv.ts` currently consumes the first two positional tokens as command path and defines current global flags/aliases.
- **Existing plugin contract:** `sdk/node-plugin/README.md` and `sdk/node-plugin/src/manifest.ts` define producer Node manifest fields and remote-entry behavior; `sdk/build-utils/src/extensions/validate.ts` defines current CodeRef syntax and same-plugin exposed-module validation.
- **Existing UI mapping:** `server/internal/transport/http/uiconfig.go` projects `pluginKey` separately from manifest-name `scope` and exposed `module`; `docs/ui/feature-contracts.md` documents CodeRef and exposed-module semantics.
- **Related design:** [PR #8](https://github.com/stolostron/ocm-fleet/pull/8) contains the generic Node loader design referenced by FM-118. It is not present in this worktree yet.
