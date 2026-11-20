# Console Plugin System

The plugin system lets domain features extend a generic console shell through independently built UI plugins, keeping domain views out of shell infrastructure. Composition uses Scalprum and Module Federation; this reference describes current code and links the established distribution direction.

## Build contract

Plugins declare exposed modules and typed `ocm-fleet.*` extensions. `FleetPlugin` validates declarations against exposed-module metadata before delegating to the Dynamic Plugin SDK's `DynamicRemotePlugin`.

The current toolchain uses Rspack with SWC, not the former webpack/TypeScript-transform setup. Shared React and runtime dependencies are configured through Module Federation; PatternFly barrel imports are rewritten to granular paths through SWC import transforms and module replacement helpers.

The separate private [`@ocm-fleet/node-plugin`](node-plugin.md) SDK package provides an Rspack producer for Node.js Module Federation remotes. It emits the standard `init`/`get` container and a producer manifest; it does not implement remote discovery or a Node/Scalprum loader.

Sources:

- [Core plugin build and extension declarations](../../extensions/core/client/rspack.config.ts)
- [FleetPlugin build wrapper](../../sdk/build-utils/src/extensions/FleetPlugin.ts)
- [Extension types](../../sdk/build-utils/src/extensions/types.ts)
- [Shell build and shared dependencies](../../client/web/rspack.config.ts)

## Bootstrap and loading

The resource manager reads a build-generated `plugin-registry.json` from its configured web directory. It derives Scalprum configuration, page metadata, and default navigation from registry entries.

The browser requests:

1. `/api/ui/config` for public plugin and OIDC bootstrap metadata.
2. `/api/ui/user-config` for default navigation, authenticated when OIDC is configured.

`ScalprumShell` initializes the provider with that configuration. `PluginLoader` loads every configured plugin from its embedded manifest or manifest location before rendering children. This is not currently a per-cluster discovery pipeline.

Sources: [server assembly](../../server/internal/transport/http/uiconfig.go), [browser configuration](../../client/web/src/contexts/AppConfigContext.tsx), [plugin loader](../../client/web/src/components/Root/PluginLoader.tsx).

### Portable shell SDK

The private [`@ocm-fleet/shell`](../../sdk/shell/src/index.ts) SDK provides a separate, UI-framework-neutral bootstrap contract. The existing web app above has not yet migrated to it. `ConsoleProvider` accepts a stable `source` object: `{ type: "static", config }`, `{ type: "url", url }`, or `{ type: "factory", load }`. URL and factory sources may resolve asynchronously. `useConsoleConfig()` exposes `config`, `loading`, and `error` to children even while bootstrap is pending or fails; hosts decide how to present those states. Keep the source object stable across ordinary renders.

Version 1 requires `schemaVersion: 1`, a `catalog.pluginEntries` array containing unique keys and complete plugin manifests, and public `auth` metadata (`{ type: "none" }` or `{ type: "oidc", authority, clientId, scope? }`). Optional `defaultNavLayout` supplies page, group, and section entries. Authentication metadata must not contain secrets or tokens; this provider does not implement login. Unknown versions, missing auth, invalid manifests, duplicate identities, request failures, and plugin load failures are reported as errors rather than silently falling back.

The provider maps the catalog to its internal plugin runtime and loads manifests automatically. For remote manifests, `baseURL` is that plugin's asset directory; relative `loadScripts` paths (including leading slashes) resolve beneath it, while absolute and protocol-relative URLs remain unchanged. The host supplies neither runtime configuration nor a manifest transform. Module Federation's default share scope must already be initialized by the host bootstrap.

The underlying runtime currently uses a shared singleton plugin store. This SDK is intended for one console provider per page and a stable plugin catalog for its lifetime: changing a catalog after bootstrap reports an error requiring a page reload, because prior plugins cannot safely be unloaded or replaced. Source loads are cancelled/ignored on replacement, but live plugin replacement and isolation between providers are not implemented.

## Pages and extension consumers

The backend derives page identities and default navigation from `ocm-fleet.route` declarations, optionally grouped by `ocm-fleet.route-group`. These route contributions select JavaScript exposed modules; they are not themselves Module Federation modules. Plugins own domain views and can declare extension points, such as cluster creation providers and cluster detail tabs. The shell provides routing and shared infrastructure rather than implementing those domain features itself. See [routes and route groups](route-groups.md) for default link visibility and ordering.

Cross-plugin links resolve a plugin scope and module against the page registry. Navigation overrides change presentation without assigning new routes. See [cross-plugin navigation](cross-plugin-navigation.md) and [customization](customization.md).

## Current limits

- The registry comes from packaged build output, not a runtime addon-catalog API.
- The resource manager currently serves shell and plugin assets under `/app`.
- The user-config handler generates navigation; it does not persist a layout per identity.
- `getClusterIdsForPlugin` returns an empty list; cluster-change and general event subscriptions are stubs.
- Page registration, local presentation enablement, and authorization are separate concerns.

Sources: [HTTP configuration handlers](../../server/internal/transport/http/uiconfig.go), [asset mounting](../../server/internal/bootstrap/ui.go), [shell API implementation](../../client/web/src/components/Root/ScalprumShell.tsx).

## Distribution direction

The [addon UI architecture](../design/addon-ui-architecture.md) establishes runtime addon metadata and independently packaged/hosted frontend assets. Fleetlet asset uploads and resource-manager-owned JavaScript distribution are not that target model.

[Client Asset Separation](../design/client-asset-separation.md) is a draft refinement proposing an immutable shell, Gateway, and Operator. Its same-origin gateway arrangement differs from the older design's direct browser fetches from an external host. That reconciliation is unresolved here; these draft components are not current implementation. See [plugin discovery and distribution](plugin-discovery.md) for the distinction.
