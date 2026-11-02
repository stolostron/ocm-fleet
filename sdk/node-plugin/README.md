# @ocm-fleet/node-plugin

A private workspace package for producing Node.js Module Federation remotes with Rspack. It is not published or included in release workflows. The package provides a generic producer and manifest helpers; production host integration is owned by `client/cli` and is not implemented by this producer package.

## Quick start

Use this package through the repository's npm workspaces. The consuming build must provide these peer dependencies:

| Package                       | Required version |
| ----------------------------- | ---------------- |
| `@module-federation/enhanced` | `^2.9.0`         |
| `@module-federation/node`     | `^2.7.52`        |
| `@rspack/core`                | `>=2.0.8`        |

After building the package, use an ESM Rspack configuration such as `rspack.config.mjs`:

```js
import path from "node:path";

import { NodeRemotePlugin } from "@ocm-fleet/node-plugin";

export default {
  mode: "production",
  entry: {},
  target: "async-node",
  output: { path: path.resolve("dist/remote") },
  plugins: [
    new NodeRemotePlugin({
      pluginMetadata: {
        name: "example_remote",
        version: "1.0.0",
        exposedModules: { command: "./src/command.js" },
      },
      extensions: [],
    }),
  ],
};
```

Create `src/command.js` with the exports your host will consume. `entry: {}` avoids Rspack's default application entry in a producer-only build.

The plugin configures a `commonjs-module` container with a self-contained initial runtime and includes `@module-federation/node/runtimePlugin` for HTTP loading of exposed modules and lazy chunks. It supplies these output defaults:

```js
output: {
  module: false,
  filename: "[name].cjs",
  chunkFilename: "[name].cjs",
  chunkFormat: "commonjs",
  chunkLoading: "async-node",
},
optimization: { runtimeChunk: false },
```

The caller owns TypeScript/JSX transforms, loaders, externals, and shared dependency policy. There is no automatic React or Ink preset; dependency identity and native ESM interop remain application concerns.

## Options and validation

`NodeRemotePlugin` accepts `NodeRemotePluginOptions`, exported from the package:

| Option                          | Default and behavior                                                                        |
| ------------------------------- | ------------------------------------------------------------------------------------------- |
| `pluginMetadata.name`           | Required nonempty string; the federation container and manifest name.                       |
| `pluginMetadata.version`        | Required nonempty string; the manifest version.                                             |
| `pluginMetadata.exposedModules` | Map of public lookup names to source paths. At least one entry is required.                 |
| `extensions`                    | Required array of generic values, preserved in the manifest; `[]` is valid.                 |
| `sharedModules`                 | Optional; passed to Enhanced's `shared` option without a preset.                            |
| `entryScriptFilename`           | `"remoteEntry.cjs"`; must be a nonempty static `.cjs` filename without Rspack placeholders. |
| `pluginManifestFilename`        | `"<name>-manifest.json"`.                                                                   |

Expose names and paths must be nonempty. `command` becomes federation key `./command`; an existing `./` prefix is preserved. Names that normalize to the same key are rejected. Public SDK lookup names do not gain the prefix.

The build requires `node` or `async-node` targets, including versioned Node targets. Explicit ESM output, non-CommonJS chunks, chunk loading other than `async-node`, non-`.cjs` output filenames, and extracted runtime chunks are rejected. Output filename and chunk filename templates are allowed if they end in `.cjs`. Initial `splitChunks` extraction is rejected if it adds initial assets to the container; lazy chunks remain separate. Missing or ambiguous remote entry assets also fail the build.

## Manifest and host handoff

The generated `<name>-manifest.json` contains `name`, `version`, the supplied `extensions`, `registrationMethod: "custom"`, `baseURL: "auto"`, `loadScripts`, `remoteEntry`, and `runtimeDependencies`. Plugin-produced manifests have one `loadScripts` entry identifying the actual container and an empty `runtimeDependencies` array. The exported `createNodeRemoteManifest` helper can also represent multiple scripts; entry identification does not depend on ordering.

A host can resolve `baseURL: "auto"` using its existing `transformPluginManifest` path, then pass the transformed absolute script URLs to the exported helper:

```js
import { identifyRemoteEntry } from "@ocm-fleet/node-plugin";

const entryURL = identifyRemoteEntry(
  ["https://assets.example.test/plugins/example/remoteEntry.cjs?rev=42"],
  "remoteEntry.cjs",
);
// "https://assets.example.test/plugins/example/remoteEntry.cjs?rev=42"
```

`identifyRemoteEntry(loadScripts, remoteEntry)` returns the exact matching input URL, preserving its prefix, query, and fragment. Matching ignores query/fragment suffixes and accepts an exact asset path or a path ending in `/<remoteEntry>` after path normalization. Missing and ambiguous matches throw. The helper performs no base inference or second URL rewrite, and this package does not export `transformPluginManifest`.

The federation runtime owns initialization, share scopes, and lazy chunk acquisition. The manifest is not an instruction to hand-load every script. The producer package does not own Scalprum/Fleet host discovery or caching; the CLI's explicit plugin command and runtime integration are documented in [the CLI guide](../../client/cli/README.md#node-federation-plugin-acceptance).

## Verification

Run from the repository root:

```sh
npx nx run node-plugin:build
npx nx run node-plugin:lint
npx nx run node-plugin:test
npx nx run node-plugin:fixture
```

Unit tests cover metadata and incompatible compiler options. The fixture builds a remote and a Rspack-bundled CommonJS Enhanced host, serves the remote over real HTTP, and verifies default/named exports, a lazy export's result, entry/chunk requests, the single-script manifest, and transformed-path entry identification. The host uses `createInstance`, `registerRemotes`, `loadRemote`, and the Node runtime plugin without manually calling `remote.init`.

The documented qualification used Node 26.8.1, Rspack 2.1.7, Enhanced 2.9.2, and Node federation 2.7.52. Node 22 has not been qualified.

See [Node Module Federation producer](../../docs/ui/node-plugin.md) for more detail on the producer contract and verification scope.
