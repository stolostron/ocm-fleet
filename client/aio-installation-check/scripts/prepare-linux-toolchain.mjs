#!/usr/bin/env node
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

import { fs } from "zx";

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

async function inspectLLVM(config, env, run) {
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
  return {
    identity: {
      config: await fs.realpath(config),
      configSha256: await sha256(config),
      prefix,
      libdir,
      version,
      libraries,
      systemLibraries: await query("--system-libs", "--link-static"),
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
      } catch {
        // Unversioned LLVM may be another major, or a development install may
        // be incomplete. Continue discovery, then use the verified local bundle.
      }
    }
  }
  return undefined;
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
  if (discovered) return discovered;
  const destination = path.join(managed, "llvm");
  const marker = path.join(destination, ".aio-llvm.json");
  const inspectInstalled = async () => {
    const llvm = await inspectLLVM(
      path.join(destination, "bin/llvm-config"),
      env,
      run,
    );
    // Nx restores files with fresh timestamps. For our checksum-pinned bundle,
    // identify the release by its digest and library sizes, not restore times.
    llvm.identity.archiveSha256 = bundle.sha256;
    llvm.identity.libraries = llvm.identity.libraries.map(({ file, size }) => ({
      file,
      size,
    }));
    return llvm;
  };
  try {
    const installed = await fs.readJson(marker);
    if (JSON.stringify(installed) === JSON.stringify(bundle)) {
      return await inspectInstalled();
    }
  } catch {
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
    await inspectLLVM(path.join(stage, "bin/llvm-config"), env, run);
    await fs.writeJson(path.join(stage, ".aio-llvm.json"), bundle);
    await fs.remove(destination);
    await fs.rename(stage, destination);
    return await inspectInstalled();
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
