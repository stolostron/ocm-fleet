#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import {
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readlink,
  readdir,
  realpath,
  rename,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import { fs } from "zx";

import { prepareLinuxCompiler } from "./prepare-linux-toolchain.mjs";

const projectRoot = fileURLToPath(new URL("../", import.meta.url));
const repository = "https://github.com/PerryTS/perry.git";
const owner = "aio-installation-check native toolchain v1";

export function createPerryBuildEnvironment(
  baseEnv,
  workspaceRoot,
  rustToolchain,
) {
  const managed = path.dirname(workspaceRoot);
  const cargoHome = path.join(managed, "cargo");
  const env = {
    ...baseEnv,
    CARGO_HOME: cargoHome,
    RUSTUP_HOME: path.join(managed, "rustup"),
    CARGO_TARGET_DIR: path.join(workspaceRoot, "target"),
    RUSTUP_TOOLCHAIN: rustToolchain,
    PERRY_WORKSPACE_ROOT: workspaceRoot,
    PERRY_NO_TELEMETRY: "1",
    PATH: [path.join(cargoHome, "bin"), baseEnv.PATH]
      .filter(Boolean)
      .join(path.delimiter),
  };
  if (!rustToolchain) delete env.RUSTUP_TOOLCHAIN;
  // Presence, even with an empty value, disables Perry's coherent native rebuild.
  delete env.PERRY_NO_AUTO_OPTIMIZE;
  delete env.PERRY_BUILD_COMMIT;
  // Git must describe this checkout, never a caller's repository/index/config.
  for (const key of Object.keys(env)) {
    if (key.startsWith("GIT_")) delete env[key];
  }
  return env;
}

export function runCommand(command, args, options) {
  const result = spawnSync(command, args, {
    ...options,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  });
  if (result.error || result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed: ${result.error?.message || `exit ${result.status}, signal ${result.signal || "none"}`}`,
    );
  }
  return result.stdout;
}

async function exists(file) {
  try {
    return await lstat(file);
  } catch (error) {
    if (error.code === "ENOENT") return undefined;
    throw error;
  }
}

async function rejectSymlink(directory) {
  const info = await exists(directory);
  if (info && (info.isSymbolicLink() || !info.isDirectory())) {
    throw new Error(
      `Managed directory must not be a symlink or file: ${directory}`,
    );
  }
}

async function claimManagedDirectory(directory) {
  await rejectSymlink(directory);
  await mkdir(directory, { recursive: true });
  const marker = path.join(directory, ".managed");
  if (!(await exists(marker)) && (await readdir(directory)).length === 0) {
    await writeFile(marker, owner, { flag: "wx" });
  }
  if (!(await exists(marker)) || (await readFile(marker, "utf8")) !== owner) {
    throw new Error(
      `Refusing unmanaged directory: ${directory}. Move it aside, then retry.`,
    );
  }
  for (const child of [
    "perry",
    "cargo",
    "rustup",
    "build",
    "compiler",
    "llvm",
  ]) {
    await rejectSymlink(path.join(directory, child));
  }
}

async function validateNativeTargetLink(source) {
  const target = path.join(source, "target");
  const build = path.join(path.dirname(source), "build");
  const info = await exists(target);
  if (!info?.isSymbolicLink()) return false;
  if (
    path.toNamespacedPath(path.resolve(source, await readlink(target))) !==
    path.toNamespacedPath(build)
  ) {
    throw new Error(`Unexpected native target symlink: ${target}`);
  }
  await rejectSymlink(build);
  return true;
}

async function prepareNativeBuildDirectory(source, managed) {
  const target = path.join(source, "target");
  const build = path.join(managed, "build");
  const info = await exists(target);
  const managedLink = await validateNativeTargetLink(source);
  if (info && !managedLink) {
    if (!info.isDirectory() || (await exists(build))) {
      throw new Error(
        `Cannot move native target to ${build}; preserve both directories and move the conflicting path aside before retrying.`,
      );
    }
    // Preserve existing warm artifacts with an atomic rename, never a delete.
    await rename(target, build);
  }
  await mkdir(build, { recursive: true });
  if (!info?.isSymbolicLink()) {
    await symlink(
      process.platform === "win32" ? build : "../build",
      target,
      process.platform === "win32" ? "junction" : "dir",
    );
  }
  // Cache the checkout whole (including .git), but only a pointer to artifacts.
  // Nx preserves this link rather than copying its target. Restoring source
  // can replace the link safely while leaving the sibling build/ tree intact.
  // Keep CARGO_TARGET_DIR at source/target so existing Cargo paths stay stable.
}

async function readPin(root) {
  const manifest = JSON.parse(
    await readFile(path.join(root, "package.json"), "utf8"),
  );
  const version = manifest.devDependencies?.["@perryts/perry"];
  if (typeof version !== "string" || !/^\d+\.\d+\.\d+$/.test(version)) {
    throw new Error(
      "@perryts/perry must have an exact version pin in package.json.",
    );
  }
  return version;
}

export async function validatePerrySource(root, version, run, env) {
  const options = { cwd: root, env };
  if (
    (await run("git", ["remote", "get-url", "origin"], options)).trim() !==
    repository
  ) {
    throw new Error(
      `Perry checkout has unexpected origin: ${root}. Move it aside, then retry.`,
    );
  }
  if (
    (
      await run("git", ["describe", "--tags", "--exact-match", "HEAD"], options)
    ).trim() !== `v${version}`
  ) {
    throw new Error(
      `Perry checkout must be exactly v${version}: ${root}. Move it aside, then retry.`,
    );
  }
  const managedLink = await validateNativeTargetLink(root);
  const status = await run(
    "git",
    ["status", "--porcelain", "--untracked-files=normal"],
    options,
  );
  // Upstream ignores target/ directories, not symlinks. Only our validated
  // root link may be untracked; retain every other tracked/untracked change.
  if (
    status
      .split(/\r?\n/)
      .some((line) => line.trim() && !(managedLink && line === "?? target"))
  ) {
    throw new Error(
      `Perry checkout has modified or untracked source files: ${root}. Restore them or move it aside, then retry.`,
    );
  }
  for (const file of [
    "Cargo.toml",
    "Cargo.lock",
    "crates/perry-runtime-static/Cargo.toml",
    "crates/perry-stdlib-static/Cargo.toml",
    "crates/perry-ext-net/Cargo.toml",
    "crates/perry-ext-ws/Cargo.toml",
    "crates/perry-ext-events/Cargo.toml",
  ]) {
    if (!(await stat(path.join(root, file))).isFile()) {
      throw new Error(`Perry checkout is incomplete: ${file} in ${root}`);
    }
  }
  const tools = JSON.parse(
    await readFile(path.join(root, "external-tools.json"), "utf8"),
  );
  const rustToolchain = tools.tools?.rust?.version;
  if (
    typeof rustToolchain !== "string" ||
    !/^nightly-\d{4}-\d{2}-\d{2}$/.test(rustToolchain)
  ) {
    throw new Error(
      `Perry external-tools.json must declare pinned tools.rust.version: ${root}`,
    );
  }
  return rustToolchain;
}

async function downloadInstaller(url, destination) {
  const response = await fetch(url, { signal: AbortSignal.timeout(120_000) });
  if (!response.ok)
    throw new Error(`Download ${url} failed: HTTP ${response.status}`);
  await fs.writeFile(destination, Buffer.from(await response.arrayBuffer()), {
    mode: 0o700,
  });
}

function rustHost(platform, arch) {
  const cpu = { x64: "x86_64", arm64: "aarch64" }[arch];
  const system = {
    darwin: "apple-darwin",
    linux: "unknown-linux-gnu",
    win32: "pc-windows-msvc",
  }[platform];
  if (!cpu || !system)
    throw new Error(
      `Native Rust bootstrap does not support ${platform}/${arch}.`,
    );
  return `${cpu}-${system}`;
}

export async function prepareNativeToolchain(
  root = projectRoot,
  {
    env: baseEnv = process.env,
    run = runCommand,
    download = downloadInstaller,
    platform = process.platform,
    arch = process.arch,
    libc = process.report.getReport().header.glibcVersionRuntime,
    prepareLinux = prepareLinuxCompiler,
  } = {},
) {
  root = await realpath(root);
  const version = await readPin(root);
  const host = rustHost(platform, arch);
  if (platform === "linux" && !libc)
    throw new Error(
      "Linux musl is unsupported; use a glibc host with a C compiler/linker and development SDK.",
    );
  const managed = path.join(root, ".native-toolchain");
  const source = path.join(managed, "perry");
  let env = createPerryBuildEnvironment(baseEnv, source);
  await claimManagedDirectory(managed);
  try {
    if (!(await exists(source))) {
      // Clone atomically: interrupted downloads never become reusable checkouts.
      // Only this invocation's temporary directory is removed, never user paths.
      const temporary = await mkdtemp(path.join(managed, "clone-"));
      try {
        const checkout = path.join(temporary, "source");
        await run(
          "git",
          [
            "clone",
            "--depth",
            "1",
            "--branch",
            `v${version}`,
            "--single-branch",
            repository,
            checkout,
          ],
          { cwd: root, env },
        );
        await validatePerrySource(checkout, version, run, env);
        await rename(checkout, source);
      } finally {
        await rm(temporary, { recursive: true, force: true });
      }
    }
    const rustToolchain = await validatePerrySource(source, version, run, env);
    await prepareNativeBuildDirectory(source, managed);
    env = createPerryBuildEnvironment(baseEnv, source, rustToolchain);
    const suffix = platform === "win32" ? ".exe" : "";
    const rustup = path.join(env.CARGO_HOME, "bin", `rustup${suffix}`);
    if (!(await exists(rustup))) {
      const temporary = await mkdtemp(path.join(managed, "rustup-init-"));
      try {
        const installer = path.join(temporary, `rustup-init${suffix}`);
        await download(
          `https://static.rust-lang.org/rustup/dist/${host}/rustup-init${suffix}`,
          installer,
        );
        await chmod(installer, 0o700);
        await run(
          installer,
          [
            "-y",
            "--no-modify-path",
            "--profile",
            "minimal",
            "--default-toolchain",
            "none",
          ],
          { cwd: root, env },
        );
      } finally {
        await rm(temporary, { recursive: true, force: true });
      }
    }
    const verify = async () => {
      for (const tool of ["rustc", "cargo"]) {
        await run(rustup, ["run", rustToolchain, tool, "--version"], {
          cwd: source,
          env,
        });
      }
    };
    try {
      await verify();
    } catch {
      await run(
        rustup,
        ["toolchain", "install", rustToolchain, "--profile", "minimal"],
        { cwd: source, env },
      );
      await verify();
    }
    // Linux npm binaries have a different runtime source stamp than the clean
    // release tag. Build the compiler from exactly the checkout used by its
    // auto-built runtime/stdlib/net/ws/events archives. Other hosts keep npm.
    const linux =
      platform === "linux"
        ? await prepareLinux({
            managed,
            source,
            rustToolchain,
            version,
            env,
            run,
            arch,
            libc,
            validateSource: () =>
              validatePerrySource(source, version, run, env),
          })
        : {};
    return { root: source, rustToolchain, env, version, ...linux };
  } catch (error) {
    throw new Error(
      `Native toolchain preparation failed: ${error.message}\nCheck network access and Git/native build prerequisites, then rerun npx nx run aio-installation-check:prepare-native-toolchain. Managed files: ${managed}`,
      { cause: error },
    );
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const prepared = await prepareNativeToolchain();
  console.log(
    `Prepared Perry v${prepared.version}: ${prepared.root}\nRust: ${prepared.rustToolchain}`,
  );
}
