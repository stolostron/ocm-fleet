# Console Authentication

The console uses the configured OIDC issuer for login through `react-oidc-context` and `oidc-client-ts`, independently of provider-specific account APIs or roles.

## Configuration and identity

The shell reads issuer (`authority`), UI client ID, and scope from `/api/ui/config`. It uses authorization code flow and OIDC discovery, with automatic silent renewal. Register `/app/auth/callback` and `/app/silent-renew.html` on the console origin.

The server must trust the issuer and API access-token audience. See [AIO configuration](../../deploy/aio/README.md#external-issuer-dex-off) for deployment settings.

The shell projects identity from standard OIDC claims:

| Shell value    | Source                                                                                      |
| -------------- | ------------------------------------------------------------------------------------------- |
| `id`           | `sub`                                                                                       |
| `username`     | First nonempty string among `preferred_username`, `email`, `name`, then `sub`               |
| `display_name` | Capitalized `preferred_username` when present; otherwise `name`, then the resolved username |

Optional display claims are trimmed; empty or non-string values are ignored. The shell does not infer local `ops`/`dev` roles from `realm_access` or another provider-specific role claim. Display claims and navigation visibility do not establish API permissions. Issuer-scoped identity and authorization are server concerns; see the [authentication design](../design/authentication.md).

Sources: [OIDC configuration](../../client/web/src/auth/oidcConfig.ts), [identity projection](../../client/web/src/auth/oidcIdentity.ts), [AuthContext](../../client/web/src/contexts/AuthContext.tsx).

## Debugging login

1. Inspect `/api/ui/config` for the expected issuer, client, and scope. Compare the issuer exactly with discovery, including any opaque path.
2. Inspect discovery and callback failures in browser developer tools. Check registered redirect URIs, TLS trust, browser origin/CORS settings, and provider-supported scopes.
3. For API 401 responses, check issuer and resource audience independently from display claims.
