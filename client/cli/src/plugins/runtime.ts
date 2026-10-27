import { init } from "@module-federation/enhanced/runtime";
import nodeRuntimePlugin from "@module-federation/node/runtimePlugin";
import { consoleLogger } from "@openshift/dynamic-plugin-sdk";
import { getModule, initialize } from "@scalprum/core";
import { createNodePluginLoader } from "@scalprum/node-loader";
import * as React from "react";
import * as jsxRuntime from "react/jsx-runtime";

export interface PluginRuntime {
  getModule<T = unknown>(pluginName: string, moduleName: string): Promise<T>;
  dispose(): void;
}

export function pluginManifestLocation(
  baseURL: string,
  pluginName: string,
): string {
  if (!/^[A-Za-z0-9_-]+$/.test(pluginName)) {
    throw new Error(`Invalid plugin name "${pluginName}".`);
  }

  const base = new URL(baseURL);
  if (!base.pathname.endsWith("/")) base.pathname += "/";
  const manifest = new URL(`${pluginName}-manifest.json`, base);
  manifest.search = base.search;
  return manifest.href;
}

/** Initialize Scalprum's SDK PluginStore with its Node Module Federation loader. */
export function initializePluginRuntime(
  baseURL: string,
  pluginName: string,
): PluginRuntime {
  const manifestLocation = pluginManifestLocation(baseURL, pluginName);

  if (!Reflect.get(globalThis, "__webpack_share_scopes__")) {
    Reflect.set(globalThis, "__webpack_share_scopes__", { default: {} });
  }
  if (!Reflect.get(globalThis, "__webpack_init_sharing__")) {
    Reflect.set(globalThis, "__webpack_init_sharing__", async () => undefined);
  }

  init({
    name: "fleetctl",
    remotes: [],
    shared: {
      react: {
        version: React.version,
        lib: () => React,
        shareConfig: { singleton: true, requiredVersion: false },
      },
      "react/jsx-runtime": {
        version: React.version,
        lib: () => jsxRuntime,
        shareConfig: { singleton: true, requiredVersion: false },
      },
    },
    plugins: [nodeRuntimePlugin()],
  });

  const originalFetch = globalThis.fetch;
  const originalInfo = consoleLogger.info;
  const infoToStderr = (...args: unknown[]) => console.error(...args);
  const loader = createNodePluginLoader({
    fetchImpl: (url, requestInit) => originalFetch(url, requestInit),
    transformPluginManifest(manifest) {
      if (Reflect.get(manifest, "baseURL") !== "auto") return manifest;

      const assetBase = new URL(".", manifestLocation);
      const loadScripts: unknown = Reflect.get(manifest, "loadScripts");
      if (!Array.isArray(loadScripts)) return manifest;
      return {
        ...manifest,
        baseURL: assetBase.href,
        loadScripts: loadScripts.map(
          (script) => new URL(script, assetBase).href,
        ),
      } as typeof manifest;
    },
  });

  const fetchWithValidatedManifest = async (
    input: RequestInfo | URL,
    requestInit?: RequestInit,
  ): Promise<Response> => {
    const requestedURL =
      input instanceof Request
        ? input.url
        : input instanceof URL
          ? input.href
          : input;
    if (requestedURL !== manifestLocation) {
      return originalFetch(input, requestInit);
    }

    // Scalprum 0.13 fetches string manifest locations before handing the parsed
    // object to its injected loader. Route that fetch through the Node loader's
    // validator, then let getModule continue through the normal PluginStore path.
    const manifest = await loader.loadPluginManifest(manifestLocation);
    return new Response(JSON.stringify(manifest), {
      headers: { "content-type": "application/json" },
    });
  };
  globalThis.fetch = fetchWithValidatedManifest;
  consoleLogger.info = infoToStderr;

  try {
    initialize({
      appsConfig: {
        [pluginName]: { name: pluginName, manifestLocation },
      },
      loader,
    });
  } catch (error) {
    if (globalThis.fetch === fetchWithValidatedManifest) {
      globalThis.fetch = originalFetch;
    }
    if (consoleLogger.info === infoToStderr) {
      consoleLogger.info = originalInfo;
    }
    throw error;
  }

  return {
    getModule: <T>(name: string, moduleName: string) =>
      getModule<T>(name, moduleName),
    dispose: () => {
      if (globalThis.fetch === fetchWithValidatedManifest) {
        globalThis.fetch = originalFetch;
      }
      if (consoleLogger.info === infoToStderr) {
        consoleLogger.info = originalInfo;
      }
    },
  };
}

export async function loadPluginCommand(
  runtime: PluginRuntime,
  pluginName: string,
  moduleName: string,
  retry: boolean,
): Promise<unknown> {
  let command: unknown;
  try {
    command = await runtime.getModule(pluginName, moduleName);
  } catch (error) {
    if (!retry) throw error;
    command = await runtime.getModule(pluginName, moduleName);
  }

  if (command === null || command === undefined) {
    throw new Error(
      `Plugin "${pluginName}" module "${moduleName}" did not export a command component.`,
    );
  }
  if (typeof command !== "function") {
    throw new Error(
      `Plugin "${pluginName}" module "${moduleName}" did not export a command component.`,
    );
  }
  return command;
}
