import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const modules = {
  server: "server",
  "deploy-aio": "deploy/aio",
  "nx-cache-proxy": "sdk/nx-cache-proxy",
  "ocm-fleet": "tools/golangci-lint",
};

// These are existing Nx targets: application tests keep their usual environments
// and the root project runs the isolated tooling's own checks.
const checkTargets = [
  "lint",
  "test",
  "lint:golangci-lint",
  "test:golangci-lint-setup",
  "test:golangci-lint-analyzers",
  "test:golangci-lint-config",
  "test:go-workflow",
];

export async function runGoWorkflow(
  action,
  { root = process.cwd(), projects = Object.keys(modules) } = {},
) {
  if (!["fmt", "verify", "fix"].includes(action)) {
    throw new Error("Expected fmt, verify, or fix");
  }
  const selected = [...new Set(projects)];
  if (!selected.length) throw new Error("Select at least one Go project");
  // Validate the whole selection before changing any files.
  for (const project of selected) {
    if (!Object.hasOwn(modules, project)) {
      throw new Error(`Unknown Go project: ${project}`);
    }
  }

  const binary = join(root, "bin/tools/golangci-lint");
  const config = join(root, ".golangci.yaml");
  for (const project of selected) {
    const cwd = join(root, modules[project]);
    console.log(`Formatting ${project}...`);
    // Remove stale imports before type checking: run --fix cannot load a package
    // whose unused imports prevent it from compiling.
    await run(binary, ["fmt", "--config", config, "./..."], { cwd });
    if (action === "fix") {
      console.log(`Fixing ${project}...`);
      await run(
        binary,
        [
          "run",
          "--fix",
          "--allow-serial-runners",
          "--path-mode=abs",
          "--config",
          config,
          "./...",
        ],
        {
          cwd,
          // A fix pass may leave findings it cannot repair. Still run the fresh
          // checks and tests below; tool failures other than findings stop here.
          allowFindings: true,
        },
      );
    }
  }
  if (action === "fmt") return;

  // Start Nx after all rewrites, so it hashes the final source rather than the
  // inputs of the enclosing task. Disabling the daemon also avoids watcher lag.
  const nxDirectory = join(root, "node_modules/nx");
  const { bin } = JSON.parse(
    await readFile(join(nxDirectory, "package.json"), "utf8"),
  );
  await run(
    process.execPath,
    [
      join(nxDirectory, bin.nx),
      "run-many",
      `--projects=${selected.join(",")}`,
      `--targets=${checkTargets.join(",")}`,
      "--nxBail=false",
      "--outputStyle=static",
    ],
    { cwd: root, env: { NX_DAEMON: "false" } },
  );
}

function run(command, args, { cwd, env = {}, allowFindings = false }) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd,
      env: { ...process.env, GOWORK: "off", ...env },
      stdio: "inherit",
    });
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0 || (allowFindings && code === 1)) resolve();
      else
        reject(
          Object.assign(
            new Error(`${command} failed (${signal ?? `exit ${code}`})`),
            { exitCode: code ?? 1 },
          ),
        );
    });
  });
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  try {
    const { positionals, values } = parseArgs({
      allowPositionals: true,
      options: { projects: { type: "string" } },
    });
    if (positionals.length !== 1)
      throw new Error("Expected fmt, verify, or fix");
    await runGoWorkflow(positionals[0], {
      projects:
        values.projects === undefined ? undefined : values.projects.split(","),
    });
  } catch (error) {
    console.error(error.message);
    process.exitCode = error.exitCode ?? 1;
  }
}
