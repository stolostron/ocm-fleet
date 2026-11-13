#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { cp, mkdir, realpath, rm } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  prepareNativeToolchain,
  runCommand,
  validatePerrySource,
} from "./prepare-native-toolchain.mjs";

export { createPerryBuildEnvironment } from "./prepare-native-toolchain.mjs";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));

export async function preparePerryBuild(root = projectRoot) {
  root = await realpath(root);
  const require = createRequire(path.join(root, "package.json"));
  const inkRoot = path.dirname(path.dirname(require.resolve("ink")));
  const stage = path.join(root, "dist/perry");
  const stagedModules = path.join(stage, "node_modules");

  // Copy, never symlink: resolvers follow Ink's real path, so a hoisted Ink
  // would still import upstream Yoga even with an AIO-local dependency.
  await mkdir(stagedModules, { recursive: true });
  for (const directory of [
    "src",
    "node_modules/ink",
    "node_modules/yoga-layout",
  ]) {
    await rm(path.join(stage, directory), { recursive: true, force: true });
  }
  await cp(path.join(root, "src"), path.join(stage, "src"), {
    recursive: true,
  });
  await cp(inkRoot, path.join(stagedModules, "ink"), {
    recursive: true,
    filter: (source) => source !== path.join(inkRoot, "node_modules"),
  });
  await cp(
    path.join(root, "src/native/yoga-layout"),
    path.join(stagedModules, "yoga-layout"),
    { recursive: true },
  );

  // Other dependencies resolve upward into the normal npm workspace graph.
  // Fail before compiling if this layout ever stops isolating Ink's Yoga.
  const stagedRequire = createRequire(path.join(stage, "src/main.tsx"));
  const inkEntry = stagedRequire.resolve("ink");
  const yogaEntry = createRequire(inkEntry).resolve("yoga-layout");
  if (
    !inkEntry.startsWith(path.join(stagedModules, "ink") + path.sep) ||
    yogaEntry !== path.join(stagedModules, "yoga-layout/index.ts")
  ) {
    throw new Error("Perry build must resolve staged Ink and AIO-local Yoga");
  }
  console.log(`Perry Ink: ${inkEntry}`);
  console.log(`Perry Yoga: ${yogaEntry}`);
  return path.join(stage, "src/main.tsx");
}

export function perryCompilerInvocation(
  prepared,
  require,
  platform = process.platform,
) {
  if (platform === "linux") {
    if (!prepared.compiler)
      throw new Error("Linux requires a prepared source compiler");
    return { command: prepared.compiler, args: [] };
  }
  return {
    command: process.execPath,
    args: [require.resolve("@perryts/perry/bin/perry.js")],
  };
}

async function build() {
  const prepared = await prepareNativeToolchain();
  const { root: perryRoot, rustToolchain, env: buildEnv, version } = prepared;
  const require = createRequire(path.join(projectRoot, "package.json"));
  if (require("@perryts/perry/package.json").version !== version) {
    throw new Error(
      `Installed Perry does not match ${version}; run npm install.`,
    );
  }
  const compiler = perryCompilerInvocation(prepared, require);

  if (process.env.PERRY_NO_AUTO_OPTIMIZE !== undefined) {
    console.log(
      "Ignoring PERRY_NO_AUTO_OPTIMIZE for this target; AIO needs coherent net/ws extension archives.",
    );
  }
  console.log(`Perry source: ${perryRoot}`);
  console.log(`Rust toolchain: ${rustToolchain}`);
  const entry = await preparePerryBuild();
  await mkdir(path.join(projectRoot, "dist/native"), { recursive: true });
  const result = spawnSync(
    compiler.command,
    [
      ...compiler.args,
      "compile",
      entry,
      "--output",
      path.join(projectRoot, "dist/native/aio-installation-check"),
    ],
    { cwd: projectRoot, env: buildEnv, stdio: "inherit" },
  );
  // Auto-built runtime archives must leave the same source contract intact.
  await validatePerrySource(perryRoot, version, runCommand, buildEnv);
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await build();
}
