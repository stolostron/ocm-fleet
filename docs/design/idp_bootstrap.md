# Initial platform IdP bootstrap

## What this doc covers

How the platform learns its first provider-tenant IdP before it can authenticate anyone, and how that differs from later IdP changes.

It does not cover delivery-authorization trust at targets, signing-key enrollment, or multi-tenant IdP discovery. Those live in [authentication.md](authentication.md) and related tenancy work.

## The problem

The API cannot safely accept unauthenticated calls to install the first IdP. Once an IdP exists, the intended update model lets authenticated admins change trust through the normal API. Before that, there is nothing to authenticate against.

An open Day One setup listener would allow anyone who can reach it to shape the first trust root. The current server instead refuses to open the public API when the AuthMethod store is empty and initial OIDC configuration is incomplete.

So the first IdP must come from **authority over the runtime**, not from an open network call.

## Two paths, one model

From the platform's point of view, every IdP is external. Dex in the same image, Keycloak next door, or a corporate issuer are the same: an OIDC authority the server discovers and trusts. The server should not embed Dex-specific logic or treat "sandbox IdP" as a second auth system.

There are two ways to set that trust:

1. **Bootstrap** — install the first provider-tenant IdP from runtime authority.
2. **Update** — change IdP configuration through the authenticated API once a trust root exists.

Config-based initial installation is implemented; authenticated updates and an out-of-band recovery channel remain design work. Public AuthMethod create is disabled.

### Bootstrap (runtime authority)

Supply the first IdP when the process starts, or through a later local control channel that proves process ownership. Examples:

- Config / environment on `resource-manager serve` (issuer, audiences, clients as needed)
- A proposed local socket managed through the CLI (not implemented or required for the first cut)

Bootstrap config is **first-trust-only**: it installs an IdP when none exists yet, and does not re-apply or overwrite on later restarts.

Until bootstrap succeeds, do not open the public HTTP API. After bootstrap, start the listener and authenticate against that IdP.

This is the sole bootstrap path. A sandbox image is just packaging that already knows what to put in config.

### Update (authenticated API)

Once the platform trusts an IdP, further trust changes are ordinary authenticated admin operations. The admin's credential chains back to the current anchor.

If the change cannot go through that API (wrong IdP, lockout, recovery), use the local administrative control channel — the same class of runtime-authority path as bootstrap — not another pass of startup config.

Design updates as one model — "current provider IdP, and the procedure to change it." Multi-tenant / multi-IdP discovery is separate and still open.

## Sandbox is packaging, not a server mode

The all-in-one sandbox should look like production as much as possible:

- Same bootstrap-from-config path
- Same auth-method model
- Same update rules when we have them

What makes it a sandbox is the image and entrypoint: a peer Dex (or similar), static clients, and config that registers that issuer on start. Users skip a separate `fleetctl bootstrap` because bootstrap already did that work.

Avoid a distinct `--sandbox` auth mode that freezes the IdP, owns Dex inside the server, or blocks later change. Extra modes cost code and diverge from production. An external issuer uses the same bootstrap model regardless of its product or URL path.

Co-located IdP details stay packaging concerns: TLS termination, private CA material, UI origin contracts, and peer readiness under the process supervisor. The server only sees an ordinary issuer URL and clients.

The implemented [AIO packaging](../../deploy/aio/README.md#external-issuer-dex-off) supplies either a bundled or external issuer through this same bootstrap path.

### Audience and clients

Prefer separate CLI and UI clients. A resource-server-specific audience check is preferable where the provider supports it. Accepting multiple client audiences instead remains a possible future compatibility option, not current behavior or a reason to disable audience validation.

Any future signing ceremony must define its own token and audience requirements as part of its provenance profile; those requirements are separate from ordinary OIDC login.

Ordinary login and group federation should avoid complex provider-specific claim mappings. A group claim may support authorization integration. Login and group federation do not require signing-specific identity-provider claims.

## What the UI and CLI should assume

Clients stay IdP-agnostic. The UI discovers settings from the backend; CLI configuration is currently local, with backend-discovered contexts a future direction. Neither client should depend on provider-specific endpoints.

Consequences:

- No UI control to pick "embedded vs external" IdP as a special product mode.
- After bootstrap, first-run UI can skip OIDC issuer entry and go straight to sign-in (and later setup steps such as addons).
- Client configuration and server trust are distinct; an eventual authenticated trust-update path must govern changes after initial bootstrap.

## Deferred

| Item                                     | Notes                                                                                                                                                                        |
| ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Local-socket / CLI bootstrap             | Useful for production when IdP is not trivially known at start, and later for out-of-band recovery; not required to ship config-based bootstrap. Likely its own control API. |
| IdP update / replace after Day One       | Needed eventually; singular model, not sandbox-specific. Authenticated API first; local control channel when that is unavailable.                                            |
| Multi-tenant IdP discovery / login page  | Needed for multiple issuers; single-tenant can keep a static redirect.                                                                                                       |
| Pivot from sandbox Dex to another issuer | Same as IdP update once that path exists                                                                                                                                     |

## Open questions

- How initial configuration should include trust-signature policy and compose with later authenticated updates.
- Shape of the eventual recovery channel when the configured IdP is wrong or unavailable (ties to the "root user / out-of-band trust" note in [authentication.md](authentication.md#idp-bootstrap-and-recovery)).
- How provider-tenant bootstrap composes with later consumer-tenant IdPs under multitenancy (OME-53).

## Related

- [authentication.md](authentication.md) — delivery authorization, IdP trust at targets, open "root user" challenge
- [architecture/platform_hierarchy.md](architecture/platform_hierarchy.md) — platform bootstrap/pivot (instance lifecycle, not IdP trust)
- [architecture/tenancy_and_permissions.md](architecture/tenancy_and_permissions.md) — provider/tenant model that IdP binding must eventually serve
- OME-206 — sandbox console without a prerequisite external IdP
- OME-7 — Day 1 trust setup (user-facing ceremony once an IdP exists)
- OME-53 — multiple OIDC endpoints for multitenancy
