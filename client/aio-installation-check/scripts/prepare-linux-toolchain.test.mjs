/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
/* eslint-disable playwright/no-conditional-in-test -- injected process mocks dispatch command responses. */
import assert from "node:assert/strict";
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
