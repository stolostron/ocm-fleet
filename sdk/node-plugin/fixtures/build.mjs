import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { ModuleFederationPlugin } from "@module-federation/enhanced/rspack";
import rspack from "@rspack/core";

import { NodeRemotePlugin, identifyRemoteEntry } from "../dist/index.js";

const fixtureDir = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const outputPath = await mkdtemp(
  path.join(os.tmpdir(), "ocm-fleet-node-remote-"),
);
const remotePath = path.join(outputPath, "remote");
const requests = [];
const server = createServer(async (request, response) => {
  requests.push(request.url);
  try {
    const asset = path.basename(
      new URL(request.url, "http://localhost").pathname,
    );
    response.setHeader("Content-Type", "application/javascript");
    response.end(await readFile(path.join(remotePath, asset)));
  } catch {
    response.writeHead(404).end();
  }
});

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

try {
  const remoteConfig = {
    context: fixtureDir,
    mode: "production",
    target: "async-node",
    entry: {},
    optimization: { minimize: false },
    output: {
      path: remotePath,
      clean: true,
    },
    plugins: [
      new NodeRemotePlugin({
        pluginMetadata: {
          name: "fixture_remote",
          version: "1.0.0",
          exposedModules: { answer: "./src/exposed.js" },
        },
        extensions: [
          {
            type: "example.command",
            properties: { id: "inspect", label: "Inspect" },
          },
        ],
        entryScriptFilename: "remoteEntry.cjs",
      }),
    ],
  };
  await build(remoteConfig);

  const manifest = JSON.parse(
    await readFile(
      path.join(remotePath, "fixture_remote-manifest.json"),
      "utf8",
    ),
  );
  assert.equal(manifest.name, "fixture_remote");
  assert.equal(manifest.remoteEntry, "remoteEntry.cjs");
  assert.equal(manifest.baseURL, "auto");
  assert.deepEqual(manifest.extensions, [
    {
      type: "example.command",
      properties: { id: "inspect", label: "Inspect" },
    },
  ]);
  assert.deepEqual(manifest.loadScripts, [manifest.remoteEntry]);
  assert.deepEqual(manifest.runtimeDependencies, []);

  // Simulate the existing host transform path: it prefixes every loadScripts
  // entry while preserving the producer-declared remote entry identity.
  const transformedScripts = manifest.loadScripts.map(
    (asset) => `/crd/plugins/fixture/${asset}?revision=17`,
  );
  assert.equal(
    identifyRemoteEntry(transformedScripts, manifest.remoteEntry),
    `/crd/plugins/fixture/${manifest.remoteEntry}?revision=17`,
  );

  // Only inspect the local container shape; Enhanced owns HTTP initialization.
  const remote = require(path.join(remotePath, manifest.remoteEntry));
  assert.equal(typeof remote.init, "function");
  assert.equal(typeof remote.get, "function");
  await build({
    context: fixtureDir,
    mode: "production",
    target: "async-node",
    entry: "./src/entry.js",
    optimization: { minimize: false },
    output: {
      path: path.join(outputPath, "host"),
      filename: "host.cjs",
      chunkFilename: "[name].cjs",
      module: false,
    },
    plugins: [
      new ModuleFederationPlugin({
        name: "fixture_host",
        dts: false,
        manifest: false,
        runtimePlugins: [
          require.resolve("@module-federation/node/runtimePlugin"),
        ],
      }),
    ],
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  const { stdout } = await promisify(execFile)(
    process.execPath,
    [path.join(outputPath, "host/host.cjs")],
    {
      env: {
        ...process.env,
        FIXTURE_REMOTE_ENTRY: `http://127.0.0.1:${port}/${manifest.remoteEntry}`,
      },
      timeout: 30000,
    },
  );
  assert.match(stdout, /Enhanced HTTP host passed/);
  assert.equal(requests[0], `/${manifest.remoteEntry}`);
  const chunks = requests.filter((url) => url !== `/${manifest.remoteEntry}`);
  assert.ok(
    chunks.length >= 2,
    `Expected exposed and lazy chunks: ${requests}`,
  );
  assert.ok(chunks.includes("/fixture-lazy.cjs"), `${requests}`);
  assert.ok(
    requests.every((url) => url.endsWith(".cjs")),
    `${requests}`,
  );
  console.log(
    `Node remote HTTP fixture passed; requested ${requests.join(", ")}`,
  );
} finally {
  if (server.listening) {
    await new Promise((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
      server.closeAllConnections();
    });
  }
  await rm(outputPath, { recursive: true, force: true });
}
