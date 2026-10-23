# Node Module Federation producer

`@ocm-fleet/node-plugin` is a private SDK package for producing Node-targeted
Module Federation remotes with Rspack. It is intentionally independent of
Fleet extension contracts and browser plugin runtime packages.

## Usage

Add `NodeRemotePlugin` to a Node-targeted Rspack build. The API mirrors the
existing web producer's SDK-shaped metadata, extensions, sharing, and output
option names without importing its Fleet-specific extension types:

```ts
new NodeRemotePlugin({
  pluginMetadata: {
    name: "example_remote",
    version: "1.0.0",
    exposedModules: { command: "./src/command.ts" },
  },
  extensions: [{ type: "example.command", properties: { id: "inspect" } }],
  sharedModules: {},
  entryScriptFilename: "remoteEntry.cjs",
  pluginManifestFilename: "example_remote-manifest.json",
});
```

`pluginMetadata.exposedModules` keys become Module Federation expose keys: a
key `command` maps internally to `./command`; an already prefixed `./command`
stays unchanged. Public SDK lookup names do not gain the `./` prefix. The plugin
requires `target: "node"` or `target: "async-node"`, configures a
CommonJS-compatible container, and rejects filename templates because the
manifest currently requires a stable producer-declared entry path.

The plugin emits a manifest alongside the remote. It preserves generic
`extensions` values and SDK-shaped `name`, `version`, `registrationMethod:
"custom"`, `baseURL: "auto"`, and `loadScripts` fields. `remoteEntry` identifies
the actual container; `runtimeDependencies` records other entrypoint scripts.
The producer orders runtime scripts before the entry, but entry identification
does not rely on ordering. The Module Federation runtime owns remote
initialization and lazy chunk acquisition; the manifest does not instruct a
host to hand-load every script.

Hosts can resolve `baseURL: "auto"` through their existing
`transformPluginManifest` path and pass transformed absolute `loadScripts` to
`identifyRemoteEntry(loadScripts, manifest.remoteEntry)`. The helper returns
the exact matching transformed URL, including prefix and query, and rejects
missing or ambiguous matches. It performs no base inference or second rewrite;
this adds no asset-source contract or Fleet host integration.

## Scope and verification

This package produces remotes only. Node-side remote loading, Scalprum loader
changes, CLI discovery/caching, and Kind integration are out of scope. The
package is `private`; it is not included in publication or release workflows.
The `node-plugin:fixture` Nx target builds a multi-script Node remote, checks
its `init` and `get` exports, executes default/named exports and a lazy chunk,
and verifies transformed-path entry identification.

The fixture qualifies Rspack 2 CommonJS (`commonjs-module`) output on the
installed Node 24 runtime, including a separate runtime chunk. A future Enhanced
runtime adapter must select the matching CommonJS remote type rather than its
browser/global default. Node 22 and bundled-host/adapter qualification remain
separate checks; this fixture uses Node's native CommonJS container loading and
does not claim full loader integration.
