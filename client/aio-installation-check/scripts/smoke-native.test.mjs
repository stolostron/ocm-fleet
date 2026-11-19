import assert from "node:assert/strict";
import { readdir } from "node:fs/promises";

import test from "node:test";

import {
  REQUIRED_MARKERS,
  createIsolatedEnvironment,
  elfArchToken,
  runNativeSmoke,
} from "./smoke-native.mjs";

const binary = "dist/native/aio-installation-check";

function fakeChild({ status = 0, stdout = "", stderr = "" } = {}) {
  return { status, stdout, stderr };
}

function fakeSpawner(handlers) {
  const calls = [];
  const spawnChild = (command, args, options) => {
    calls.push({ command, args, options });
    const handler = handlers[command];
    if (!handler) throw new Error(`unexpected spawn: ${command}`);
    return handler(args, options);
  };
  return { calls, spawnChild };
}

function isolatedEnvironment(cleanup) {
  return {
    environment: {
      PATH: "/isolated-empty",
      NO_COLOR: "1",
      HOME: "/home/runner",
    },
    cleanup,
  };
}

// Minimal call recorder for isolated-environment cleanup verification.
function cleanupSpy() {
  const calls = [];
  const fn = () => calls.push(1);
  fn.assert = () => assert.equal(calls.length, 1);
  return fn;
}

function happyHandlers({
  stdout = REQUIRED_MARKERS.join("\n"),
  file = "ELF 64-bit LSB pie executable, x86-64, version 1 (SYSV)",
  exit = 0,
} = {}) {
  return {
    "/bin/sh": () => fakeChild({ status: 1 }),
    file: () => fakeChild({ stdout: file }),
    [binary]: (_args, options) => {
      assert.equal(options.env.PATH, "/isolated-empty");
      return fakeChild({ status: exit, stdout });
    },
  };
}

test("isolated environment uses a private empty PATH and cleans it up", async () => {
  const { environment, cleanup } = await createIsolatedEnvironment({
    HOME: "/home/runner",
  });

  try {
    assert.equal(environment.NO_COLOR, "1");
    assert.equal(environment.HOME, "/home/runner");
    assert.match(environment.PATH, /aio-native-smoke-/);
    // No Node.js and no runtime clients can resolve from an empty directory.
    assert.deepEqual(await readdir(environment.PATH), []);
  } finally {
    await cleanup();
  }
  await assert.rejects(() => readdir(environment.PATH), { code: "ENOENT" });
});

test("ELF arch tokens map build-host architectures", () => {
  assert.equal(elfArchToken("x86_64"), "x86-64");
  assert.equal(elfArchToken("amd64"), "x86-64");
  assert.equal(elfArchToken("aarch64"), "aarch64");
  assert.equal(elfArchToken("arm64"), "aarch64");
  assert.throws(() => elfArchToken("mips"), /unsupported host architecture/);
});

test("smoke passes for a matching ELF executable with no-clients output", async () => {
  const { calls, spawnChild } = fakeSpawner(happyHandlers());
  const cleanup = cleanupSpy();

  const summary = await runNativeSmoke({
    binaryPath: binary,
    arch: "x86_64",
    exists: () => true,
    createEnvironment: () => isolatedEnvironment(cleanup),
    spawnChild,
  });

  assert.equal(summary.exitCode, 0);
  assert.equal(summary.arch, "x86_64");
  assert.deepEqual(summary.markers, REQUIRED_MARKERS);
  const run = calls.find((call) => call.command === binary);
  assert.deepEqual(run.args, []);
  assert.equal(run.options.input, "");
  const probe = calls.find((call) => call.command === "/bin/sh");
  assert.equal(probe.options.env.PATH, "/isolated-empty");
  // Host inspection runs on the normal inherited PATH, not the isolated one.
  const describe = calls.find((call) => call.command === "file");
  assert.deepEqual(describe.args, ["-b", binary]);
  assert.equal(describe.options, undefined);
  cleanup.assert();
});

test("smoke resolves the host architecture through uname", async () => {
  const { spawnChild } = fakeSpawner({
    ...happyHandlers(),
    uname: () => fakeChild({ stdout: "x86_64\n" }),
  });

  const summary = await runNativeSmoke({
    binaryPath: binary,
    exists: () => true,
    createEnvironment: () => isolatedEnvironment(cleanupSpy()),
    spawnChild,
  });

  assert.equal(summary.arch, "x86_64");
});

test("smoke fails when Node.js stays reachable in the isolated environment", async () => {
  const { spawnChild } = fakeSpawner({
    "/bin/sh": () => fakeChild({ status: 0, stdout: "/isolated-empty/node\n" }),
  });

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /Node\.js is reachable/,
  );
});

test("smoke fails for a non-ELF executable", async () => {
  const { spawnChild } = fakeSpawner(
    happyHandlers({ file: "Mach-O 64-bit arm64 executable" }),
  );

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        arch: "x86_64",
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /Mach-O 64-bit/,
  );
});

test("smoke fails when the executable architecture differs from the host", async () => {
  const { spawnChild } = fakeSpawner(
    happyHandlers({ file: "ELF 64-bit LSB pie executable, aarch64" }),
  );

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        arch: "x86_64",
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /does not match the build host/,
  );
});

test("smoke fails when the no-clients guidance is missing", async () => {
  const { spawnChild } = fakeSpawner(
    happyHandlers({
      stdout: REQUIRED_MARKERS.filter(
        (marker) => marker !== "No usable Podman or Docker client detected",
      ).join("\n"),
    }),
  );

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        arch: "x86_64",
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /missing "No usable Podman or Docker client detected"/,
  );
});

test("smoke fails when the native executable exits nonzero", async () => {
  const { spawnChild } = fakeSpawner(happyHandlers({ exit: 1 }));

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        arch: "x86_64",
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /exited with status 1/,
  );
});

test("smoke fails with build guidance when the executable is missing", async () => {
  const { spawnChild } = fakeSpawner(happyHandlers());

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        exists: () => false,
        createEnvironment: () => isolatedEnvironment(cleanupSpy()),
        spawnChild,
      }),
    /build-native/,
  );
});

test("smoke cleans up the isolated environment when a check fails", async () => {
  const { spawnChild } = fakeSpawner(happyHandlers({ exit: 1 }));
  const cleanup = cleanupSpy();

  await assert.rejects(
    () =>
      runNativeSmoke({
        binaryPath: binary,
        arch: "x86_64",
        exists: () => true,
        createEnvironment: () => isolatedEnvironment(cleanup),
        spawnChild,
      }),
    /exited with status 1/,
  );
  cleanup.assert();
});
