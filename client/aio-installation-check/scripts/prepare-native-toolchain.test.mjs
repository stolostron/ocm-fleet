/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
import assert from "node:assert/strict";
import {
  mkdir,
  mkdtemp,
  readFile,
  readlink,
  realpath,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  prepareNativeToolchain,
  runCommand,
  validatePerrySource,
} from "./prepare-native-toolchain.mjs";

async function fixture(t) {
  const root = await realpath(
    await mkdtemp(path.join(os.tmpdir(), "aio-bootstrap-")),
  );
  t.after(() => rm(root, { recursive: true, force: true }));
  await writeFile(
    path.join(root, "package.json"),
    JSON.stringify({ devDependencies: { "@perryts/perry": "0.5.1520" } }),
  );
  const calls = [];
  let installed = false;
  let wrongTag = false;
  let dirty = false;
  let failClone = false;
  let failInstall = false;
  const run = async (command, args, options) => {
    calls.push({ command, args, ...options });
    if (args[0] === "clone") {
      if (failClone) throw new Error("network interrupted");
      const source = args.at(-1);
      for (const file of [
        "Cargo.toml",
        "Cargo.lock",
        "crates/perry-runtime-static/Cargo.toml",
        "crates/perry-stdlib-static/Cargo.toml",
        "crates/perry-ext-net/Cargo.toml",
        "crates/perry-ext-ws/Cargo.toml",
        "crates/perry-ext-events/Cargo.toml",
      ]) {
        await mkdir(path.dirname(path.join(source, file)), { recursive: true });
        await writeFile(path.join(source, file), "[workspace]\n");
      }
      await writeFile(
        path.join(source, "external-tools.json"),
        JSON.stringify({ tools: { rust: { version: "nightly-2026-08-20" } } }),
      );
    }
    if (args[0] === "remote") return "https://github.com/PerryTS/perry.git\n";
    if (args[0] === "describe") return wrongTag ? "v0.0.1\n" : "v0.5.1520\n";
    if (args[0] === "status") return dirty ? " M Cargo.toml\n" : "";
    if (args[0] === "run" && !installed)
      throw new Error("toolchain unavailable");
    if (args[0] === "toolchain") {
      if (failInstall) throw new Error("download interrupted");
      installed = true;
    }
    if (args.includes("--no-modify-path")) {
      const bin = path.join(options.env.CARGO_HOME, "bin");
      await mkdir(bin, { recursive: true });
      await writeFile(path.join(bin, "rustup"), "fake rustup");
    }
    return "";
  };
  const download = async (url, file) => {
    calls.push({ download: url });
    await writeFile(file, "fake installer");
  };
  const prepare = (options = {}) =>
    prepareNativeToolchain(root, {
      run,
      download,
      platform: "darwin",
      arch: "arm64",
      env: {
        PATH: "/usr/bin",
        CARGO_HOME: "/global/cargo",
        RUSTUP_HOME: "/global/rustup",
        PERRY_WORKSPACE_ROOT: "/external/perry",
        PERRY_NO_AUTO_OPTIMIZE: "",
        PERRY_BUILD_COMMIT: "forged",
        GIT_DIR: "/external/git",
      },
      ...options,
    });
  return {
    root,
    calls,
    run,
    prepare,
    setWrongTag: () => {
      wrongTag = true;
    },
    setDirty: () => {
      dirty = true;
    },
    setFailClone: (value) => {
      failClone = value;
    },
    setFailInstall: (value) => {
      failInstall = value;
    },
  };
}

test("cold setup follows npm/source pins, isolates every child, then reuses installation", async (t) => {
  const f = await fixture(t);
  const result = await f.prepare();
  assert.equal(result.root, path.join(f.root, ".native-toolchain/perry"));
  assert.equal(result.rustToolchain, "nightly-2026-08-20");
  const clone = f.calls.find((call) => call.args?.[0] === "clone");
  assert.deepEqual(clone.args.slice(0, -1), [
    "clone",
    "--depth",
    "1",
    "--branch",
    "v0.5.1520",
    "--single-branch",
    "https://github.com/PerryTS/perry.git",
  ]);
  const install = f.calls.find((call) =>
    call.args?.includes("--no-modify-path"),
  );
  assert.ok(install.args.includes("-y"));
  assert.deepEqual(install.args.slice(-4), [
    "--profile",
    "minimal",
    "--default-toolchain",
    "none",
  ]);
  assert.ok(
    f.calls.some(
      (call) =>
        call.args?.join(" ") ===
        "toolchain install nightly-2026-08-20 --profile minimal",
    ),
  );
  for (const call of f.calls.filter((call) => call.env)) {
    assert.equal(
      call.env.CARGO_HOME,
      path.join(f.root, ".native-toolchain/cargo"),
    );
    assert.equal(
      call.env.RUSTUP_HOME,
      path.join(f.root, ".native-toolchain/rustup"),
    );
    assert.equal(call.env.PERRY_NO_TELEMETRY, "1");
    assert.equal(Object.hasOwn(call.env, "PERRY_NO_AUTO_OPTIMIZE"), false);
    assert.equal(Object.hasOwn(call.env, "PERRY_BUILD_COMMIT"), false);
    assert.equal(
      Object.keys(call.env).some((key) => key.startsWith("GIT_")),
      false,
    );
  }
  const count = f.calls.length;
  await f.prepare();
  assert.ok(
    f.calls
      .slice(count)
      .every(
        (call) =>
          !call.download &&
          call.args[0] !== "clone" &&
          call.args[0] !== "toolchain",
      ),
  );
});

test("real Git status permits only managed root target symlink, not arbitrary untracked source or target links", async (t) => {
  const f = await fixture(t);
  const prepared = await f.prepare();
  const source = prepared.root;
  const options = { cwd: source, env: prepared.env };
  // No commits needed: ignore fixture manifests so real Git status isolates
  // target/'s directory-only ignore behavior and other untracked source files.
  await writeFile(
    path.join(source, ".gitignore"),
    "/.gitignore\n/Cargo.toml\n/Cargo.lock\n/crates/\n/external-tools.json\n/target/\n",
  );
  runCommand("git", ["init", "--quiet"], options);
  assert.equal(
    runCommand(
      "git",
      ["status", "--porcelain", "--untracked-files=normal"],
      options,
    ).trim(),
    "?? target",
  );
  const runners = { status: runCommand };
  const run = (command, args, execution) =>
    (runners[args[0]] ?? f.run)(command, args, execution);
  const validate = () =>
    validatePerrySource(source, prepared.version, run, prepared.env);
  await validate();
  runners.status = () => " M target\n";
  await assert.rejects(validate(), /modified or untracked/);
  runners.status = runCommand;
  await f.prepare({ run });
  await writeFile(path.join(source, "intruder.rs"), "untracked source");
  await assert.rejects(validate(), /untracked source/);
  await rm(path.join(source, "intruder.rs"));
  const target = path.join(source, "target");
  await rm(target);
  await symlink("../elsewhere", target);
  await assert.rejects(validate(), /Unexpected native target symlink/);
  await rm(target);
  await writeFile(target, "not managed link");
  await assert.rejects(validate(), /untracked source/);
  await rm(target);
  await mkdir(target);
  await validate();
});

test("Linux delegates compiler preparation with same validated source and Rust; macOS never source-builds compiler", async (t) => {
  const f = await fixture(t);
  let linuxCalls = 0;
  const prepareLinux = async (options) => {
    linuxCalls++;
    assert.equal(options.source, path.join(f.root, ".native-toolchain/perry"));
    assert.equal(options.rustToolchain, "nightly-2026-08-20");
    assert.equal(options.env.PERRY_BUILD_COMMIT, undefined);
    await options.validateSource();
    return {
      compiler: "/managed/compiler/perry",
      env: { ...options.env, LD_LIBRARY_PATH: "/llvm/lib" },
    };
  };
  const result = await f.prepare({
    platform: "linux",
    arch: "x64",
    libc: "2.43",
    prepareLinux,
  });
  assert.equal(result.compiler, "/managed/compiler/perry");
  assert.equal(result.env.LD_LIBRARY_PATH, "/llvm/lib");
  await f.prepare({ prepareLinux });
  assert.equal(linuxCalls, 1);
  assert.equal(f.calls.filter((call) => call.args?.[0] === "build").length, 0);
});

test("rejects wrong or modified reused source before running Rust", async (t) => {
  const f = await fixture(t);
  await f.prepare();
  f.setWrongTag();
  await assert.rejects(f.prepare(), /v0\.5\.1520/);
  const g = await fixture(t);
  await g.prepare();
  g.setDirty();
  await assert.rejects(g.prepare(), /modified|dirty/);
});

test("failed clone and interrupted Rust install can retry without deleting foreign files", async (t) => {
  const f = await fixture(t);
  f.setFailClone(true);
  await assert.rejects(f.prepare(), /network interrupted/);
  f.setFailClone(false);
  f.setFailInstall(true);
  await assert.rejects(f.prepare(), /download interrupted/);
  f.setFailInstall(false);
  await f.prepare();
  assert.equal(f.calls.filter((call) => call.args?.[0] === "clone").length, 2);
});

test("refuses unowned or symlinked managed directories", async (t) => {
  const f = await fixture(t);
  const managed = path.join(f.root, ".native-toolchain");
  await mkdir(managed);
  await writeFile(path.join(managed, "keep.txt"), "user work");
  await assert.rejects(f.prepare(), /unmanaged|unowned/);
  assert.equal(
    await readFile(path.join(managed, "keep.txt"), "utf8"),
    "user work",
  );
  assert.equal(f.calls.length, 0);
  const g = await fixture(t);
  await symlink(managed, path.join(g.root, ".native-toolchain"));
  await assert.rejects(g.prepare(), /symlink/);
});

test("requires exact npm pin and valid upstream Rust declaration", async (t) => {
  const f = await fixture(t);
  await writeFile(
    path.join(f.root, "package.json"),
    JSON.stringify({ devDependencies: { "@perryts/perry": "^0.5.1520" } }),
  );
  await assert.rejects(f.prepare(), /exact.*pin/);
  assert.equal(f.calls.length, 0);
  const g = await fixture(t);
  await g.prepare();
  await writeFile(
    path.join(g.root, ".native-toolchain/perry/external-tools.json"),
    "{}",
  );
  await assert.rejects(g.prepare(), /tools.rust.version/);
});

test("moves legacy target artifacts out of cached source without losing warm files", async (t) => {
  const f = await fixture(t);
  const prepared = await f.prepare();
  const target = path.join(prepared.root, "target");
  const build = path.join(f.root, ".native-toolchain/build");
  // Simulate the previous bootstrap layout, confined to this test fixture.
  await rm(target, { recursive: true, force: true });
  await rm(build, { recursive: true, force: true });
  await mkdir(target);
  await writeFile(path.join(target, "warm.a"), "keep compiled archive");
  const reused = await f.prepare();
  assert.equal(reused.env.CARGO_TARGET_DIR, target);
  assert.equal(path.resolve(prepared.root, await readlink(target)), build);
  assert.equal(
    await readFile(path.join(build, "warm.a"), "utf8"),
    "keep compiled archive",
  );
  await f.prepare();
  assert.equal(
    await readFile(path.join(target, "warm.a"), "utf8"),
    "keep compiled archive",
  );
});
