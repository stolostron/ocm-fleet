import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import rspack from "@rspack/core";

import { NodeRemotePlugin, identifyRemoteEntry } from "../dist/index.js";

const fixtureDir = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const outputPath = await mkdtemp(
  path.join(os.tmpdir(), "ocm-fleet-node-remote-"),
);

try {
  const compiler = rspack({
    context: fixtureDir,
    mode: "production",
    target: "node",
    entry: "./src/host.js",
    optimization: {
      runtimeChunk: "single",
    },
    output: {
      path: outputPath,
      filename: "[name].cjs",
      chunkFilename: "[name].js",
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
  });

  const stats = await new Promise((resolve, reject) => {
    compiler.run((error, result) => {
      if (error) reject(error);
      else if (result?.hasErrors()) {
        reject(new Error(result.toString({ all: false, errors: true })));
      } else resolve(result);
    });
  });
  await new Promise((resolve) => compiler.close(resolve));
  assert.ok(stats);

  const manifest = JSON.parse(
    await readFile(
      path.join(outputPath, "fixture_remote-manifest.json"),
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
  assert.ok(manifest.loadScripts.length > 1);
  assert.ok(manifest.runtimeDependencies.length > 0);
  assert.notEqual(manifest.loadScripts[0], manifest.remoteEntry);
  assert.ok(manifest.loadScripts.includes(manifest.remoteEntry));

  // Simulate the existing host transform path: it prefixes every loadScripts
  // entry while preserving the producer-declared remote entry identity.
  const transformedScripts = manifest.loadScripts.map(
    (asset) => `/crd/plugins/fixture/${asset}?revision=17`,
  );
  assert.equal(
    identifyRemoteEntry(transformedScripts, manifest.remoteEntry),
    `/crd/plugins/fixture/${manifest.remoteEntry}?revision=17`,
  );

  const remote = require(path.join(outputPath, manifest.remoteEntry));
  assert.equal(typeof remote.init, "function");
  assert.equal(typeof remote.get, "function");
  await remote.init({});
  const moduleFactory = await remote.get("./answer");
  const exposedModule = moduleFactory();
  assert.equal(exposedModule.answer, 42);
  assert.equal(exposedModule.default(), "answer");
  assert.equal(await exposedModule.loadLazyAnswer(), "loaded lazily");
  console.log("Node remote fixture passed");
} finally {
  await rm(outputPath, { recursive: true, force: true });
}
