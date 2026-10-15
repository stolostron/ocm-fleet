/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
/* eslint-disable playwright/no-conditional-in-test -- injected process mocks dispatch command responses. */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fs } from "zx";

import {
  prepareLinuxCompiler,
  prepareLLVM,
} from "./prepare-linux-toolchain.mjs";

async function fixture(t) {
  const managed = await fs.realpath(
    await fs.mkdtemp(path.join(os.tmpdir(), "aio-linux-")),
  );
  t.after(() => fs.remove(managed));
  const prefix = path.join(managed, "system-llvm");
  const source = path.join(managed, "perry");
  // Copied adapter keeps the repository's explicit zx import convention.
  await fs.symlink(
    new URL("../../../node_modules", import.meta.url).pathname,
    path.join(managed, "node_modules"),
  );
  await fs.ensureDir(source);
  await fs.outputFile(path.join(prefix, "bin/llvm-config"), "config");
  await fs.chmod(path.join(prefix, "bin/llvm-config"), 0o755);
  await fs.outputFile(path.join(prefix, "lib/libLLVMCore.a"), "static LLVM");
  const calls = [];
  let llvmVersion = "22.1.8";
  let failBuild = false;
  let commit = "a".repeat(40);
  let dirty = false;
  const run = async (command, args, options) => {
    calls.push({ command, args, ...options });
    if (command.endsWith("llvm-config")) {
      const installed = command.includes("system-llvm")
        ? prefix
        : path.dirname(path.dirname(command));
      if (args[0] === "--version") return llvmVersion;
      if (args[0] === "--prefix") return installed;
      if (args[0] === "--libdir") return path.join(installed, "lib");
      if (args[0] === "--libfiles")
        return path.join(installed, "lib/libLLVMCore.a");
      if (args[0] === "--system-libs") return "-lz -lzstd";
    }
    if (command === "git") return commit;
    if (args[0] === "build") {
      assert.equal(dirty, false);
      if (failBuild) throw new Error("compiler interrupted");
      await fs.outputFile(
        path.join(source, "target/perry-dev/perry"),
        `built ${commit} ${llvmVersion}`,
      );
      await fs.chmod(path.join(source, "target/perry-dev/perry"), 0o755);
    }
    if (args[0] === "--version") return "perry 0.5.1520";
    return "";
  };
  const env = {
    PATH: path.join(prefix, "bin"),
    LLVM_SYS_221_PREFIX: prefix,
    CARGO_HOME: path.join(managed, "cargo"),
  };
  const options = {
    managed,
    source,
    version: "0.5.1520",
    rustToolchain: "nightly-2026-08-20",
    env,
    run,
    arch: "x64",
    libc: "2.43",
    validateSource: async () => {
      if (dirty) throw new Error("dirty source");
    },
  };
  return {
    managed,
    prefix,
    source,
    calls,
    options,
    setVersion: (v) => {
      llvmVersion = v;
    },
    setCommit: (v) => {
      commit = v;
    },
    setFail: (v) => {
      failBuild = v;
    },
    setDirty: () => {
      dirty = true;
    },
  };
}

test("Linux compiler uses pinned dev profile, source provenance, separate executable cache and LLVM environment", async (t) => {
  const f = await fixture(t);
  const prepared = await prepareLinuxCompiler(f.options);
  assert.equal(prepared.compiler, path.join(f.managed, "compiler/perry"));
  assert.equal(prepared.env.LLVM_SYS_221_PREFIX, f.prefix);
  assert.ok(
    prepared.env.LD_LIBRARY_PATH.split(path.delimiter).includes(
      path.join(f.prefix, "lib"),
    ),
  );
  assert.deepEqual(f.calls.find((c) => c.args[0] === "build").args, [
    "build",
    "--locked",
    "--profile",
    "perry-dev",
    "-p",
    "perry",
    "--no-default-features",
    "--features",
    "dev-cli",
  ]);
  const manifest = await fs.readJson(
    path.join(f.managed, "compiler/provenance.json"),
  );
  assert.equal(manifest.sourceCommit, "a".repeat(40));
  assert.equal(manifest.rustToolchain, f.options.rustToolchain);
  assert.equal(manifest.llvm.version, "22.1.8");
  assert.equal(
    manifest.sha256,
    createHash("sha256")
      .update(await fs.readFile(prepared.compiler))
      .digest("hex"),
  );
  await prepareLinuxCompiler(f.options);
  assert.equal(f.calls.filter((c) => c.args[0] === "build").length, 1);
  f.setCommit("b".repeat(40));
  await prepareLinuxCompiler(f.options);
  f.setVersion("22.1.9");
  await prepareLinuxCompiler(f.options);
  await fs.appendFile(prepared.compiler, "corruption");
  await prepareLinuxCompiler(f.options);
  assert.equal(f.calls.filter((c) => c.args[0] === "build").length, 4);
  await prepareLinuxCompiler({
    ...f.options,
    rustToolchain: "nightly-2026-08-21",
  });
  await prepareLinuxCompiler({
    ...f.options,
    env: { ...f.options.env, RUSTFLAGS: "-C debuginfo=0" },
  });
  assert.equal(f.calls.filter((c) => c.args[0] === "build").length, 6);
});

test("partial compiler cache and failed build retry; dirty checkout never accepted", async (t) => {
  const f = await fixture(t);
  await fs.outputFile(
    path.join(f.managed, "compiler/perry"),
    "unproven executable",
  );
  f.setFail(true);
  await assert.rejects(prepareLinuxCompiler(f.options), /compiler interrupted/);
  await assert.rejects(
    fs.readJson(path.join(f.managed, "compiler/provenance.json")),
    /ENOENT/,
  );
  f.setFail(false);
  await prepareLinuxCompiler(f.options);
  f.setDirty();
  await assert.rejects(prepareLinuxCompiler(f.options), /dirty source/);
});

test("invalid explicit LLVM prefix fails actionable; versioned PATH discovery checks static libraries", async (t) => {
  const f = await fixture(t);
  f.setVersion("21.1.0");
  await assert.rejects(prepareLLVM(f.options), /LLVM.*22/);
  f.setVersion("22.1.8");
  await fs.rename(
    path.join(f.prefix, "bin/llvm-config"),
    path.join(f.prefix, "bin/llvm-config-22"),
  );
  const run = (command, args, options) =>
    f.options.run(
      command.replace("llvm-config-22", "llvm-config"),
      args,
      options,
    );
  const llvm = await prepareLLVM({
    ...f.options,
    env: { PATH: path.join(f.prefix, "bin") },
    run,
  });
  assert.equal(llvm.identity.prefix, f.prefix);
  await fs.remove(path.join(f.prefix, "lib/libLLVMCore.a"));
  await assert.rejects(prepareLLVM(f.options), /LLVM|ENOENT/);
});

test("official LLVM download verifies digest, installs atomically and retries failed transfer", async (t) => {
  const f = await fixture(t);
  const archive = Buffer.from("fixture archive");
  const bundle = {
    url: "https://example.invalid/llvm.tar.xz",
    sha256: createHash("sha256").update(archive).digest("hex"),
  };
  let fail = true;
  const options = {
    ...f.options,
    env: { PATH: "" },
    bundle,
    downloadArchive: async (_url, file) => {
      await fs.writeFile(file, fail ? "truncated" : archive);
    },
    run: async (command, args, execution) => {
      if (command === "tar") {
        const destination = args[args.indexOf("-C") + 1];
        await fs.copy(f.prefix, destination);
        return "";
      }
      return f.options.run(command, args, execution);
    },
  };
  await assert.rejects(prepareLLVM(options), /checksum|SHA256/);
  assert.equal(await fs.pathExists(path.join(f.managed, "llvm")), false);
  fail = false;
  const llvm = await prepareLLVM(options);
  assert.equal(llvm.identity.prefix, path.join(f.managed, "llvm"));
  assert.equal(
    (await fs.readdir(f.managed)).some((name) =>
      name.startsWith("llvm-install-"),
    ),
    false,
  );
  await prepareLLVM({
    ...options,
    downloadArchive: () => {
      throw new Error("must reuse verified install");
    },
  });
  const compilerOptions = {
    ...options,
    env: { ...options.env, CARGO_HOME: path.join(f.managed, "cargo") },
  };
  await prepareLinuxCompiler(compilerOptions);
  // Nx restoration may change timestamps; pinned bundle identity must survive it.
  await fs.utimes(
    path.join(f.managed, "llvm/lib/libLLVMCore.a"),
    new Date(),
    new Date(),
  );
  await prepareLinuxCompiler(compilerOptions);
  assert.equal(f.calls.filter((call) => call.args[0] === "build").length, 1);
});

test("network and extraction failures leave no valid LLVM install and permit retry", async (t) => {
  const f = await fixture(t);
  const options = {
    ...f.options,
    env: { PATH: "" },
    downloadArchive: async (_url, file) => {
      await fs.writeFile(file, "partial");
      throw new Error("network interrupted");
    },
  };
  await assert.rejects(prepareLLVM(options), /network interrupted/);
  assert.equal(await fs.pathExists(path.join(f.managed, "llvm")), false);
  assert.equal(
    (await fs.readdir(f.managed)).some((name) =>
      name.startsWith("llvm-install-"),
    ),
    false,
  );
  const bytes = Buffer.from("archive");
  await assert.rejects(
    prepareLLVM({
      ...options,
      bundle: {
        url: "https://example.invalid/archive",
        sha256: createHash("sha256").update(bytes).digest("hex"),
      },
      downloadArchive: (_url, file) => fs.writeFile(file, bytes),
      run: () => {
        throw new Error("extraction interrupted");
      },
    }),
    /extraction interrupted/,
  );
  assert.equal(await fs.pathExists(path.join(f.managed, "llvm")), false);
});

test("compiler version mismatch cannot certify a cached or newly built executable", async (t) => {
  const f = await fixture(t);
  await prepareLinuxCompiler(f.options);
  const run = (command, args, options) =>
    args[0] === "--version" && !command.endsWith("llvm-config")
      ? "perry 0.0.1"
      : f.options.run(command, args, options);
  await assert.rejects(
    prepareLinuxCompiler({ ...f.options, run }),
    /version mismatch/,
  );
  assert.equal(
    await fs.pathExists(path.join(f.managed, "compiler/provenance.json")),
    false,
  );
});

test("Cargo source mutation is rejected before compiler cache promotion", async (t) => {
  const f = await fixture(t);
  const run = async (command, args, options) => {
    const output = await f.options.run(command, args, options);
    if (args[0] === "build") f.setDirty();
    return output;
  };
  await assert.rejects(
    prepareLinuxCompiler({ ...f.options, run }),
    /dirty source/,
  );
  assert.equal(
    await fs.pathExists(path.join(f.managed, "compiler/provenance.json")),
    false,
  );
});

test("musl unsupported before any download or build", async (t) => {
  const f = await fixture(t);
  await assert.rejects(
    prepareLinuxCompiler({ ...f.options, libc: null }),
    /musl.*glibc/,
  );
  assert.equal(f.calls.length, 0);
});

async function systemLibraryFixture(t) {
  const f = await fixture(t);
  const missing = path.join(f.managed, "debian/libzstd.a");
  const valid = path.join(f.prefix, "lib/libvalid.a");
  const host = path.join(f.managed, "host/libzstd.so");
  await fs.outputFile(valid, "valid archive");
  await fs.outputFile(host, "host shared library");
  const raw = `-lrt ${missing} "${valid}" -lzstd\n`;
  const run = async (command, args, options) => {
    if (args[0] === "--system-libs") return raw;
    if (args[0] === "-print-file-name=libzstd.so") return host;
    if (args[0] === "-print-file-name=libzstd.a") return "libzstd.a";
    return f.options.run(command, args, options);
  };
  return { ...f, missing, valid, host, raw, options: { ...f.options, run } };
}

test("missing absolute system libs use host SDK and local adapter; valid paths and tools preserved", async (t) => {
  const f = await systemLibraryFixture(t);
  await fs.outputFile(path.join(f.prefix, "bin/clang"), "clang");
  await fs.outputFile(path.join(f.prefix, "bin/opt"), "opt");
  const llvm = await prepareLLVM(f.options);
  const adapter = path.join(f.managed, "llvm-config-adapter");
  assert.equal(llvm.env.LLVM_SYS_221_PREFIX, adapter);
  assert.equal(
    llvm.env.PATH.split(path.delimiter)[0],
    path.join(f.prefix, "bin"),
  );
  assert.ok(
    llvm.env.LIBRARY_PATH.split(path.delimiter).includes(path.dirname(f.host)),
  );
  assert.equal(
    await fs.realpath(path.join(adapter, "bin/clang")),
    path.join(f.prefix, "bin/clang"),
  );
  assert.equal(
    await fs.realpath(path.join(adapter, "lib")),
    path.join(f.prefix, "lib"),
  );
  assert.equal(await fs.pathExists(f.missing), false);
  assert.deepEqual(llvm.identity.systemLibraryRemapping, [
    {
      original: f.missing,
      name: "zstd",
      file: f.host,
      size: 19,
      mtimeMs: (await fs.stat(f.host)).mtimeMs,
    },
  ]);
  assert.match(llvm.identity.adapterSha256, /^[a-f0-9]{64}$/);
  assert.ok(
    (await fs.stat(path.join(adapter, "bin/llvm-config"))).mode & 0o111,
  );
  const adapterStat = await fs.stat(adapter);
  await prepareLLVM(f.options);
  assert.equal((await fs.stat(adapter)).mtimeMs, adapterStat.mtimeMs);
  await prepareLinuxCompiler(f.options);
  await prepareLinuxCompiler(f.options);
  assert.equal(f.calls.filter((call) => call.args[0] === "build").length, 1);
  await fs.appendFile(f.host, "changed SDK");
  await prepareLinuxCompiler(f.options);
  assert.equal(f.calls.filter((call) => call.args[0] === "build").length, 2);
  const config = await fs.readJson(path.join(adapter, "config.json"));
  assert.equal(config.remapping[0].size, (await fs.stat(f.host)).size);
});

test("host lookup prefers shared library, falls back to regular static file, preserves linker flags", async (t) => {
  const f = await systemLibraryFixture(t);
  await fs.remove(f.host);
  await fs.ensureDir(f.host); // Existing directory is not a library.
  const archive = path.join(f.managed, "host/libzstd.a");
  await fs.outputFile(archive, "archive");
  const calls = [];
  const llvm = await prepareLLVM({
    ...f.options,
    env: { ...f.options.env, CC: "host-cc", LIBRARY_PATH: "/existing" },
    run: (command, args, options) => {
      if (command === "host-cc") {
        calls.push(args[0]);
        return args[0].endsWith(".so") ? f.host : archive;
      }
      return f.options.run(command, args, options);
    },
  });
  assert.deepEqual(calls, [
    "-print-file-name=libzstd.so",
    "-print-file-name=libzstd.a",
  ]);
  assert.equal(llvm.identity.systemLibraryRemapping[0].file, archive);
  assert.equal(
    llvm.env.LIBRARY_PATH,
    `${path.dirname(archive)}${path.delimiter}/existing`,
  );
  const plain = await fixture(t);
  const unchanged = await prepareLLVM(plain.options);
  assert.equal(unchanged.env.LLVM_SYS_221_PREFIX, plain.prefix);
  assert.equal(unchanged.env.LIBRARY_PATH, undefined);
  assert.equal(
    await fs.pathExists(path.join(plain.managed, "llvm-config-adapter")),
    false,
  );
});

test("missing host SDK fails actionable during discovery without bundle download", async (t) => {
  const f = await systemLibraryFixture(t);
  await fs.remove(f.host);
  let downloads = 0;
  await assert.rejects(
    prepareLLVM({
      ...f.options,
      env: { PATH: path.join(f.prefix, "bin") },
      downloadArchive: () => {
        downloads++;
      },
    }),
    /libzstd-devel/,
  );
  assert.equal(downloads, 0);
  await assert.rejects(
    prepareLLVM({
      ...f.options,
      run: (command, args, options) =>
        args[0] === "--system-libs"
          ? path.join(f.managed, "missing/libxml2.a")
          : args[0].startsWith("-print-file-name=")
            ? args[0].slice(17)
            : f.options.run(command, args, options),
    }),
    /native.*SDK/,
  );
});

test("adapter real subprocess forwards arguments, bytes, stderr and exit; only system tokens change", async (t) => {
  const f = await systemLibraryFixture(t);
  const config = path.join(f.prefix, "bin/llvm-config");
  await fs.writeFile(
    config,
    `#!/usr/bin/env node\nconst args = process.argv.slice(2);\nprocess.stderr.write('llvm diagnostic\\n');\nprocess.stdout.write(args.includes('--system-libs') ? ${JSON.stringify(f.raw)} : JSON.stringify(args) + '\\n');\nprocess.exit(args.includes('--fail') ? 7 : 0);\n`,
  );
  const llvm = await prepareLLVM(f.options);
  const adapter = path.join(llvm.env.LLVM_SYS_221_PREFIX, "bin/llvm-config");
  const invoke = (args) =>
    spawnSync(adapter, args, { env: process.env, encoding: "utf8" });
  const system = invoke(["--link-static", "--system-libs"]);
  assert.equal(system.status, 0, system.stderr);
  assert.equal(system.stdout, f.raw.replace(f.missing, "-lzstd"));
  assert.equal(system.stderr, "llvm diagnostic\n");
  for (const flag of ["--version", "--prefix", "--libdir", "--libs"]) {
    const result = invoke([flag, "argument with spaces"]);
    assert.equal(result.status, 0, result.stderr);
    assert.equal(
      result.stdout,
      JSON.stringify([flag, "argument with spaces"]) + "\n",
    );
  }
  const failed = invoke(["--system-libs", "--fail"]);
  assert.equal(failed.status, 7);
  assert.equal(failed.stderr, "llvm diagnostic\n");
  assert.equal(failed.stdout, f.raw);
});

test("verified managed bundle with missing native SDK never triggers another download", async (t) => {
  const f = await systemLibraryFixture(t);
  const prefix = path.join(f.managed, "llvm");
  const bundle = {
    url: "https://example.invalid/llvm.tar.xz",
    sha256: "fixture",
  };
  await fs.copy(f.prefix, prefix);
  await fs.writeJson(path.join(prefix, ".aio-llvm.json"), bundle);
  await fs.remove(f.host);
  let downloads = 0;
  await assert.rejects(
    prepareLLVM({
      ...f.options,
      env: { PATH: "" },
      bundle,
      downloadArchive: () => {
        downloads++;
      },
    }),
    /libzstd-devel/,
  );
  assert.equal(downloads, 0);
});

test("cold verified bundle remains installed when host SDK validation fails, avoiding repeat downloads", async (t) => {
  const f = await systemLibraryFixture(t);
  await fs.remove(f.host);
  const bytes = Buffer.from("verified fixture archive");
  let downloads = 0;
  const options = {
    ...f.options,
    env: { PATH: "" },
    bundle: {
      url: "https://example.invalid/llvm.tar.xz",
      sha256: createHash("sha256").update(bytes).digest("hex"),
    },
    downloadArchive: async (_url, file) => {
      downloads++;
      await fs.writeFile(file, bytes);
    },
    run: async (command, args, execution) => {
      if (command === "tar") {
        await fs.copy(f.prefix, args[args.indexOf("-C") + 1]);
        return "";
      }
      return f.options.run(command, args, execution);
    },
  };
  await assert.rejects(prepareLLVM(options), /libzstd-devel/);
  assert.equal(
    await fs.pathExists(path.join(f.managed, "llvm/.aio-llvm.json")),
    true,
  );
  await assert.rejects(prepareLLVM(options), /libzstd-devel/);
  assert.equal(downloads, 1);
});
