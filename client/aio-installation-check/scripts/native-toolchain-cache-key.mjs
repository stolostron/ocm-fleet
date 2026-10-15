#!/usr/bin/env node
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { fs } from "zx";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));

export async function nativeToolchainCacheKey(directory = projectRoot) {
  const root = await fs.realpath(directory);
  const manifest = await fs.readJson(path.join(root, "package.json"));
  return {
    perry: manifest.devDependencies["@perryts/perry"],
    platform: process.platform,
    arch: process.arch,
    os: os.release(),
    libc: process.report.getReport().header.glibcVersionRuntime ?? null,
    // Rustup metadata and proxies are location-bound; never share across roots.
    root,
  };
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  console.log(JSON.stringify(await nativeToolchainCacheKey()));
}
