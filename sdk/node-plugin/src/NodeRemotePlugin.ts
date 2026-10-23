import { ModuleFederationPlugin } from "@module-federation/enhanced/rspack";
import { Compilation, type Compiler, sources } from "@rspack/core";

import { createNodeRemoteManifest, findRemoteEntry } from "./manifest.js";

type FederationOptions = NonNullable<
  ConstructorParameters<typeof ModuleFederationPlugin>[0]
>;

export type NodeRemotePluginOptions = {
  pluginMetadata: {
    name: string;
    version: string;
    exposedModules?: Record<string, string>;
  };
  extensions: unknown[];
  sharedModules?: FederationOptions["shared"];
  entryScriptFilename?: string;
  pluginManifestFilename?: string;
};

const PLUGIN_NAME = "OCMFleetNodeRemotePlugin";

/**
 * Configure a Node-compatible Module Federation container and emit the small
 * SDK-shaped manifest needed to find its entry and runtime chunks.
 */
export class NodeRemotePlugin {
  private readonly options: NodeRemotePluginOptions;
  private readonly federationPlugin: ModuleFederationPlugin;

  constructor(options: NodeRemotePluginOptions) {
    if (!options?.pluginMetadata) {
      throw new Error("NodeRemotePlugin requires pluginMetadata");
    }
    const { name, version, exposedModules = {} } = options.pluginMetadata;
    if (typeof name !== "string" || !name.trim()) {
      throw new Error("Node remote pluginMetadata.name must not be empty");
    }
    if (typeof version !== "string" || !version.trim()) {
      throw new Error("Node remote pluginMetadata.version must not be empty");
    }
    if (
      typeof exposedModules !== "object" ||
      exposedModules === null ||
      Array.isArray(exposedModules)
    ) {
      throw new Error(
        "Node remote pluginMetadata.exposedModules must be an object",
      );
    }
    if (!Array.isArray(options.extensions)) {
      throw new Error("Node remote extensions must be an array");
    }
    if (Object.keys(exposedModules).length === 0) {
      throw new Error("Node remote must expose at least one module");
    }

    const exposes: Record<string, string> = {};
    for (const [moduleName, modulePath] of Object.entries(exposedModules)) {
      if (
        !moduleName.trim() ||
        typeof modulePath !== "string" ||
        !modulePath.trim()
      ) {
        throw new Error(
          "Node remote exposed module names and paths must not be empty",
        );
      }
      const federationKey = moduleName.startsWith("./")
        ? moduleName
        : `./${moduleName}`;
      if (Object.hasOwn(exposes, federationKey)) {
        throw new Error(`Duplicate Node remote exposed module "${moduleName}"`);
      }
      exposes[federationKey] = modulePath;
    }
    const entryScriptFilename =
      options.entryScriptFilename ?? "remoteEntry.cjs";
    if (
      typeof entryScriptFilename !== "string" ||
      !entryScriptFilename.trim()
    ) {
      throw new Error("Node remote entryScriptFilename must not be empty");
    }
    if (entryScriptFilename.includes("[")) {
      throw new Error(
        "Node remote entryScriptFilename templates are unsupported; use a static filename",
      );
    }

    this.options = options;
    this.federationPlugin = new ModuleFederationPlugin({
      name,
      filename: entryScriptFilename,
      exposes,
      shared: options.sharedModules,
      library: { type: "commonjs-module" },
      dts: false,
      manifest: false,
    });
  }

  apply(compiler: Compiler): void {
    const targets = Array.isArray(compiler.options.target)
      ? compiler.options.target
      : [compiler.options.target];
    const hasNodeTarget = targets.some(
      (target) =>
        typeof target === "string" &&
        /^(?:async-)?node(?:\d+(?:\.\d+)?)?$/.test(target),
    );
    if (!hasNodeTarget) {
      throw new Error(
        'NodeRemotePlugin requires Rspack target "node" or "async-node"',
      );
    }

    // The Rspack entry point is structurally compatible but Module Federation
    // still exposes webpack's nominal compiler type.
    this.federationPlugin.apply(compiler as never);

    compiler.hooks.thisCompilation.tap(PLUGIN_NAME, (compilation) => {
      compilation.hooks.processAssets.tap(
        {
          name: PLUGIN_NAME,
          stage: Compilation.PROCESS_ASSETS_STAGE_ADDITIONAL,
        },
        () => {
          const remoteEntryFilename =
            this.options.entryScriptFilename ?? "remoteEntry.cjs";
          let ambiguousEntryAsset = false;
          const entrypoints = [...compilation.entrypoints.values()].flatMap(
            (entrypoint) => {
              const loadScripts = [...entrypoint.getFiles()];
              const match = findRemoteEntry(loadScripts, remoteEntryFilename);
              if (match.status === "ambiguous") {
                ambiguousEntryAsset = true;
                return [];
              }
              return match.status === "matched"
                ? [{ entrypoint, loadScripts }]
                : [];
            },
          );

          if (ambiguousEntryAsset) {
            compilation.errors.push(
              new Error(
                `Module Federation remote entry "${remoteEntryFilename}" matches multiple assets in an entrypoint`,
              ),
            );
            return;
          }
          if (entrypoints.length === 0) {
            compilation.errors.push(
              new Error(
                `Module Federation remote entry "${remoteEntryFilename}" was not emitted`,
              ),
            );
            return;
          }
          if (entrypoints.length > 1) {
            compilation.errors.push(
              new Error(
                `Module Federation remote entry "${remoteEntryFilename}" is ambiguous across entrypoints`,
              ),
            );
            return;
          }

          const manifest = createNodeRemoteManifest({
            name: this.options.pluginMetadata.name,
            version: this.options.pluginMetadata.version,
            remoteEntryFilename,
            loadScripts: entrypoints[0].loadScripts,
            extensions: this.options.extensions,
          });
          const manifestFilename =
            this.options.pluginManifestFilename ??
            `${this.options.pluginMetadata.name}-manifest.json`;
          compilation.emitAsset(
            manifestFilename,
            new sources.RawSource(`${JSON.stringify(manifest, null, 2)}\n`),
          );
        },
      );
    });
  }
}
