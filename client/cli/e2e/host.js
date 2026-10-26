import assert from "node:assert/strict";
import { PassThrough } from "node:stream";

import { getInstance } from "@module-federation/enhanced/runtime";
import { getModule, initialize } from "@scalprum/core";
import { createNodePluginLoader } from "@scalprum/node-loader";
import React from "react";

import { renderView } from "../src/app.tsx";

const remoteName = "fleetctl_fixture_remote";
const manifestURL = process.env.FIXTURE_MANIFEST_URL;
const mode = process.env.FIXTURE_MODE ?? "success";

if (!manifestURL) throw new Error("FIXTURE_MANIFEST_URL is required");

const counters = {
  registerRemotes: 0,
  readinessAcquisitions: 0,
  moduleInits: 0,
  loadRemote: 0,
  manifestTransforms: 0,
};
const loaderEvents = [];
const remoteModuleResults = [];

const runtime = getInstance();
assert.ok(runtime, "Rspack host did not initialize Enhanced runtime");

const registerRemotes = runtime.registerRemotes.bind(runtime);
runtime.registerRemotes = (...args) => {
  counters.registerRemotes += 1;
  return registerRemotes(...args);
};

const remoteHandler = runtime.remoteHandler;
if (typeof remoteHandler?.getRemoteModuleAndOptions !== "function") {
  throw new Error("Enhanced host does not expose remote readiness API");
}
const getRemoteModuleAndOptions =
  remoteHandler.getRemoteModuleAndOptions.bind(remoteHandler);
remoteHandler.getRemoteModuleAndOptions = async (...args) => {
  counters.readinessAcquisitions += 1;
  const result = await getRemoteModuleAndOptions(...args);
  const moduleInit = result.module.init.bind(result.module);
  return {
    ...result,
    module: new Proxy(result.module, {
      get(target, property, receiver) {
        if (property === "init") {
          return (...initArgs) => {
            counters.moduleInits += 1;
            return moduleInit(...initArgs);
          };
        }
        return Reflect.get(target, property, receiver);
      },
    }),
  };
};

const loadRemote = runtime.loadRemote.bind(runtime);
runtime.loadRemote = async (id, ...args) => {
  counters.loadRemote += 1;
  if (
    (mode === "null-module" || mode === "undefined-module") &&
    id === `${remoteName}/command`
  ) {
    // Preserve real registration and readiness; isolate only the module result.
    const result = mode === "null-module" ? null : undefined;
    remoteModuleResults.push({
      id,
      kind: result === null ? "null" : "undefined",
    });
    return result;
  }
  const result = await loadRemote(id, ...args);
  remoteModuleResults.push({
    id,
    kind:
      result === null ? "null" : result === undefined ? "undefined" : "module",
  });
  return result;
};

let transformedEntry;
const loader = createNodePluginLoader({
  transformPluginManifest(manifest) {
    counters.manifestTransforms += 1;
    if (manifest.baseURL !== "auto") {
      throw new Error(`Expected baseURL auto, got ${manifest.baseURL}`);
    }
    const loadScripts = manifest.loadScripts.map(
      (script) => new URL(script, manifestURL).href,
    );
    transformedEntry = loadScripts.find((script) =>
      /(?:^|\/)remoteEntry(?:\.[\w-]+)?\.(?:c|m)?js(?:[?#].*)?$/i.test(script),
    );
    return { ...manifest, loadScripts };
  },
});
const loadPluginManifest = loader.loadPluginManifest.bind(loader);
loader.loadPluginManifest = async (...args) => {
  try {
    const manifest = await loadPluginManifest(...args);
    loaderEvents.push({ stage: "manifest", success: true });
    return manifest;
  } catch (error) {
    loaderEvents.push({
      stage: "manifest",
      success: false,
      errorMessage: error instanceof Error ? error.message : String(error),
    });
    throw error;
  }
};
const loadPlugin = loader.loadPlugin.bind(loader);
loader.loadPlugin = async (...args) => {
  const result = await loadPlugin(...args);
  loaderEvents.push({
    stage: "plugin",
    success: result.success,
    ...(result.errorMessage ? { errorMessage: result.errorMessage } : {}),
  });
  if (result.entryModule && typeof result.entryModule.get === "function") {
    const getModule = result.entryModule.get.bind(result.entryModule);
    result.entryModule.get = async (...moduleArgs) => {
      try {
        return await getModule(...moduleArgs);
      } catch (error) {
        loaderEvents.push({
          stage: "exposed-module",
          success: false,
          errorMessage: error instanceof Error ? error.message : String(error),
        });
        throw error;
      }
    };
  }
  return result;
};

initialize({
  appsConfig: {
    [remoteName]: { name: remoteName, manifestLocation: manifestURL },
  },
  loader,
});

function resultRecord(extra = {}) {
  return {
    mode,
    counters,
    loaderEvents,
    remoteModuleResults,
    transformedEntry,
    ...extra,
  };
}

function emitResult(result) {
  process.stdout.write(`FLEETCTL_NODE_E2E_RESULT:${JSON.stringify(result)}\n`);
}

async function waitForOutput(
  stream,
  chunks,
  expected,
  timeoutMilliseconds = 8000,
) {
  const visibleText = () =>
    Buffer.concat(chunks)
      .toString("utf8")
      .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "");

  if (visibleText().includes(expected)) return visibleText();

  return new Promise((resolve, reject) => {
    const onData = () => {
      const text = visibleText();
      if (text.includes(expected)) {
        cleanup();
        resolve(text);
      }
    };
    const onError = (error) => {
      cleanup();
      reject(error);
    };
    const timer = setTimeout(() => {
      cleanup();
      reject(
        new Error(`Ink render did not contain ${JSON.stringify(expected)}`),
      );
    }, timeoutMilliseconds);
    const cleanup = () => {
      clearTimeout(timer);
      stream.off("data", onData);
      stream.off("error", onError);
    };
    stream.on("data", onData);
    stream.on("error", onError);
    onData();
  });
}

async function renderRemoteCommand() {
  const RemoteCommand = await getModule(remoteName, "command");
  const remoteLabel = await getModule(remoteName, "command", "remoteLabel");
  assert.equal(typeof RemoteCommand, "function");
  assert.equal(remoteLabel, "Node federation command");

  const stdout = new PassThrough();
  const chunks = [];
  stdout.on("data", (chunk) => chunks.push(Buffer.from(chunk)));
  const instance = renderView(React.createElement(RemoteCommand), stdout);
  try {
    const rendered = await waitForOutput(
      stdout,
      chunks,
      "Remote command: ready",
    );
    return { remoteLabel, rendered: rendered.trim() };
  } finally {
    instance.unmount();
    stdout.destroy();
  }
}

async function main() {
  if (mode === "invalid-manifest") {
    try {
      await loader.loadPluginManifest(manifestURL);
    } catch (error) {
      emitResult(
        resultRecord({
          expectedFailure:
            error instanceof Error ? error.message : String(error),
        }),
      );
      return;
    }
    throw new Error(
      "Expected malformed manifest to fail Node loader validation",
    );
  }

  if (mode === "retry-readiness") {
    // Both getModule calls run in this host process; the second must retry the failed entry.
    let firstFailure;
    try {
      await getModule(remoteName, "command");
    } catch (error) {
      firstFailure = error instanceof Error ? error.message : String(error);
    }
    assert.ok(firstFailure, "Expected initial Enhanced readiness to fail");
    const rendered = await renderRemoteCommand();
    emitResult(resultRecord({ firstFailure, ...rendered }));
    return;
  }

  if (mode === "success") {
    const rendered = await renderRemoteCommand();
    emitResult(resultRecord(rendered));
    return;
  }

  try {
    await getModule(remoteName, "command");
  } catch (error) {
    emitResult(
      resultRecord({
        expectedFailure: error instanceof Error ? error.message : String(error),
      }),
    );
    return;
  }

  throw new Error(`Expected ${mode} scenario to fail`);
}

main().catch((error) => {
  process.stderr.write(
    `${error instanceof Error ? error.stack : String(error)}\n`,
  );
  process.exitCode = 1;
});
