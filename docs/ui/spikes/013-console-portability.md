# SPIKE: Portable Console — Sharing Shell Core with OCM

- **Jira**: FM-TBD (pending approval to create)
- **Epic**: SPIKE - User Interface (Web)
- **Status**: Proposal. No extracted shared provider or second-host production integration exists. POC evidence is local and outside this repository.
- **Related**: [Feature contracts](../feature-contracts.md), [Plugin system](../plugin-system.md), [Console authentication](../authentication.md), [Addon UI architecture](../../design/addon-ui-architecture.md), [Client Asset Separation draft](../../design/client-asset-separation.md)

## Question and scope

Can OCM and OCM Fleet share a browser-shell core and Fleet-built plugin runtime while each host retains its own bootstrap producer, authentication integration, domain APIs, and UI? This is a shell/runtime portability proposal, not a Kubernetes-independent REST API or reusable OCM authentication service. The POC used a hub API for sample data; that alone does not make APIs portable.

OCM Fleet owns and versions the shared core; OCM.io is a consumer. Coordinate shared shell contracts—bootstrap config, route identity, navigation and search placement, and the shell-facing auth interface. Plugin/domain contracts remain with their consumers; do not imply a blanket cross-project compatibility promise.

## Current implementation and proposed boundary

Fleet currently fetches `/api/ui/config` and `/api/ui/user-config`. The resource manager reads build-generated `plugin-registry.json`, returns `scalprumConfig`, `pluginPages`, `pluginEntries`, and `assetsHost`, and generates route identities plus a default `navLayout`. The shell provides Scalprum and an injected `api` object; `PluginLoader` loads configured plugins. See [AppConfigContext](../../../client/web/src/contexts/AppConfigContext.tsx#L68-L119), [uiconfig.go](../../../server/internal/transport/http/uiconfig.go#L157-L180), [ScalprumShell](../../../client/web/src/components/Root/ScalprumShell.tsx#L18-L107), and [PluginLoader](../../../client/web/src/components/Root/PluginLoader.tsx#L4-L28).

The proposed `ConsoleProvider` owns Scalprum, provider API injection, config loading/validation, manifest transformation, extension loading, and auth-package composition. Hosts receive normalized config (`useConsoleConfig()`), auth state/requests (`useConsoleAuth()`), and reusable navigation/routing components or templates—not Scalprum context or a fillable API bag. Full manifests in `pluginEntries` provide user-independent route/page identity and canonical paths; navigation layout is separately customizable. `defaultNavLayout` is optional; saved preferences load separately; generate an initial layout only if neither exists.

This consolidates work that otherwise leaks into every host: converting bootstrap data into Scalprum config, resolving manifest assets, choosing auth composition, and coordinating when extensions load. Hosts still own branding, top-level layout, and domain-specific APIs. The proposal deliberately avoids hiding those differences behind a shared REST client or generic plugin API.

Illustrative TypeScript only; these proposed types/APIs do not exist. `NavLayoutEntry` and `NavLayoutOverride` are current `@ocm-fleet/common` types; auth/provider interfaces are conceptual:

```ts
import type { NavLayoutEntry, NavLayoutOverride } from "@ocm-fleet/common";

interface ConsoleCatalog {
  pluginEntries: Array<{
    key: string;
    pluginManifest: unknown; // Full manifest; validate against the selected runtime type.
    assetBase?: string;
  }>;
}

type PublicAuthConfig =
  | { mode: "anonymous" }
  | { mode: "oidc"; authority: string; clientId: string; scope?: string };

interface ConsoleConfig {
  schemaVersion: "1.0";
  catalog: ConsoleCatalog;
  defaultNavLayout?: NavLayoutEntry[];
  auth: PublicAuthConfig;
}

type ConsoleConfigSource =
  | { kind: "url"; url: string }
  | { kind: "static"; config: ConsoleConfig }
  | { kind: "factory"; load: () => Promise<ConsoleConfig> };

interface NavigationPreferencesAdapter {
  load(): Promise<NavLayoutOverride | null>;
  save(override: NavLayoutOverride): Promise<void>;
  clear(): Promise<void>;
}
```

URL, static, and factory sources adapt host-owned public configuration to one shape. Config contains no credentials. Anonymous mode is explicit; missing or failed auth config is an error, not anonymous fallback. OIDC is one host option, not a Fleet/OCM-wide auth mandate. Auth code is selected at build time, never by remote catalog data. `unknown` avoids inventing an SDK manifest export; extraction must validate the actual manifest type. Fleet's current `oidc` field needs producer/SDK mapping to this proposed `auth` shape; adding `schemaVersion` alone is insufficient. The adapter does not decide remote/local merge or sync policy.

Authenticated host composition, also illustrative. `OidcAuth` is a trusted build-time dependency, not a remotely loaded plugin:

```tsx
import { ConsoleProvider } from "@ocm-fleet/shell-core";
import { OidcAuth } from "@ocm-fleet/auth-oidc";

<ConsoleProvider
  source={{ kind: "url", url: "/api/ui/config" }}
  authPackage={OidcAuth}
  navigationPreferences={preferencesAdapter}
>
  <App />
</ConsoleProvider>
```

A static host can supply `auth: { mode: "anonymous" }` and omit `authPackage`; `App` owns branding and domain composition. These package names and signatures illustrate the boundary, not available APIs.

Reconcile a stored layout override against the current supplied default or manifest-generated default: preserve user organization/hidden choices and host navigation choices, add new contributions, and remove obsolete ones. Do not make every route—especially detail routes—a navigation item. This catalog reconciliation is not remote preference sync policy.

Extraction must define canonical route identity/path generation, including parameterized detail routes; Fleet currently has a built-in server-side special case. Reordering, grouping, or hiding navigation never changes route registration or API authorization.

## POC evidence and limits

A local POC ran an OCM v1.4.0 hub and registered managed cluster in Kind. A Go add-on served an embedded minimal shell/plugin, static config, and read-only `/api/managedclusters` backed by the hub API. The shell was reimplemented minimally with React 19, `@scalprum/react-core`, and `@openshift/dynamic-plugin-sdk`; it was not an extracted `ConsoleProvider`. A plugin built with `@ocm-fleet/build-utils` and local npm `file:` dependencies rendered using the Fleet extension contract/runtime.

This supports reuse of Fleet's `FleetPlugin`/`createRoute` build contract and remote runtime in a separately written host. It does not prove shared-provider extraction, production asset delivery, auth reuse, API portability, or plugin sandboxing. The POC's config/assets were unauthenticated; it does not establish production access control, trusted scopes, signing, or integrity.

### Findings to preserve

- **Async entry**: the POC encountered `RUNTIME-006` without an async shell entry. Fleet starts at `client/web/src/index.ts` and dynamically imports `bootstrap`; preserve and verify this boundary during extraction.
- **Asset URL resolution**: the POC exposed a literal `baseURL: "auto"` prefix. Current server code rewrites origin-rooted registry paths to `/app`; current `transformPluginManifest` handles only `routing-plugin`, using `assetsHost` or `APP_BASENAME` for relative scripts. The provider hides a generalized transform in internal `pluginSDKOptions`; producers need not rewrite manifests. It needs per-plugin asset-base metadata, preserves absolute URLs, and must not prefix every script with the console root. See [uiconfig.go](../../../server/internal/transport/http/uiconfig.go#L241-L273) and [ScalprumShell](../../../client/web/src/components/Root/ScalprumShell.tsx#L79-L105).
- **SPA hosting**: hosts must route known client-side paths to the entry document while preserving genuine asset, API, and config 404s; this belongs to the host, not the SDK.
- **Packaging**: the POC observed broad `@ocm-fleet/common` imports pulling PatternFly icon dynamic-module data into an external build. Measure package weight and Module Federation sharing with the intended published dependency graph; current workspace `0.0.0` versions and local `file:` references do not establish a release policy. See [`sdk/common/package.json`](../../../sdk/common/package.json#L1-L19) and [`sdk/build-utils/package.json`](../../../sdk/build-utils/package.json#L1-L12).
- **Extension consumption**: use `useDynamicExtensions` as intended successor to `useExtensions`/`useResolvedExtensions` when available. It does not replace catalog/manifest discovery or resolved CodeRef/export handling.

## Proposed ownership and rationale

| Surface | Owner | Boundary |
| --- | --- | --- |
| Public config/provider | Shared shell core | URL/static/factory input; validate config; full manifests in `pluginEntries`; derive route/page identity without duplicate `scalprumConfig` or `pluginPages`. |
| Routes and navigation | Shared core plus host preference source | Canonical route identity is independent of user layout. Layout edits cannot remove routes or authorize data. Stored preference, then optional default, then generated initial layout. |
| Search | Shared shell placement; host/plugin data | Standardize mount point; keep queries, inventory schemas, and renderers domain-owned. |
| Auth | Shared interface; build-time selected host implementation | Explicit anonymous mode or configured trusted provider; host owns issuer, sessions, token transport, and API authorization. |
| Plugin/domain contracts | Consuming owners | Evolve per consumer; no universal versioning guarantee. |
| Shared state and UI | Shared hook modules and components/templates | Replace injected `api` bag with remote hooks; expose reusable navigation/routing UI without host access to provider internals. |
| Domain APIs/data | Each host/plugin | Fleet APIs and OCM hub APIs remain distinct; POC defines no common REST layer. |
| Asset distribution | Fleet design remains unresolved | Do not conflate Fleet's competing delivery proposals with OCM's embedded add-on assets. No sandbox or signing guarantee is established. |

These boundaries keep shared contracts to shell-owned surfaces, separate canonical routes from editable navigation, and keep auth implementation out of runtime discovery. Compile-time auth selection narrows configuration/supply-chain ambiguity; it does not sandbox plugin JavaScript or establish asset integrity. Backend authorization remains authoritative regardless of hidden navigation entries.

Sharing the core trades independent control of shell contracts for reuse and standardization, without coupling every plugin/domain release cycle.

Today `ScalprumShell` injects `api.fleet`; `PluginLink` and `usePluginNavigate` still read it through `useScalprum`. Extraction must remove that dependency rather than describe current helpers as already independent. See [ScalprumShell](../../../client/web/src/components/Root/ScalprumShell.tsx#L18-L73), [PluginLink](../../../sdk/common/src/PluginLink.tsx#L15-L39), [usePluginNavigate](../../../sdk/common/src/usePluginNavigate.tsx#L12-L44), and [FleetApi](../../../sdk/common/src/scalprum.ts#L10-L19).

Fleet currently returns a generated default layout and browser-local IndexedDB overrides; resource-manager-authoritative preferences are planned, not implemented. The shared proposal keeps public defaults separate from saved user state and does not define sync/conflict handling. See [uiconfig.go](../../../server/internal/transport/http/uiconfig.go#L207-L238), [useNavLayout](../../../sdk/common/src/useNavLayout.ts#L7-L31), and [current IndexedDB store](../../../sdk/common/src/extensionInstall.ts#L32-L69).

The existing Fleet `AuthProvider` also has a `requireAuth={false}` path that can render children after OIDC config failure. That behavior is not the proposed provider contract: auth-enabled hosts must show configuration/login failure rather than silently becoming anonymous. Issuer and client settings are public bootstrap data; credentials and backend authorization decisions are not. See [AuthContext](../../../client/web/src/contexts/AuthContext.tsx#L115-L161).

## OCM console add-on

OCM hub does not supply its own authentication. For deployed use, this console proposal requires an existing hub, a browser-reachable OIDC issuer, and an HTTPS Ingress or OpenShift Route. Local anonymous runs may use port-forward. The add-on installs its own configuration CRD; this is not an upstream OCM requirement. OIDC is an add-on deployment requirement, not an OCM platform requirement; OpenShift OAuth is not automatically a standard OIDC issuer.

OCM's console is one minimal application: one Go add-on hosts one UI plugin containing the whole console. It provides public bootstrap config, embedded asset serving (the CDN role), and a same-origin data API. It shows and acts on OCM resources as-is; Fleet remains the upgrade path for the resource manager, planned authoritative preferences, and broader plugin model.

### Config and single-app lifecycle

An administrator or deployment process manages a CR from day one. The add-on combines that configuration with its embedded plugin manifest to produce public `ConsoleConfig`: one catalog entry, the full manifest, public auth settings, and an optional default layout. Config edits need no rebuild; new console capabilities ship as an add-on/plugin release. There is no third-party registration protocol, and config cannot introduce external script origins.

### Authentication and API authorization

The browser uses OIDC authorization-code flow with PKCE. Discovery and token endpoints fetched cross-origin by the browser must allow CORS requests from the console origin. The add-on validates issuer, audience, signature, and expiry, then makes each Kubernetes API request as the authenticated user through Kubernetes impersonation using its service account. Kubernetes RBAC remains the authorization authority and is evaluated per request; data and authz decisions are not cached. The browser never receives the add-on service-account credentials. Audit retains the original caller and records the effective impersonated identity.

This facade keeps browser requests same-origin and lets Kubernetes enforce subject-based permissions for console requests. The same Kubernetes RoleBindings apply only when both clients use the same stable subject/group mapping. The add-on does not substitute its own blanket read authorization for user RBAC. Token group claims represent the identity at token issuance; per-request RBAC evaluation does not refresh stale claims in an unrenewed token.

Explicit local-only anonymous mode impersonates a fixed, non-system development identity authorized by local RBAC—not caller-supplied identities or implicit privileged add-on service-account reads. Deployed use remains OIDC-only; no anonymous data access.

Impersonation requires powerful user/group impersonation grants; a compromised add-on can act with those privileges. Exclude reserved system users/groups and service accounts, accept no caller-supplied identity headers, and grant no unrelated data access. Exact claim mapping and RBAC rules remain implementation choices.

### Hosting, cache, and asset trust

The add-on serves embedded shell/plugin assets over HTTP behind TLS-terminating Ingress/Route. The browser-facing host needs a stable HTTPS origin matching the OIDC redirect registration; an optional load balancer may provide exposure where appropriate. The add-on itself need not manage certificates or require cert-manager.

Install manifests provide optional Ingress/Route templates; administrators supply hostname and certificate configuration. Shell redirect URIs derive from browser origin, so the add-on needs no separate hostname setting. The add-on-to-apiserver path stays in-cluster.

SPA fallback is route-aware: serve `index.html` for root and reserved shell/auth paths and for manifest-derived route prefixes matched on segment boundaries. Do not use `/` as a catch-all. Unknown paths, real asset requests, and API/config endpoints retain their 404/JSON responses rather than booting the app.

| Response class | Cache policy |
| --- | --- |
| Entry document, config, and `remoteEntry.js` | `no-cache` |
| Content-hashed shell/plugin assets | `immutable` with long max-age |
| Authenticated API responses | `no-store` |

No Kubernetes data or authorization cache. OIDC discovery/JWKS may be cached. Content-hashed assets are immutable, but upgrades can remove old lazy chunks before a long-lived browser session reloads; the client may need reload after a chunk 404. The image is the JavaScript trust root. CR write access is administrator-level because config controls public auth settings; CR data cannot point at arbitrary external scripts.

### Browser-local preferences and hub data

The browser stores navigation overrides in identity-scoped IndexedDB, not on the server. Preferences are per browser/device; another device starts from the supplied default or generated layout. This is the minimal OCM tier, not Fleet's planned resource-manager-authoritative preference service.

No database, PVC, or server-side user state is introduced. Storage failure falls back to defaults, never another identity's record. Cross-device synchronization is out of scope.

The hub already aggregates managed-cluster status and OCM workload/placement state (`ManagedCluster`, `ManagedClusterAddOn`, `ManifestWork`, `Placement`/`PlacementDecision`). The console consumes these existing APIs through the facade: no new agents or aggregation service. Managed-cluster-local metrics, logs, and node inspection remain outside this scope.

## Remaining scope

Exact config/schema and package-release details belong to extraction work. Auth claim/RBAC policy belongs to add-on implementation. Fleet asset distribution remains a separate unresolved design topic; see [Addon UI architecture](../../design/addon-ui-architecture.md) and [Client Asset Separation](../../design/client-asset-separation.md). No additional OCM features or third-party plugin system are required by this proposal.

Contract-sharing and compatibility policy, cross-project review rules, and remote preference cache/sync/conflict policy remain unknown and intentionally outside this proposal.

## Key files

These links locate current implementation evidence and likely extraction points; they do not imply proposed APIs already exist.

| File | Relevance |
| --- | --- |
| [AppConfigContext](../../../client/web/src/contexts/AppConfigContext.tsx#L45-L55) | Current shell config shape; proposed catalog derives route identity and separates default layout/preferences. |
| [ScalprumShell](../../../client/web/src/components/Root/ScalprumShell.tsx#L18-L107) | Current provider, injected API, and routing-plugin manifest transform; extraction must hide provider and injection. |
| [PluginLoader](../../../client/web/src/components/Root/PluginLoader.tsx#L4-L28) | Current eager plugin load sequence; candidate provider internals. |
| [Resource-manager UI config](../../../server/internal/transport/http/uiconfig.go#L157-L180) | Current `/api/ui/config`, plus [page generation](../../../server/internal/transport/http/uiconfig.go#L276-L355) and [default layout](../../../server/internal/transport/http/uiconfig.go#L380-L409). |
| [AppNav](../../../client/web/src/components/AppNav/AppNav.tsx#L74-L113) and [PluginPage](../../../client/web/src/pages/PluginPage.tsx#L23-L115) | Current navigation and remote page rendering. |
| [AuthContext](../../../client/web/src/contexts/AuthContext.tsx#L115-L161) and [OIDC config](../../../client/web/src/auth/oidcConfig.ts#L13-L56) | Current Fleet OIDC flow; proposed provider/auth package is not implemented. |
| [PluginLink](../../../sdk/common/src/PluginLink.tsx#L15-L39), [usePluginNavigate](../../../sdk/common/src/usePluginNavigate.tsx#L12-L44), and [FleetApi](../../../sdk/common/src/scalprum.ts#L10-L19) | Current helpers depend on injected Scalprum API. |
| [Navigation types](../../../sdk/common/src/navLayout.ts#L3-L77), [useNavLayout](../../../sdk/common/src/useNavLayout.ts#L7-L31), and [IndexedDB store](../../../sdk/common/src/extensionInstall.ts#L32-L69) | Current layout model and local preference implementation; no server-authoritative preference service yet. |
| [FleetPlugin builders](../../../sdk/build-utils/src/extensions/builders.ts#L30-L45), [FleetPlugin](../../../sdk/build-utils/src/extensions/FleetPlugin.ts#L21-L40), and [core build config](../../../extensions/core/client/rspack.config.ts#L79-L102) | In-repo build contract reused by the POC; not proof of shared shell extraction. |
| [Shell Rspack config](../../../client/web/rspack.config.ts#L30-L41), [async entry](../../../client/web/src/index.ts), and [common exports](../../../sdk/common/package.json#L7-L19) | Build boundary, async bootstrap, and package surface. |
| [Addon UI architecture](../../design/addon-ui-architecture.md) and [Client Asset Separation draft](../../design/client-asset-separation.md) | Different Fleet asset-delivery proposals; neither is settled by the OCM embedded-addon choice. |
