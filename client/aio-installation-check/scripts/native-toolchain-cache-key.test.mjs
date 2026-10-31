/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, realpath, rm, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { fs } from "zx";

import { nativeToolchainCacheKey } from "./native-toolchain-cache-key.mjs";

test("fingerprint tracks exact npm pin, host ABI, and canonical location", async (t) => {
  const root = await realpath(
    await mkdtemp(path.join(os.tmpdir(), "aio-cache-key-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  const manifest = path.join(root, "package.json");
  await writeFile(
    manifest,
    JSON.stringify({ devDependencies: { "@perryts/perry": "0.5.1520" } }),
  );
  const first = await nativeToolchainCacheKey(root);
  assert.deepEqual(first, {
    perry: "0.5.1520",
    platform: process.platform,
    arch: process.arch,
    os: os.release(),
    libc: process.report.getReport().header.glibcVersionRuntime ?? null,
    root,
    ...(process.platform === "linux"
      ? { compilerEnvironment: first.compilerEnvironment, llvm: first.llvm }
      : {}),
  });
  const alias = path.join(root, "alias");
  await symlink(root, alias);
  assert.deepEqual(await nativeToolchainCacheKey(alias), first);
  await writeFile(
    manifest,
    JSON.stringify({ devDependencies: { "@perryts/perry": "0.5.1521" } }),
  );
  assert.notDeepEqual(await nativeToolchainCacheKey(root), first);
});

test("Linux fingerprint includes accepted compiler environment and LLVM prefix; macOS ignores compiler setup", async (t) => {
  const root = await fsFixture(t);
  const first = await nativeToolchainCacheKey(root, {
    platform: "linux",
    env: {
      PATH: "",
      LLVM_SYS_221_PREFIX: "/missing/llvm22",
      RUSTFLAGS: "-C debuginfo=0",
    },
  });
  assert.equal(
    first.compilerEnvironment.LLVM_SYS_221_PREFIX,
    "/missing/llvm22",
  );
  assert.match(first.llvm.invalid, /LLVM.*22/);
  const second = await nativeToolchainCacheKey(root, {
    platform: "linux",
    env: { PATH: "", LLVM_SYS_221_PREFIX: "/different/llvm22" },
  });
  assert.notDeepEqual(second, first);
  const mac = await nativeToolchainCacheKey(root, {
    platform: "darwin",
    env: { LLVM_SYS_221_PREFIX: "/missing/llvm22" },
  });
  assert.equal(mac.compilerEnvironment, undefined);
  assert.equal(mac.llvm, undefined);
});

async function fsFixture(t) {
  const root = await realpath(
    await mkdtemp(path.join(os.tmpdir(), "aio-cache-env-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ devDependencies: { "@perryts/perry": "0.5.1520" } }),
  );
  return root;
}

test("CLI emits one JSON fingerprint independent of working directory", async () => {
  const script = fileURLToPath(
    new URL("./native-toolchain-cache-key.mjs", import.meta.url),
  );
  const result = spawnSync(process.execPath, [script], {
    cwd: os.tmpdir(),
    encoding: "utf8",
  });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stderr, "");
  assert.deepEqual(JSON.parse(result.stdout), await nativeToolchainCacheKey());
});

test("Linux fingerprint tracks remapped host libraries for discovered and managed LLVM without writing adapters", async (t) => {
  const root = await fsFixture(t);
  const prefix = path.join(root, ".native-toolchain/llvm");
  const config = path.join(prefix, "bin/llvm-config");
  const archive = path.join(prefix, "lib/libLLVMCore.a");
  const host = path.join(root, "host/libzstd.so");
  await fs.outputFile(config, "config");
  await fs.chmod(config, 0o755);
  await fs.outputFile(archive, "LLVM archive");
  await fs.outputFile(host, "host shared");
  const run = async (_command, args) => {
    const responses = {
      "--version": "22.1.8",
      "--prefix": prefix,
      "--libdir": path.dirname(archive),
      "--libfiles": archive,
      "--system-libs": "/foreign/libzstd.a -lz",
      "-print-file-name=libzstd.so": host,
    };
    return responses[args[0]];
  };
  const options = { platform: "linux", env: { PATH: "" }, run };
  const managed = await nativeToolchainCacheKey(root, options);
  assert.equal(managed.llvm.systemLibraryRemapping[0].file, host);
  assert.match(managed.llvm.adapterSha256, /^[a-f0-9]{64}$/);
  assert.equal(
    await fs.pathExists(
      path.join(root, ".native-toolchain/llvm-config-adapter"),
    ),
    false,
  );
  await fs.appendFile(host, "new SDK");
  const changed = await nativeToolchainCacheKey(root, options);
  assert.notDeepEqual(changed.llvm, managed.llvm);
  const explicit = await nativeToolchainCacheKey(root, {
    ...options,
    env: { PATH: "", LLVM_SYS_221_PREFIX: prefix },
  });
  assert.deepEqual(explicit.llvm, changed.llvm);
  await fs.writeJson(path.join(prefix, ".aio-llvm.json"), {
    sha256: "pinned-bundle",
  });
  const pinned = await nativeToolchainCacheKey(root, options);
  await fs.utimes(archive, new Date(0), new Date(0));
  assert.deepEqual(
    (await nativeToolchainCacheKey(root, options)).llvm,
    pinned.llvm,
  );
});
