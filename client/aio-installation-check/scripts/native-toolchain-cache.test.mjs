/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
import assert from "node:assert/strict";
import {
  chmod,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readlink,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { NxCache, closeDbConnection, connectToNxDb } = require("nx/src/native");
const { getIgnoreObject } = require("nx/src/utils/ignore");
const projectRoot = fileURLToPath(new URL("../", import.meta.url));
const workspaceRoot = path.resolve(projectRoot, "../..");
const projectPath = "client/aio-installation-check";

test("root ignores exclude managed checkout and nested ignore-file events", () => {
  const ignored = getIgnoreObject(workspaceRoot);
  for (const relative of [
    "perry/Cargo.toml",
    "perry/.gitignore",
    "cargo/registry/index/config.json",
    "build/release/runtime.a",
  ]) {
    assert.ok(
      ignored.ignores(`${projectPath}/.native-toolchain/${relative}`),
      relative,
    );
  }
});

test("actual Nx cache restores source/Git/Rust while preserving uncached warm artifacts", async (t) => {
  const root = await mkdtemp(path.join(os.tmpdir(), "aio-nx-cache-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const config = JSON.parse(
    await readFile(path.join(projectRoot, "project.json"), "utf8"),
  );
  const outputs = config.targets["prepare-native-toolchain"].outputs.map(
    (output) => output.replace("{projectRoot}", projectPath),
  );
  const managed = path.join(root, projectPath, ".native-toolchain");
  const fixture = async (relative, contents) => {
    const file = path.join(managed, relative);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, contents);
  };
  await fixture(".managed", "owned");
  await fixture("perry/.git/HEAD", "ref: refs/heads/main\n");
  await fixture("perry/Cargo.toml", "pinned source");
  await fixture("cargo/bin/rustup", "executable");
  await chmod(path.join(managed, "cargo/bin/rustup"), 0o755);
  await symlink("rustup", path.join(managed, "cargo/bin/cargo"));
  await fixture("rustup/toolchains/pinned/bin/rustc", "compiler");
  await fixture("compiler/perry", "source compiler");
  await chmod(path.join(managed, "compiler/perry"), 0o755);
  await fixture("compiler/provenance.json", "source + LLVM provenance");
  await fixture("llvm/lib/libLLVM.so.22.1", "LLVM shared library");
  await symlink("libLLVM.so.22.1", path.join(managed, "llvm/lib/libLLVM.so"));
  await fixture("build/warm.a", "keep build");
  await fixture("cargo/registry/warm.crate", "keep registry");
  await symlink("../build", path.join(managed, "perry/target"));
  await mkdir(path.join(root, "db"));
  const connection = connectToNxDb(path.join(root, "db"));
  try {
    const cache = new NxCache(
      root,
      path.join(root, "cache"),
      connection,
      false,
    );
    cache.put("fixture", "prepared", outputs, 0);
    const cached = cache.get("fixture");
    const snapshot = path.join(
      cached.outputsPath,
      projectPath,
      ".native-toolchain",
    );
    await assert.rejects(lstat(path.join(snapshot, "build")), {
      code: "ENOENT",
    });
    await assert.rejects(lstat(path.join(snapshot, "cargo/registry")), {
      code: "ENOENT",
    });
    assert.equal(
      await readlink(path.join(snapshot, "perry/target")),
      "../build",
    );
    // Remove only fixture setup outputs, leaving downstream build state warm.
    for (const relative of [
      ".managed",
      "perry",
      "cargo/bin",
      "rustup",
      "compiler",
      "llvm",
    ]) {
      await rm(path.join(managed, relative), { recursive: true, force: true });
    }
    for (let repeat = 0; repeat < 2; repeat++) {
      cache.copyFilesFromCache(cached, outputs);
      assert.equal(
        await readFile(path.join(managed, "perry/.git/HEAD"), "utf8"),
        "ref: refs/heads/main\n",
      );
      assert.equal(
        await readFile(path.join(managed, "perry/target/warm.a"), "utf8"),
        "keep build",
      );
      assert.equal(
        await readFile(path.join(managed, "cargo/registry/warm.crate"), "utf8"),
        "keep registry",
      );
      assert.equal(
        await readFile(path.join(managed, ".managed"), "utf8"),
        "owned",
      );
      assert.equal(
        await readFile(path.join(managed, "compiler/perry"), "utf8"),
        "source compiler",
      );
      assert.equal(
        await readFile(path.join(managed, "compiler/provenance.json"), "utf8"),
        "source + LLVM provenance",
      );
      assert.equal(
        (await stat(path.join(managed, "compiler/perry"))).mode & 0o111,
        0o111,
      );
      assert.equal(
        await readFile(path.join(managed, "llvm/lib/libLLVM.so"), "utf8"),
        "LLVM shared library",
      );
      assert.equal(
        await readlink(path.join(managed, "cargo/bin/cargo")),
        "rustup",
      );
      assert.equal(
        (await stat(path.join(managed, "cargo/bin/rustup"))).mode & 0o111,
        0o111,
      );
      assert.equal(
        await readFile(
          path.join(managed, "rustup/toolchains/pinned/bin/rustc"),
          "utf8",
        ),
        "compiler",
      );
    }
  } finally {
    closeDbConnection(connection);
  }
});
