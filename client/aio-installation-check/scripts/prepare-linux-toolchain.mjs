#!/usr/bin/env node
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath } from "node:url";

import { fs } from "zx";

const adapterScript = fileURLToPath(
  new URL("./llvm-config-adapter.mjs", import.meta.url),
);

// Release API digests verified with gh api repos/llvm/llvm-project/releases/tags/llvmorg-22.1.8.
const llvmBundles = {
  x64: {
    url: "https://github.com/llvm/llvm-project/releases/download/llvmorg-22.1.8/LLVM-22.1.8-Linux-X64.tar.xz",
    sha256: "df0e1ecf16caf3489a272a5eea4eec9b0d82878f6477fa309504f918a0006384",
  },
  arm64: {
    url: "https://github.com/llvm/llvm-project/releases/download/llvmorg-22.1.8/LLVM-22.1.8-Linux-ARM64.tar.xz",
    sha256: "805efad2bb91cb4967fa569e0881d10c0f69c04461cf671cccbae19f547acc34",
  },
};
const buildArgs = [
  "build",
  "--locked",
  "--profile",
  "perry-dev",
  "-p",
  "perry",
  "--no-default-features",
  "--features",
  "dev-cli",
];

export function compilerEnvironmentIdentity(env) {
  // Record accepted compiler/linker overrides, including Cargo target/profile knobs.
  // The caller first removes source-stamping overrides and sets managed Rust homes.
  return Object.fromEntries(
    Object.entries(env)
      .filter(([key]) =>
        /^(CARGO_(BUILD_|TARGET_|PROFILE_|ENCODED_RUSTFLAGS$|RUSTC|HOME$)|RUST(C|DOC|FLAGS|UP_HOME|UP_TOOLCHAIN)|LLVM_SYS_|LLVM_CONFIG_PATH$|(?:CC|CXX|AR|CFLAGS|CXXFLAGS|CPPFLAGS|LDFLAGS)(?:_|$)|LIBRARY_PATH$|LD_LIBRARY_PATH$|PATH$|PKG_CONFIG)/.test(
          key,
        ),
      )
      .sort(([a], [b]) => a.localeCompare(b)),
  );
}

async function sha256(file) {
  const hash = createHash("sha256");
  for await (const chunk of fs.createReadStream(file)) hash.update(chunk);
  return hash.digest("hex");
}

export async function downloadLLVMArchive(url, destination) {
  // Official archives approach 2 GB. Never materialize them in JS memory.
  const response = await fetch(url, {
    signal: AbortSignal.timeout(60 * 60_000),
  });
  if (!response.ok || !response.body)
    throw new Error(`LLVM download failed: HTTP ${response.status} (${url})`);
  await pipeline(
    Readable.fromWeb(response.body),
    fs.createWriteStream(destination, { flags: "wx" }),
  );
}

async function executable(file) {
  try {
    await fs.access(file, fs.constants.X_OK);
    return (await fs.stat(file)).isFile();
  } catch {
    return false;
  }
}

function llvmEnvironment(env, prefix, libdir = path.join(prefix, "lib")) {
  return {
    ...env,
    LLVM_SYS_221_PREFIX: prefix,
    PATH: [path.join(prefix, "bin"), env.PATH]
      .filter(Boolean)
      .join(path.delimiter),
    LD_LIBRARY_PATH: [libdir, path.join(prefix, "lib64"), env.LD_LIBRARY_PATH]
      .filter(Boolean)
      .join(path.delimiter),
  };
}

class MissingSystemLibraryError extends Error {}

async function regularFile(file) {
  try {
    return (await fs.stat(file)).isFile();
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR") return false;
    throw error;
  }
}

// Read-only and injectable for discovery, cache fingerprinting and host tests.
// Never repair an installed LLVM distribution or a global library directory.
export async function resolveSystemLibraries(output, { env, run }) {
  const remapping = [];
  for (const token of output.match(/(?:[^\s"']+|"[^"]*"|'[^']*')+/g) ?? []) {
    const original = token.replace(/^(?:"(.*)"|'(.*)')$/, "$1$2");
    if (
      !path.isAbsolute(original) ||
      remapping.some((entry) => entry.original === original)
    )
      continue;
    if (await regularFile(original)) continue;
    const match = /^lib([A-Za-z0-9_+.-]+?)(?:\.a|\.so(?:\.\d+)*)$/.exec(
      path.basename(original),
    );
    const name = match?.[1];
    const compiler = env.CC || "cc";
    let file;
    let failure;
    if (name) {
      for (const extension of ["so", "a"]) {
        try {
          const candidate = (
            await run(compiler, [`-print-file-name=lib${name}.${extension}`], {
              env,
            })
          ).trim();
          // GCC echoes the filename when it cannot find it. A directory or a
          // dangling symlink is not proof that the native SDK is installed.
          if (path.isAbsolute(candidate) && (await regularFile(candidate))) {
            file = candidate;
            break;
          }
        } catch (error) {
          failure = error.message;
        }
      }
    }
    if (!file) {
      const packages = {
        zstd: "libzstd-devel",
        z: "zlib-devel",
        xml2: "libxml2-devel",
        tinfo: "ncurses-devel",
        ffi: "libffi-devel",
        edit: "libedit-devel",
      };
      throw new MissingSystemLibraryError(
        `LLVM system library ${original} is missing; ${compiler} could not locate ${name ? `lib${name}.so or lib${name}.a` : "a supported host library"}. Install the native C compiler/linker SDK${packages[name] ? ` (Fedora: dnf install ${packages[name]})` : " and the matching development package"}, then retry.${failure ? ` Compiler search: ${failure}` : ""}`,
      );
    }
    const info = await fs.stat(file);
    remapping.push({
      original,
      name,
      file,
      size: info.size,
      mtimeMs: info.mtimeMs,
    });
  }
  return remapping;
}

export async function inspectLLVM(config, env, run, { archiveSha256 } = {}) {
  // The loader may need this before llvm-config itself can run.
  const initialEnv = llvmEnvironment(env, path.dirname(path.dirname(config)));
  const query = async (...args) =>
    (await run(config, args, { env: initialEnv })).trim();
  const version = await query("--version");
  if (!/^22\./.test(version))
    throw new Error(`LLVM 22 required; ${config} reports ${version}`);
  const prefix = await fs.realpath(await query("--prefix"));
  const libdir = await fs.realpath(await query("--libdir"));
  // llvm-sys 221's default prefers static linking. Merely finding clang or a
  // shared LLVM library is insufficient. Validate every archive llvm-config names.
  const libfiles =
    (await query("--libfiles", "--link-static")).match(
      /(?:[^\s"]+|"[^"]*")+/g,
    ) ?? [];
  if (!libfiles.length)
    throw new Error(`LLVM 22 static development libraries missing: ${config}`);
  const libraries = [];
  for (const entry of libfiles) {
    const file = entry.replace(/^"|"$/g, "");
    const info = await fs.stat(file);
    if (!info.isFile()) throw new Error(`LLVM library missing: ${file}`);
    libraries.push({ file, size: info.size, mtimeMs: info.mtimeMs });
  }
  const systemLibraries = await query("--system-libs", "--link-static");
  const systemLibraryRemapping = await resolveSystemLibraries(systemLibraries, {
    env: initialEnv,
    run,
  });
  return {
    identity: {
      config: await fs.realpath(config),
      configSha256: await sha256(config),
      prefix,
      libdir,
      version,
      // Nx restores the checksum-pinned bundle with fresh timestamps. Host SDK
      // remappings below retain their stats, even when LLVM itself is pinned.
      libraries: archiveSha256
        ? libraries.map(({ file, size }) => ({ file, size }))
        : libraries,
      ...(archiveSha256 ? { archiveSha256 } : {}),
      systemLibraries,
      ...(systemLibraryRemapping.length
        ? {
            systemLibraryRemapping,
            adapterSha256: await sha256(adapterScript),
          }
        : {}),
    },
    env: llvmEnvironment(env, prefix, libdir),
  };
}

export async function discoverLLVM(env, run) {
  if (env.LLVM_SYS_221_PREFIX) {
    const prefix = path.resolve(env.LLVM_SYS_221_PREFIX);
    for (const name of ["llvm-config", "llvm-config-22"]) {
      const config = path.join(prefix, "bin", name);
      if (await executable(config)) return inspectLLVM(config, env, run);
    }
    throw new Error(
      `LLVM 22 llvm-config missing under LLVM_SYS_221_PREFIX=${prefix}; install development libraries or unset prefix for automatic bootstrap.`,
    );
  }
  for (const name of ["llvm-config-22", "llvm-config"]) {
    for (const directory of (env.PATH ?? "")
      .split(path.delimiter)
      .filter(Boolean)) {
      const config = path.resolve(directory, name);
      if (!(await executable(config))) continue;
      try {
        return await inspectLLVM(config, env, run);
      } catch (error) {
        // Downloading another LLVM cannot install a missing host SDK.
        if (error instanceof MissingSystemLibraryError) throw error;
        // Unversioned LLVM may be another major, or a development install may
        // be incomplete. Continue discovery, then use the verified local bundle.
      }
    }
  }
  return undefined;
}

async function prepareLLVMAdapter(llvm, managed) {
  const remapping = llvm.identity.systemLibraryRemapping;
  if (!remapping?.length) return llvm;
  const directory = path.join(managed, "llvm-config-adapter");
  const config = {
    config: llvm.identity.config,
    prefix: llvm.identity.prefix,
    adapterSha256: llvm.identity.adapterSha256,
    remapping,
  };
  let reusable = false;
  try {
    reusable =
      JSON.stringify(await fs.readJson(path.join(directory, "config.json"))) ===
        JSON.stringify(config) &&
      (await executable(path.join(directory, "bin/llvm-config"))) &&
      (await sha256(path.join(directory, "bin/llvm-config"))) ===
        config.adapterSha256;
  } catch {
    /* Missing/corrupt adapter is recreated from the tracked script. */
  }
  if (!reusable) {
    const stage = await fs.mkdtemp(path.join(managed, "llvm-adapter-"));
    try {
      await fs.ensureDir(path.join(stage, "bin"));
      for (const name of await fs.readdir(path.join(config.prefix, "bin"))) {
        // llvm-sys tries versioned config names before llvm-config. Do not
        // expose any entry point that would bypass our validated remapping.
        if (/^llvm(?:-config|\d+-config)/.test(name)) continue;
        await fs.symlink(
          path.join(config.prefix, "bin", name),
          path.join(stage, "bin", name),
        );
      }
      for (const name of ["lib", "lib64", "include"]) {
        const target = path.join(config.prefix, name);
        if (await fs.pathExists(target))
          await fs.symlink(target, path.join(stage, name));
      }
      await fs.copyFile(adapterScript, path.join(stage, "bin/llvm-config"));
      await fs.chmod(path.join(stage, "bin/llvm-config"), 0o755);
      // Ensure extensionless llvm-config runs as ESM on supported Node versions.
      await fs.writeJson(path.join(stage, "package.json"), { type: "module" });
      await fs.writeJson(path.join(stage, "config.json"), config, {
        spaces: 2,
      });
      await fs.remove(directory);
      await fs.rename(stage, directory);
    } finally {
      await fs.remove(stage);
    }
  }
  const libraryDirs = [
    ...new Set(remapping.map(({ file }) => path.dirname(file))),
  ];
  return {
    ...llvm,
    env: {
      ...llvm.env,
      LLVM_SYS_221_PREFIX: directory,
      // Keep real clang/opt first; llvm-sys uses its explicit managed prefix.
      PATH: [
        ...new Set([
          path.join(llvm.identity.prefix, "bin"),
          path.join(directory, "bin"),
          ...llvm.env.PATH.split(path.delimiter),
        ]),
      ]
        .filter(Boolean)
        .join(path.delimiter),
      LIBRARY_PATH: [...libraryDirs, llvm.env.LIBRARY_PATH]
        .filter(Boolean)
        .join(path.delimiter),
      LD_LIBRARY_PATH: [llvm.env.LD_LIBRARY_PATH, ...libraryDirs]
        .filter(Boolean)
        .join(path.delimiter),
    },
  };
}

export async function prepareLLVM({
  managed,
  env,
  run,
  arch = process.arch,
  libc = process.report.getReport().header.glibcVersionRuntime,
  downloadArchive = downloadLLVMArchive,
  bundle = llvmBundles[arch],
}) {
  if (!libc)
    throw new Error(
      "Linux musl is unsupported; use a glibc Linux host with a C compiler/linker and development SDK.",
    );
  if (!llvmBundles[arch])
    throw new Error(
      `Linux LLVM bootstrap does not support ${arch}; supported architectures: x64, arm64.`,
    );
  const discovered = await discoverLLVM(env, run);
  if (discovered) return prepareLLVMAdapter(discovered, managed);
  const destination = path.join(managed, "llvm");
  const marker = path.join(destination, ".aio-llvm.json");
  const inspectInstalled = () =>
    inspectLLVM(path.join(destination, "bin/llvm-config"), env, run, {
      archiveSha256: bundle.sha256,
    });
  try {
    const installed = await fs.readJson(marker);
    if (JSON.stringify(installed) === JSON.stringify(bundle)) {
      return await prepareLLVMAdapter(await inspectInstalled(), managed);
    }
  } catch (error) {
    if (error instanceof MissingSystemLibraryError) throw error;
    /* Interrupted installs are rebuilt into a new staging directory. */
  }
  const temporary = await fs.mkdtemp(path.join(managed, "llvm-install-"));
  try {
    const archive = path.join(temporary, "llvm.tar.xz");
    console.log(
      `Downloading official LLVM 22.1.8 (${arch}, ~2 GB): ${bundle.url}`,
    );
    await downloadArchive(bundle.url, archive);
    if ((await sha256(archive)) !== bundle.sha256)
      throw new Error("LLVM archive SHA256 checksum mismatch; retry download.");
    const stage = path.join(temporary, "llvm");
    await fs.ensureDir(stage);
    await run("tar", ["-xJf", archive, "-C", stage, "--strip-components=1"], {
      env,
    });
    try {
      await inspectLLVM(path.join(stage, "bin/llvm-config"), env, run);
    } catch (error) {
      if (!(error instanceof MissingSystemLibraryError)) throw error;
      // The checksum and LLVM archives are valid. Keep this large download;
      // host SDK validation below still fails, but retries can reuse LLVM.
    }
    await fs.writeJson(path.join(stage, ".aio-llvm.json"), bundle);
    await fs.remove(destination);
    await fs.rename(stage, destination);
    return await prepareLLVMAdapter(await inspectInstalled(), managed);
  } catch (error) {
    throw new Error(
      `LLVM 22 preparation failed: ${error.message}. Official bundle needs tar/xz and a compatible glibc/native SDK; alternatively install LLVM 22 development libraries (including static libraries) on PATH.`,
      { cause: error },
    );
  } finally {
    await fs.remove(temporary);
  }
}

export async function prepareLinuxCompiler(options) {
  const { managed, source, version, rustToolchain, run, validateSource } =
    options;
  // Validate again here, including on cache hits. No source identity is inferred
  // from a filename, version string, or an inherited PERRY_BUILD_COMMIT.
  await validateSource();
  const llvm = await prepareLLVM(options);
  const env = llvm.env;
  const sourceCommit = (
    await run("git", ["rev-parse", "HEAD"], { cwd: source, env })
  ).trim();
  if (!/^[a-f0-9]{40,64}$/.test(sourceCommit))
    throw new Error("Cannot determine Perry source Git commit");
  const provenance = {
    schema: 1,
    version,
    sourceCommit,
    rustToolchain,
    host: {
      platform: "linux",
      arch: options.arch ?? process.arch,
      libc:
        options.libc ?? process.report.getReport().header.glibcVersionRuntime,
      os: os.release(),
    },
    args: buildArgs,
    llvm: llvm.identity,
    environment: compilerEnvironmentIdentity(env),
  };
  const destination = path.join(managed, "compiler");
  const compiler = path.join(destination, "perry");
  const manifest = path.join(destination, "provenance.json");
  const verifyVersion = async (file) => {
    const output = (
      await run(file, ["--version"], { cwd: source, env })
    ).trim();
    if (
      !new RegExp(
        `^perry\\s+${version.replaceAll(".", "\\.")}(?:\\s|$)`,
        "i",
      ).test(output)
    ) {
      throw new Error(
        `Source compiler version mismatch: expected Perry ${version}, got ${output}`,
      );
    }
  };
  try {
    const { sha256: digest, ...cached } = await fs.readJson(manifest);
    if (
      JSON.stringify(cached) === JSON.stringify(provenance) &&
      (await executable(compiler)) &&
      digest === (await sha256(compiler))
    ) {
      await verifyVersion(compiler);
      await validateSource();
      return { compiler, env };
    }
  } catch {
    /* Missing, corrupt or unloadable compiler must be rebuilt. */
  }
  // Remove the validity marker first. Interrupted Cargo builds never certify an
  // old executable; their reusable intermediates stay in uncached source/target.
  await fs.remove(manifest);
  const temporary = await fs.mkdtemp(path.join(managed, "compiler-build-"));
  try {
    console.log(
      `Building Linux Perry ${version} compiler (${rustToolchain}, perry-dev, LLVM ${llvm.identity.version})`,
    );
    try {
      await run(path.join(env.CARGO_HOME, "bin/cargo"), buildArgs, {
        cwd: source,
        env,
      });
    } finally {
      await validateSource();
    }
    const built = path.join(source, "target/perry-dev/perry");
    const staged = path.join(temporary, "perry");
    await fs.copy(built, staged);
    await fs.chmod(staged, 0o755);
    await verifyVersion(staged);
    await validateSource();
    const digest = await sha256(staged);
    await fs.writeJson(
      path.join(temporary, "provenance.json"),
      { ...provenance, sha256: digest },
      { spaces: 2 },
    );
    await fs.remove(destination);
    await fs.rename(temporary, destination);
    return { compiler, env };
  } finally {
    await fs.remove(temporary);
  }
}
