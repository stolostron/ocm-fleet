/* eslint playwright/expect-expect: ["error", { "assertFunctionNames": ["equal", "deepEqual", "ok", "rejects"] }] */
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import {
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { promisify } from "node:util";
import { installGolangciLint } from "./install-golangci-lint.mjs";
import { serveFixtures } from "./test-support/http-server.mjs";

const run = promisify(execFile);

test("installs a verified release and reuses it without another download", async (t) => {
  const { release, requests, options } = await fixture(t);
  const pinned = await release();
  const binary = await installGolangciLint({ ...options, release: pinned });

  assert.equal(binary, join(options.installDir, "golangci-lint"));
  assert.equal(await readFile(binary, "utf8"), "fixture linter\n");
  assert.ok((await stat(binary)).mode & 0o111);
  assert.equal(
    await installGolangciLint({ ...options, release: pinned }),
    binary,
  );
  assert.equal(requests.length, 1);
});

test("repairs a changed or missing binary", async (t) => {
  const { release, requests, options } = await fixture(t);
  const pinned = await release();
  const binary = await installGolangciLint({ ...options, release: pinned });
  await writeFile(binary, "changed binary");
  await installGolangciLint({ ...options, release: pinned });
  assert.equal(await readFile(binary, "utf8"), "fixture linter\n");

  await rm(binary);
  await installGolangciLint({ ...options, release: pinned });
  assert.equal(await readFile(binary, "utf8"), "fixture linter\n");
  assert.equal(requests.length, 3);
});

test("replaces an installed release when the pin changes", async (t) => {
  const { release, requests, options } = await fixture(t);
  await installGolangciLint({ ...options, release: await release() });
  const binary = await installGolangciLint({
    ...options,
    release: await release("2.0.0", "updated linter\n"),
  });

  assert.equal(await readFile(binary, "utf8"), "updated linter\n");
  assert.equal(requests.length, 2);
});

test("does not reuse an old release when only the pinned version changes", async (t) => {
  const { release, requests, options } = await fixture(t);
  const previousRelease = await release();
  const binary = await installGolangciLint({
    ...options,
    release: previousRelease,
  });
  const nextRelease = await release("2.0.0", "updated linter\n");
  nextRelease.sha256 = previousRelease.sha256;

  await assert.rejects(
    installGolangciLint({ ...options, release: nextRelease }),
    /checksum mismatch/i,
  );
  assert.equal(await readFile(binary, "utf8"), "fixture linter\n");
  assert.equal(requests.length, 2);
});

test("rejects a checksum mismatch and preserves the installed binary", async (t) => {
  const { release, options } = await fixture(t);
  const binary = await installGolangciLint({
    ...options,
    release: await release(),
  });
  const nextRelease = await release("2.0.0");
  nextRelease.sha256["linux-amd64"] = "0".repeat(64);

  await assert.rejects(
    installGolangciLint({ ...options, release: nextRelease }),
    /checksum mismatch/i,
  );
  assert.equal(await readFile(binary, "utf8"), "fixture linter\n");
  assert.deepEqual((await readdir(options.installDir)).sort(), [
    ".golangci-lint.json",
    "golangci-lint",
  ]);
});

test("reports failed downloads and removes incomplete installation files", async (t) => {
  const { options } = await fixture(t);
  await assert.rejects(
    installGolangciLint({
      ...options,
      release: { version: "1.0.0", sha256: { "linux-amd64": "0".repeat(64) } },
    }),
    /404/,
  );
  assert.deepEqual(await readdir(options.installDir), []);
});

test("rejects unsupported platforms before downloading", async (t) => {
  const { release, requests, options } = await fixture(t);
  await assert.rejects(
    installGolangciLint({
      ...options,
      release: await release(),
      platform: "win32",
    }),
    /unsupported platform.*win32/i,
  );
  assert.equal(requests.length, 0);
});

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), "golangci-lint-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const { responses: archives, requests, baseUrl } = await serveFixtures(t);

  async function release(version = "1.0.0", binary = "fixture linter\n") {
    const name = `golangci-lint-${version}-linux-amd64`;
    await mkdir(join(directory, name));
    await writeFile(join(directory, name, "golangci-lint"), binary);
    const archivePath = join(directory, `${name}.tar.gz`);
    await run("tar", ["-czf", archivePath, "-C", directory, name]);
    const archive = await readFile(archivePath);
    archives.set(`/v${version}/${name}.tar.gz`, archive);
    return {
      version,
      sha256: {
        "linux-amd64": createHash("sha256").update(archive).digest("hex"),
      },
    };
  }

  return {
    release,
    requests,
    options: {
      installDir: join(directory, "bin"),
      platform: "linux",
      arch: "x64",
      releaseBaseUrl: baseUrl,
    },
  };
}
