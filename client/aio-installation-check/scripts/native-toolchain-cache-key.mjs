#!/usr/bin/env node
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { fs } from "zx";

import {
  compilerEnvironmentIdentity,
  discoverLLVM,
} from "./prepare-linux-toolchain.mjs";
import { runCommand } from "./prepare-native-toolchain.mjs";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));

export async function nativeToolchainCacheKey(
  directory = projectRoot,
  { env = process.env, platform = process.platform, run = runCommand } = {},
) {
  const root = await fs.realpath(directory);
  const manifest = await fs.readJson(path.join(root, "package.json"));
  const key = {
    perry: manifest.devDependencies["@perryts/perry"],
    platform: process.platform,
    arch: process.arch,
    os: os.release(),
    libc: process.report.getReport().header.glibcVersionRuntime ?? null,
    // Rustup metadata and proxies are location-bound; never share across roots.
    root,
  };
  if (platform === "linux") {
    key.compilerEnvironment = compilerEnvironmentIdentity(env);
    try {
      key.llvm = (await discoverLLVM(env, run))?.identity ?? null;
    } catch (error) {
      key.llvm = { invalid: error.message };
    }
  }
  return key;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  console.log(JSON.stringify(await nativeToolchainCacheKey()));
}
