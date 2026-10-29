# Node Module Federation producer

`@ocm-fleet/node-plugin` is a private SDK package for producing Node-targeted
Module Federation remotes with Rspack. It is intentionally independent of
Fleet extension contracts and browser plugin runtime packages.

## Usage

Install `@module-federation/enhanced` (^2.9.0), `@module-federation/node`
(^2.7.52), and `@rspack/core` (>=2.0.8) alongside this package; they are peers.
Add `NodeRemotePlugin` to a Node-targeted Rspack build. The API mirrors the
existing web producer's SDK-shaped metadata, extensions, sharing, and output
option names without importing its Fleet-specific extension types:

```ts
import path from "node:path";

import { NodeRemotePlugin } from "@ocm-fleet/node-plugin";

export default {
  mode: "production",
  target: "async-node",
  entry: {},
  output: { path: path.resolve("dist/remote") },
  plugins: [
    new NodeRemotePlugin({
      pluginMetadata: {
        name: "example_remote",
        version: "1.0.0",
        exposedModules: { command: "./src/command.js" },
      },
      extensions: [{ type: "example.command", properties: { id: "inspect" } }],
      sharedModules: {},
      entryScriptFilename: "remoteEntry.cjs",
      pluginManifestFilename: "example_remote-manifest.json",
    }),
  ],
};
```

`pluginMetadata.exposedModules` keys become Module Federation expose keys: a
key `command` maps internally to `./command`; an already prefixed `./command`
stays unchanged. Public SDK lookup names do not gain the `./` prefix. The plugin
requires `target: "node"` or `target: "async-node"` (including versioned Node
targets). It configures a `commonjs-module` container and includes
`@module-federation/node/runtimePlugin` by default, enabling HTTP acquisition of
exposed modules and their lazy chunks.

The plugin supplies `output.module: false`, `output.filename: "[name].cjs"`,
`output.chunkFilename: "[name].cjs"`, `output.chunkFormat: "commonjs"`,
`output.chunkLoading: "async-node"`, and `optimization.runtimeChunk: false`.
Explicit incompatible options fail the build: ESM output, synchronous or browser
chunk loading, non-CommonJS chunks, non-`.cjs` filenames, and extracted initial
runtimes are unsupported. Custom entry filenames must be static and end in
`.cjs`; output chunk filenames may contain Rspack placeholders. Initial
`splitChunks` extraction also fails if it gives the container additional initial
assets: an HTTP-loaded entry must embed its initial runtime. Lazy chunks remain
separate and load through the Node federation runtime.

Use `entry: {}` for a producer-only build so Rspack does not also build its default
application entry. The caller owns loaders, JSX transforms, externals, and shared
dependency policy. In particular, React/Ink identity and native ESM interop are
application concerns; this generic producer adds no React or Ink configuration.

The plugin emits a manifest alongside the remote. It preserves generic
`extensions` values and SDK-shaped `name`, `version`, `registrationMethod:
"custom"`, `baseURL: "auto"`, and `loadScripts` fields. `remoteEntry` identifies
the actual container. Builds produced by `NodeRemotePlugin` have a single
`loadScripts` entry and empty `runtimeDependencies`; the generic manifest helper
can still represent multiple scripts. Entry identification does not rely on
ordering. The Module Federation runtime owns remote
initialization and lazy chunk acquisition; the manifest does not instruct a
host to hand-load every script.

Hosts can resolve `baseURL: "auto"` through their existing
`transformPluginManifest` path and pass transformed absolute `loadScripts` to
`identifyRemoteEntry(loadScripts, manifest.remoteEntry)`. The helper returns
the exact matching transformed URL, including prefix and query, and rejects
missing or ambiguous matches. It performs no base inference or second rewrite;
this adds no asset-source contract or Fleet host integration.

## Scope and verification

This package produces remotes only; the CLI host and runtime integration live in
`client/cli`. The package is `private`; it is not included in publication or
release workflows. The production [`e2e-cli:test:plugin` acceptance target](../../client/cli/README.md#node-federation-plugin-acceptance)
executes the built CLI with a remote produced by this package. It exercises the
Scalprum Node loader, Enhanced remote registration/initialization, manifest
validation through `getModule`, asset URL transformation, command module loading,
and Ink rendering over real HTTP. The `node-plugin:fixture` target remains a
producer/runtime fixture: it builds a Node remote and a separate Rspack host,
then verifies default/named exports, a lazy export, entry/chunk requests, the
single-script manifest, and transformed-path entry identification. Enhanced owns
remote initialization and share-scope handling; the fixture does not hand-load
dependencies or manually initialize the remote container.

Unit tests cover incompatible compiler options and producer metadata validation.

The producer fixture qualifies Rspack 2.1.7, Enhanced 2.9.2, and Node federation
2.7.52 on the installed Node 26.8.1 runtime. Node 22 qualification remains
separate; the CLI e2e result reports the Node runtime used for its execution.
