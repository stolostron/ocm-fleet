# TypeScript CLI Rules

## Scope

- This package owns TypeScript `fleetctl`.
- Keep command groups under `src/commands/<group>/`.
- Keep every command in its own file.
- Keep parser, rendering, config, and transport code outside command groups.
- Shared SDK and dynamic plugin loading are in scope for the CLI. Use the SDK-compatible Scalprum Node runtime loader (`@scalprum/node-loader`, injected via `initialize({ appsConfig, loader })`) with remotes produced by `@ocm-fleet/node-plugin`; do not hand-roll a separate remote-loading mechanism.

## Structure

- `src/main.tsx`: process entry point and dispatch.
- `src/command.tsx`: typed command registration adapter.
- `src/app.tsx`: output formatting, stdout writes, and Ink rendering.
- `src/commands/registry.ts`: command registration only.
- `src/commands/help.ts`: help rendering.
- `src/commands/<group>/index.ts`: group metadata and child registration.
- `src/commands/<group>/<command>.tsx`: one command per file, exporting a typed command spec; use JSX when needed.
- `src/__tests__/`: Vitest and Ink component tests.

## Verification

Run from repository root through Nx:

```text
npx nx run cli:build
npx nx run cli:test
npx nx run cli:verify # formatting/import fixes, lint, typecheck, and tests
```

Use `ink-testing-library` for Ink components. Use `execa` for subprocess tests.

Executable name is `fleetctl`. Build through root target `npx nx run fleetctl`; it updates top-level `bin/fleetctl`.
