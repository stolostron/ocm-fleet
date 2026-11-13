#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const projectRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const defaultBinary = path.join(
  projectRoot,
  "dist/native/aio-installation-check",
);
const defaultEvidence = path.join(projectRoot, "dist/native-smoke-evidence");

// Isolated child environment for the executable: a private, empty PATH
// directory guarantees native startup without Node.js and runtime discovery
// that cannot depend on any runner-installed engine or service. This smoke
// validates startup and discovery only, not full AIO readiness or launch.
export async function createIsolatedEnvironment(inherited = process.env) {
  const directory = await mkdtemp(path.join(os.tmpdir(), "aio-native-smoke-"));
  return {
    environment: { ...inherited, PATH: directory, NO_COLOR: "1" },
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}

// Rendered Ink output, not the pre-render message strings: the banner style
// discards its message text, so assert on what the executable actually emits.
// The final marker is the explicit no-clients guidance the executable prints
// when no runtime client resolves from the isolated PATH.
export const REQUIRED_MARKERS = [
  "OCM Fleet",
  "AIO installation check",
  "Discovering local container runtimes",
  "Podman:",
  "Docker:",
  "No usable Podman or Docker client detected",
];

export function elfArchToken(arch) {
  if (arch === "x86_64" || arch === "amd64") return "x86-64";
  if (arch === "aarch64" || arch === "arm64") return "aarch64";
  throw new Error(`unsupported host architecture: ${arch}`);
}

function spawn(command, args, options) {
  return spawnSync(command, args, { encoding: "utf8", ...options });
}

// Host inspection runs with the normal inherited PATH.
function hostArch(spawnChild) {
  const result = spawnChild("uname", ["-m"], {});
  if (result.status !== 0) throw new Error(`uname -m failed: ${result.stderr}`);
  return result.stdout.trim();
}

// Runs the built executable with no input in a Node-free, client-free
// environment on the build host and verifies real output: branding, runtime
// discovery labels for both engines, and the explicit no-clients guidance.
// Empty stdin keeps the run deterministic: any unexpected prompt would hit
// EOF and fail the smoke instead of choosing or approving anything.
export async function runNativeSmoke({
  binaryPath = defaultBinary,
  createEnvironment = createIsolatedEnvironment,
  arch,
  spawnChild = spawn,
  exists = existsSync,
  timeoutMs = 120_000,
} = {}) {
  if (!exists(binaryPath)) {
    throw new Error(
      `Native executable not found: ${binaryPath}. Run "npx nx run aio-installation-check:build-native" first.`,
    );
  }

  const { environment, cleanup } = await createEnvironment();
  try {
    const probe = spawnChild("/bin/sh", ["-c", "command -v node"], {
      env: environment,
    });
    if (probe.status === 0) {
      throw new Error(
        `Node.js is reachable in the isolated environment via ${probe.stdout.trim()}; the executable must run without Node.js.`,
      );
    }

    const host = arch ?? hostArch(spawnChild);
    const token = elfArchToken(host);
    const description = spawnChild("file", ["-b", binaryPath]);
    if (description.status !== 0) {
      throw new Error(`file(1) failed: ${description.stderr}`);
    }
    if (!/ELF 64-bit LSB/.test(description.stdout)) {
      throw new Error(
        `Expected a 64-bit LSB ELF executable, got: ${description.stdout.trim()}`,
      );
    }
    if (!description.stdout.includes(token)) {
      throw new Error(
        `Executable architecture does not match the build host (${host}); file reports: ${description.stdout.trim()}`,
      );
    }

    const run = spawnChild(binaryPath, [], {
      env: environment,
      input: "",
      timeout: timeoutMs,
      killSignal: "SIGKILL",
    });
    const output = `${run.stdout}\n${run.stderr}`;
    if (run.status !== 0) {
      throw new Error(
        `Native executable exited with status ${run.status}:\n${output}`,
      );
    }
    for (const marker of REQUIRED_MARKERS) {
      if (!output.includes(marker)) {
        throw new Error(
          `Native smoke output is missing "${marker}":\n${output}`,
        );
      }
    }
    return {
      arch: host,
      elf: description.stdout.trim(),
      exitCode: run.status,
      markers: REQUIRED_MARKERS,
      binary: binaryPath,
      output,
    };
  } finally {
    await cleanup();
  }
}

async function main() {
  const smoke = await runNativeSmoke();
  await mkdir(defaultEvidence, { recursive: true });
  await writeFile(path.join(defaultEvidence, "smoke-output.txt"), smoke.output);
  const summary = {
    smoke: {
      binary: smoke.binary,
      arch: smoke.arch,
      elf: smoke.elf,
      exitCode: smoke.exitCode,
      markers: smoke.markers,
    },
  };
  await writeFile(
    path.join(defaultEvidence, "results.json"),
    `${JSON.stringify(summary, null, 2)}\n`,
  );
  console.log(JSON.stringify(summary, null, 2));
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  await main();
}
