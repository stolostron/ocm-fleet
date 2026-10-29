import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { NodeRemotePlugin } from "@ocm-fleet/node-plugin";
import rspack from "@rspack/core";

const execFileAsync = promisify(execFile);
const pluginRoot = path.dirname(fileURLToPath(import.meta.url));
const workspaceRoot = path.resolve(pluginRoot, "../../..");
const cliRoot = path.join(workspaceRoot, "client/cli");
const scratch = await mkdtemp(path.join(os.tmpdir(), "fleetctl-plugin-e2e-"));
const remotePath = path.join(scratch, "remote");
const binary = path.join(cliRoot, "bin/fleetctl");
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
    context: workspaceRoot,
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
          exposedModules: {
            command: "./e2e/cli/plugin/remote-command.tsx",
            "command-null": "./e2e/cli/plugin/remote-null.ts",
            "command-undefined": "./e2e/cli/plugin/remote-undefined.ts",
          },
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

async function startServer(manifest) {
  server = createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    const { pathname } = url;
    const record = {
      method: request.method,
      host: request.headers.host,
      path: pathname,
      status: 200,
    };
    requests.push(record);

    if (pathname === `/plugins/${remoteName}-manifest.json`) {
      const manifestCase = url.searchParams.get("case");
      if (manifestCase === "manifest-http-error") {
        record.status = 503;
        response.writeHead(503).end("manifest fixture failure");
        return;
      }
      if (manifestCase === "invalid-json") {
        response.writeHead(200, { "content-type": "application/json" });
        response.end("{not-json");
        return;
      }
      const delivered = structuredClone(manifest);
      switch (manifestCase) {
        case "missing-name":
          delete delivered.name;
          break;
        case "missing-version":
          delete delivered.version;
          break;
        case "missing-base-url":
          delete delivered.baseURL;
          break;
        case "missing-extensions":
          delete delivered.extensions;
          break;
        case "missing-load-scripts":
          delete delivered.loadScripts;
          break;
        case "missing-registration-method":
          delete delivered.registrationMethod;
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
        case "non-array-extensions":
          delivered.extensions = {};
          break;
        case "invalid-extension":
          delivered.extensions = [{ type: "fixture.command", properties: [] }];
          break;
        case "invalid-registration-method":
          delivered.registrationMethod = "automatic";
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

async function runCli(baseURL, options = {}) {
  serverScenario = options.serverScenario ?? "success";
  retryEntryRequests = 0;
  const requestStart = requests.length;
  const manifestCase = options.manifestCase ?? "valid";
  const pluginBaseURL = `${baseURL}/plugins/?case=${manifestCase}`;
  const args = ["plugin", "run", remoteName];
  if (options.moduleName) args.push("--module", options.moduleName);
  if (options.retry) args.push("--retry");

  try {
    const result = await execFileAsync(process.execPath, [binary, ...args], {
      cwd: cliRoot,
      timeout: 30000,
      maxBuffer: 2 * 1024 * 1024,
      env: {
        ...process.env,
        FLEETCTL_PLUGIN_BASE_URL: pluginBaseURL,
        NO_COLOR: "1",
        FORCE_COLOR: "0",
      },
    });
    return {
      ok: true,
      stdout: result.stdout,
      stderr: result.stderr,
      requests: requests.slice(requestStart),
    };
  } catch (error) {
    return {
      ok: false,
      stdout: error.stdout ?? "",
      stderr: error.stderr ?? "",
      requests: requests.slice(requestStart),
    };
  }
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
  const baseURL = await startServer(manifest);

  const success = await runCli(baseURL);
  assert.equal(success.ok, true, success.stderr);
  assert.match(success.stdout, /Remote command: ready/);
  assert.ok(
    success.requests.some(
      (item) => item.path === `/plugins/${remoteName}-manifest.json`,
    ),
    "The production CLI did not fetch the SDK manifest",
  );
  assert.ok(
    success.requests.some(
      (item) =>
        item.path === `/plugins/${manifest.remoteEntry}` &&
        item.host === new URL(baseURL).host &&
        item.status === 200,
    ),
    "The production CLI did not fetch the transformed absolute remote entry",
  );

  for (const manifestCase of [
    "missing-name",
    "missing-version",
    "missing-base-url",
    "missing-extensions",
    "missing-load-scripts",
    "missing-registration-method",
    "empty-name",
    "non-string-name",
    "non-string-version",
    "non-array-load-scripts",
    "non-string-load-script",
    "non-array-extensions",
    "invalid-extension",
    "invalid-registration-method",
  ]) {
    const failure = await runCli(baseURL, { manifestCase });
    assert.equal(failure.ok, false, `Expected ${manifestCase} to fail`);
    assert.match(
      `${failure.stdout}\n${failure.stderr}`,
      /Invalid remote plugin manifest/,
      `Expected ${manifestCase} validation through production getModule`,
    );
    assert.ok(
      failure.requests.some(
        (item) => item.path === `/plugins/${remoteName}-manifest.json`,
      ),
    );
  }

  const missingEntry = await runCli(baseURL, {
    manifestCase: "missing-remote-entry",
  });
  assert.equal(missingEntry.ok, false);
  assert.match(
    `${missingEntry.stdout}\n${missingEntry.stderr}`,
    /no declared remoteEntry/,
  );
  assert.ok(
    missingEntry.requests.some(
      (item) => item.path === `/plugins/${remoteName}-manifest.json`,
    ),
  );

  const manifestHttpError = await runCli(baseURL, {
    manifestCase: "manifest-http-error",
  });
  assert.equal(manifestHttpError.ok, false);
  assert.match(
    `${manifestHttpError.stdout}\n${manifestHttpError.stderr}`,
    /Unable to load plugin manifest/,
  );
  assert.ok(
    manifestHttpError.requests.some(
      (item) =>
        item.path === `/plugins/${remoteName}-manifest.json` &&
        item.status === 503,
    ),
  );

  const invalidManifestJson = await runCli(baseURL, {
    manifestCase: "invalid-json",
  });
  assert.equal(invalidManifestJson.ok, false);
  assert.match(
    `${invalidManifestJson.stdout}\n${invalidManifestJson.stderr}`,
    /Unable to parse plugin manifest/,
  );

  const readinessFailure = await runCli(baseURL, {
    serverScenario: "retry-readiness",
  });
  assert.equal(readinessFailure.ok, false);
  assert.ok(
    readinessFailure.requests.some(
      (item) =>
        item.path === `/plugins/${manifest.remoteEntry}` && item.status === 503,
    ),
  );

  const missingAsset = await runCli(baseURL, {
    serverScenario: "missing-entry-asset",
  });
  assert.equal(missingAsset.ok, false);
  assert.ok(
    missingAsset.requests.some(
      (item) =>
        item.path === `/plugins/${manifest.remoteEntry}` && item.status === 404,
    ),
  );

  const retry = await runCli(baseURL, {
    serverScenario: "retry-readiness",
    retry: true,
  });
  assert.equal(retry.ok, true, retry.stderr);
  assert.match(retry.stdout, /Remote command: ready/);
  const failedEntry = retry.requests.findIndex(
    (item) =>
      item.path === `/plugins/${manifest.remoteEntry}` && item.status === 503,
  );
  const recoveredEntry = retry.requests.findIndex(
    (item, index) =>
      index > failedEntry &&
      item.path === `/plugins/${manifest.remoteEntry}` &&
      item.status === 200,
  );
  assert.ok(failedEntry >= 0, "Initial Enhanced readiness did not fail");
  assert.ok(
    recoveredEntry > failedEntry,
    "getModule did not recover in-process",
  );

  for (const [kind, moduleName] of [
    ["null", "command-null"],
    ["undefined", "command-undefined"],
  ]) {
    const result = await runCli(baseURL, { moduleName });
    assert.equal(result.ok, false);
    assert.match(
      `${result.stdout}\n${result.stderr}`,
      /did not export a command component/,
      `Expected nullish ${kind} export to be rejected`,
    );
    assert.ok(
      result.requests.some(
        (item) =>
          item.path === `/plugins/${manifest.remoteEntry}` &&
          item.status === 200,
      ),
      `Expected ${kind} export to resolve through the remote`,
    );
  }

  console.log(
    `Production fleetctl plugin e2e passed on Node ${process.version}: SDK manifest, absolute asset URL, Enhanced load/init, Ink state, manifest/entry failures, in-process retry, and nullish exports; HTTP requests ${requests.length}`,
  );
} finally {
  await closeServer();
  await rm(scratch, { recursive: true, force: true });
}
