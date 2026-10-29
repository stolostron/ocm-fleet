import { execFileSync } from "node:child_process";
import {
  cp,
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const moduleRoot = fileURLToPath(new URL("./golangci-lint/", import.meta.url));
const binaryPath = fileURLToPath(
  new URL("../bin/tools/golangci-lint", import.meta.url),
);
const moduleFiles = ["go.mod", "go.sum"];

// The module's toolchain directive is an exact build pin here, rather than Go's
// usual minimum preference. Application modules keep their own Go environments.
export async function toolEnvironment(directory = moduleRoot) {
  const source = await readFile(join(directory, "go.mod"), "utf8");
  const pin = source.match(/^toolchain (go\d+\.\d+\.\d+)$/m)?.[1];
  if (!pin)
    throw new Error("The linter module needs an exact toolchain goX.Y.Z pin");
  const env = {
    ...process.env,
    GOTOOLCHAIN: pin,
    GOWORK: "off",
    GOFLAGS: "",
    GOEXPERIMENT: "",
    CGO_ENABLED: "0",
  };
  const host = JSON.parse(
    execFileSync("go", ["env", "-json", "GOHOSTOS", "GOHOSTARCH"], { env }),
  );
  return { ...env, GOOS: host.GOHOSTOS, GOARCH: host.GOHOSTARCH };
}

// Stage module files and the executable so failed builds preserve the installed
// tool and ordinary builds cannot silently add unreviewed dependency checksums.
export async function buildGolangciLint({
  directory = moduleRoot,
  output = binaryPath,
} = {}) {
  const env = await toolEnvironment(directory);
  await mkdir(dirname(output), { recursive: true });
  const staging = await mkdtemp(join(dirname(output), ".golangci-lint-build-"));
  try {
    for (const name of moduleFiles)
      await cp(join(directory, name), join(staging, name));
    runGo(
      [
        "build",
        "-mod=readonly",
        `-modfile=${join(staging, "go.mod")}`,
        "-trimpath",
        "-buildvcs=false",
        "-o",
        join(staging, "golangci-lint"),
        ".",
      ],
      directory,
      env,
    );
    for (const name of moduleFiles) {
      if (
        (await readFile(join(directory, name), "utf8")) !==
        (await readFile(join(staging, name), "utf8"))
      ) {
        throw new Error(
          `${name} changed during the build; update and review the tool dependencies first`,
        );
      }
    }
    await rename(join(staging, "golangci-lint"), output);
  } finally {
    await rm(staging, { recursive: true, force: true });
  }
}

// Resolve an explicit release in a disposable copy. Failed downloads or module
// resolution leave the checked-in pins untouched; successful changes are reviewed
// as the standard go.mod/go.sum diff, with no separate platform hash manifest.
export async function updateGolangciLint({
  version,
  directory = moduleRoot,
} = {}) {
  if (
    typeof version !== "string" ||
    !/^v2\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)
  ) {
    throw new Error("Provide a stable v2 version with --release=v2.X.Y");
  }
  const env = await toolEnvironment(directory);
  const original = await Promise.all(
    moduleFiles.map((name) => readFile(join(directory, name))),
  );
  const staging = await mkdtemp(join(tmpdir(), "ocm-fleet-linter-update-"));
  try {
    await cp(directory, staging, { recursive: true });
    runGo(
      ["get", `github.com/golangci/golangci-lint/v2@${version}`],
      staging,
      env,
    );
    runGo(["mod", "tidy"], staging, env);
    for (const [index, name] of moduleFiles.entries()) {
      if (!(await readFile(join(directory, name))).equals(original[index])) {
        throw new Error(
          "Tool dependencies changed during the update; review that edit and retry",
        );
      }
    }
    for (const name of moduleFiles)
      await writeFile(
        join(directory, name),
        await readFile(join(staging, name)),
      );
  } finally {
    await rm(staging, { recursive: true, force: true });
  }
}

function runGo(args, directory, env) {
  execFileSync("go", args, { cwd: directory, env, stdio: "inherit" });
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const { values, positionals } = parseArgs({
      allowPositionals: true,
      options: { release: { type: "string" } },
    });
    switch (positionals[0]) {
      case "build":
        await buildGolangciLint();
        break;
      case "update":
        await updateGolangciLint({ version: values.release });
        console.log(
          "Review the tool module diff and upstream release notes; run the tool and config tests before committing.",
        );
        break;
      case "env": {
        const env = await toolEnvironment();
        process.stdout.write(
          execFileSync(
            "go",
            [
              "env",
              "-json",
              "GOVERSION",
              "GOOS",
              "GOARCH",
              "CGO_ENABLED",
              "GOFLAGS",
              "GOEXPERIMENT",
              "GOAMD64",
              "GOARM64",
            ],
            { env },
          ),
        );
        break;
      }
      case "test":
        runGo(
          ["test", "-mod=readonly", "-count=1", "./..."],
          moduleRoot,
          await toolEnvironment(),
        );
        break;
      default:
        throw new Error("Expected build, update, env, or test");
    }
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
