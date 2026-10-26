import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createRequire } from "node:module";
import { createServer } from "node:http";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { ModuleFederationPlugin } from "@module-federation/enhanced/rspack";
import rspack from "@rspack/core";
import { NodeRemotePlugin } from "@ocm-fleet/node-plugin";

const execFileAsync = promisify(execFile);
const require = createRequire(import.meta.url);
const cliRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const scratch = await mkdtemp(path.join(os.tmpdir(), "fleetctl-node-e2e-"));
const remotePath = path.join(scratch, "remote");
const hostPath = path.join(scratch, "host");
const remoteName = "fleetctl_fixture_remote";
const requests = [];
const sharedModules = {
  react: { singleton: true, eager: true, requiredVersion: false },
  "react/jsx-runtime": { singleton: true, eager: true, requiredVersion: false },
};
let serverScenario = "success";
let retryEntryRequests = 0;
let server;

async function build(config) {
  const compiler = rspack(config);
  try {
    return await new Promise((resolve, reject) => {
      compiler.run((error, result) => {
        if (error) reject(error);
        else if (result?.hasErrors()) {
          reject(new Error(result.toString({ all: false, errors: true })));
        } else resolve(result);
      });
    });
  } finally {
    await new Promise((resolve, reject) =>
      compiler.close((error) => (error ? reject(error) : resolve())),
    );
  }
}

function transpileTsxRule() {
  return {
    test: /\.[jt]sx?$/,
    exclude: /node_modules/,
    loader: "builtin:swc-loader",
    options: {
      jsc: {
        parser: { syntax: "typescript", tsx: true },
        transform: { react: { runtime: "automatic" } },
      },
    },
  };
}

async function buildRemote() {
  await build({
    context: cliRoot,
    mode: "production",
    target: "async-node",
    entry: {},
    resolve: { extensions: [".tsx", ".ts", ".jsx", ".js"] },
    module: { rules: [transpileTsxRule()] },
    externalsPresets: { node: true },
    optimization: { minimize: false },
    output: { path: remotePath, clean: true },
    plugins: [
      new NodeRemotePlugin({
        pluginMetadata: {
          name: remoteName,
          version: "1.0.0",
          exposedModules: { command: "./e2e/remote-command.tsx" },
        },
        extensions: [{ type: "cli.command", properties: { id: "fixture" } }],
        sharedModules,
      }),
    ],
  });

  const manifest = JSON.parse(
    await readFile(
      path.join(remotePath, `${remoteName}-manifest.json`),
      "utf8",
    ),
  );
  assert.equal(manifest.name, remoteName);
  assert.equal(manifest.baseURL, "auto");
  assert.deepEqual(manifest.loadScripts, [manifest.remoteEntry]);
  assert.equal(manifest.remoteEntry, "remoteEntry.cjs");
  return manifest;
}

async function buildHost() {
  await build({
    context: cliRoot,
    mode: "production",
    target: "async-node",
    entry: "./e2e/host.js",
    resolve: { extensions: [".tsx", ".ts", ".jsx", ".js"] },
    module: { rules: [transpileTsxRule()] },
    externalsPresets: { node: true },
    optimization: { minimize: false, runtimeChunk: false },
    output: {
      path: hostPath,
      filename: "host.cjs",
      chunkFilename: "[name].cjs",
      module: false,
      chunkFormat: "commonjs",
      chunkLoading: "async-node",
    },
    plugins: [
      new ModuleFederationPlugin({
        name: "fleetctl_node_e2e_host",
        remotes: [],
        shared: sharedModules,
        dts: false,
        manifest: false,
        runtimePlugins: [
          require.resolve("@module-federation/node/runtimePlugin"),
        ],
      }),
    ],
  });
}

async function startServer(manifest) {
  server = createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    const pathname = url.pathname;
    const record = { method: request.method, path: pathname, status: 200 };
    requests.push(record);

    if (pathname === "/plugins/remote-manifest.json") {
      const delivered = structuredClone(manifest);
      switch (url.searchParams.get("case")) {
        case "missing-name":
          delete delivered.name;
          break;
        case "missing-version":
          delete delivered.version;
          break;
        case "missing-extensions":
          delete delivered.extensions;
          break;
        case "missing-load-scripts":
          delete delivered.loadScripts;
          break;
        case "empty-name":
          delivered.name = "";
          break;
        case "non-string-name":
          delivered.name = 42;
          break;
        case "non-string-version":
          delivered.version = 42;
          break;
        case "non-array-load-scripts":
          delivered.loadScripts = "remoteEntry.cjs";
          break;
        case "non-string-load-script":
          delivered.loadScripts = ["remoteEntry.cjs", 42];
          break;
        case "missing-remote-entry":
          delivered.loadScripts = ["other-asset.cjs"];
          break;
        default:
          break;
      }
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(delivered));
      return;
    }

    if (pathname.startsWith("/plugins/")) {
      if (
        serverScenario === "retry-readiness" &&
        pathname.endsWith("/remoteEntry.cjs")
      ) {
        retryEntryRequests += 1;
        if (retryEntryRequests === 1) {
          record.status = 503;
          response.writeHead(503).end("retry fixture failure");
          return;
        }
      }
      if (
        serverScenario === "missing-entry-asset" &&
        pathname.endsWith("/remoteEntry.cjs")
      ) {
        record.status = 404;
        response.writeHead(404).end("missing remote entry fixture");
        return;
      }

      try {
        const asset = path.basename(pathname);
        const body = await readFile(path.join(remotePath, asset));
        response.writeHead(200, { "content-type": "application/javascript" });
        response.end(body);
      } catch {
        record.status = 404;
        response.writeHead(404).end("asset not found");
      }
      return;
    }

    record.status = 404;
    response.writeHead(404).end("not found");
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  assert.ok(address && typeof address === "object");
  return `http://127.0.0.1:${address.port}`;
}

async function runHost(baseURL, scenario) {
  serverScenario = scenario.serverScenario ?? scenario.mode;
  retryEntryRequests = 0;
  const requestStart = requests.length;
  const manifestURL = `${baseURL}/plugins/remote-manifest.json?case=${scenario.manifestCase ?? "valid"}`;
  let result;
  try {
    const child = await execFileAsync(
      process.execPath,
      [path.join(hostPath, "host.cjs")],
      {
        cwd: cliRoot,
        timeout: 30000,
        maxBuffer: 2 * 1024 * 1024,
        env: {
          ...process.env,
          FIXTURE_MANIFEST_URL: manifestURL,
          FIXTURE_MODE: scenario.mode,
          NODE_PATH: path.resolve(cliRoot, "../../node_modules"),
          NO_COLOR: "1",
          FORCE_COLOR: "0",
        },
      },
    );
    result = child.stdout.match(/^FLEETCTL_NODE_E2E_RESULT:(.+)$/m)?.[1];
    assert.ok(
      result,
      `Host emitted no result. stdout=${child.stdout}\nstderr=${child.stderr}`,
    );
    result = JSON.parse(result);
  } catch (error) {
    const details =
      error?.stdout || error?.stderr
        ? `\nstdout:\n${error.stdout}\nstderr:\n${error.stderr}`
        : "";
    throw new Error(
      `Scenario ${scenario.mode} failed: ${error.message}${details}`,
      { cause: error },
    );
  }
  return { result, requests: requests.slice(requestStart) };
}

function assertLoaderError(result, stage, expectedMessage) {
  const event = result.loaderEvents.find(
    (item) => item.stage === stage && item.success === false,
  );
  assert.ok(event, `Expected a failed ${stage} loader event`);
  assert.match(event.errorMessage, expectedMessage);
  return event;
}

async function closeServer() {
  if (!server?.listening) return;
  await new Promise((resolve, reject) => {
    server.close((error) => (error ? reject(error) : resolve()));
    server.closeAllConnections();
  });
}

try {
  const manifest = await buildRemote();
  await buildHost();
  const baseURL = await startServer(manifest);

  const success = await runHost(baseURL, { mode: "success" });
  assert.equal(success.result.remoteLabel, "Node federation command");
  assert.match(success.result.rendered, /Remote command: ready/);
  assert.equal(success.result.counters.manifestTransforms, 1);
  assert.equal(success.result.counters.registerRemotes, 1);
  assert.ok(success.result.counters.readinessAcquisitions >= 1);
  assert.ok(success.result.counters.moduleInits >= 1);
  assert.ok(success.result.counters.loadRemote >= 2);
  assert.equal(
    success.result.transformedEntry,
    `${baseURL}/plugins/${manifest.remoteEntry}`,
  );
  assert.ok(
    success.requests.some(
      (item) => item.path === "/plugins/remote-manifest.json",
    ),
  );
  assert.ok(
    success.requests.some(
      (item) => item.path === `/plugins/${manifest.remoteEntry}`,
    ),
  );

  for (const manifestCase of [
    "missing-name",
    "missing-version",
    "missing-extensions",
    "missing-load-scripts",
    "empty-name",
    "non-string-name",
    "non-string-version",
    "non-array-load-scripts",
    "non-string-load-script",
  ]) {
    const failure = await runHost(baseURL, {
      mode: "invalid-manifest",
      manifestCase,
    });
    assertLoaderError(
      failure.result,
      "manifest",
      /Invalid remote plugin manifest/,
    );
    assert.match(
      failure.result.expectedFailure,
      /Invalid remote plugin manifest/,
    );
    assert.ok(
      failure.requests.some(
        (item) => item.path === "/plugins/remote-manifest.json",
      ),
    );
  }

  const missingEntry = await runHost(baseURL, {
    mode: "missing-remote-entry",
    manifestCase: "missing-remote-entry",
  });
  assertLoaderError(
    missingEntry.result,
    "plugin",
    new RegExp(
      `^Failed to load plugin ${remoteName}: Plugin "${remoteName}" has no declared remoteEntry`,
    ),
  );
  assert.equal(missingEntry.result.counters.registerRemotes, 0);

  const missingAsset = await runHost(baseURL, {
    mode: "missing-entry-asset",
    serverScenario: "missing-entry-asset",
  });
  const assetError = assertLoaderError(
    missingAsset.result,
    "plugin",
    new RegExp(`^Failed to load plugin ${remoteName}:`),
  );
  assert.match(assetError.errorMessage, /remoteEntry\.cjs/);
  assert.ok(
    missingAsset.requests.some(
      (item) =>
        item.path === `/plugins/${manifest.remoteEntry}` && item.status === 404,
    ),
  );

  const retry = await runHost(baseURL, { mode: "retry-readiness" });
  assert.ok(retry.result.firstFailure);
  assert.match(retry.result.rendered, /Remote command: ready/);
  const retryFailures = retry.result.loaderEvents.filter(
    (item) => item.stage === "plugin" && !item.success,
  );
  assert.ok(retryFailures.length >= 1, "First readiness attempt did not fail");
  assert.match(retryFailures[0].errorMessage, /remoteEntry\.cjs/);
  const firstFailureIndex = retry.result.loaderEvents.findIndex(
    (item) => item.stage === "plugin" && !item.success,
  );
  assert.ok(
    retry.result.loaderEvents.some(
      (item, index) =>
        index > firstFailureIndex && item.stage === "plugin" && item.success,
    ),
    "A later load in the same host did not recover successfully",
  );
  assert.ok(
    retry.result.counters.registerRemotes >= 2,
    "Retry did not register remote again in the same host",
  );
  assert.ok(
    retry.result.counters.readinessAcquisitions >= 2,
    "Retry did not acquire readiness again in the same host",
  );
  assert.ok(retry.result.counters.moduleInits >= 1);
  const firstFailedEntryRequest = retry.requests.findIndex(
    (item) =>
      item.path === `/plugins/${manifest.remoteEntry}` && item.status === 503,
  );
  const recoveredEntryRequest = retry.requests.findIndex(
    (item, index) =>
      index > firstFailedEntryRequest &&
      item.path === `/plugins/${manifest.remoteEntry}` &&
      item.status === 200,
  );
  assert.ok(firstFailedEntryRequest >= 0, "Initial entry request did not fail");
  assert.ok(recoveredEntryRequest > firstFailedEntryRequest);

  for (const kind of ["null", "undefined"]) {
    const nullish = await runHost(baseURL, { mode: `${kind}-module` });
    assert.match(
      nullish.result.expectedFailure,
      new RegExp(`Failed to load module 'command' of plugin ${remoteName}`),
    );
    assertLoaderError(
      nullish.result,
      "exposed-module",
      new RegExp(
        `Enhanced runtime returned no module for plugin "${remoteName}" request "command"`,
      ),
    );
    assert.ok(
      nullish.result.remoteModuleResults.some(
        (item) => item.id === `${remoteName}/command` && item.kind === kind,
      ),
      `Expected Enhanced runtime ${kind} module result`,
    );
    assert.ok(nullish.result.counters.moduleInits >= 1);
    assert.ok(nullish.result.counters.loadRemote >= 1);
    assert.ok(
      nullish.requests.some(
        (item) =>
          item.path === `/plugins/${manifest.remoteEntry}` &&
          item.status === 200,
      ),
    );
  }

  console.log(
    `Isolated CLI federation shell e2e passed: NodeRemotePlugin producer, real Scalprum/Enhanced loading, manifest transform, Ink render, failure/retry cases; HTTP requests ${requests.length}`,
  );
} finally {
  await closeServer();
  await rm(scratch, { recursive: true, force: true });
}
