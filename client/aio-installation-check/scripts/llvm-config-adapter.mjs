#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { fs } from "zx";

// Replace complete library tokens only; retain spacing, quoting of unrelated
// paths, and all other llvm-config output. No shell evaluates this output.
export function normalizeSystemLibraries(output, remapping) {
  const replacements = new Map(
    remapping.map(({ original, name, file }) => [
      original,
      // llvm-sys requests dylib system dependencies even with static LLVM.
      // GNU ld's exact-filename syntax permits a validated static-only fallback.
      file.endsWith(".a") ? `-l:${path.basename(file)}` : `-l${name}`,
    ]),
  );
  return output.replace(/(?:[^\s"']+|"[^"]*"|'[^']*')+/g, (token) => {
    const unquoted = token.replace(/^(?:"(.*)"|'(.*)')$/, "$1$2");
    return replacements.get(unquoted) ?? token;
  });
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const directory = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
  const config = await fs.readJson(path.join(directory, "config.json"));
  const args = process.argv.slice(2);
  const result = spawnSync(config.config, args, {
    env: process.env,
    stdio: ["inherit", "pipe", "inherit"],
  });
  if (result.error) {
    console.error(`llvm-config adapter: ${result.error.message}`);
    process.exitCode = 1;
  } else {
    process.stdout.write(
      args.includes("--system-libs") && result.status === 0
        ? normalizeSystemLibraries(
            result.stdout.toString("utf8"),
            config.remapping,
          )
        : result.stdout,
    );
    if (result.signal) process.kill(process.pid, result.signal);
    else process.exitCode = result.status;
  }
}
