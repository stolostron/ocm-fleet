/* eslint playwright/expect-expect: ["error", { "assertFunctionNames": ["equal", "rejects"] }] */
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  buildGolangciLint,
  toolEnvironment,
  updateGolangciLint,
} from "./golangci-lint.mjs";

test("CodeQL's application compiler can load the linter module", async () => {
  const env = await toolEnvironment(
    fileURLToPath(new URL("../server/", import.meta.url)),
  );
  const packages = execFileSync(
    "go",
    ["list", "-mod=readonly", "-deps", "./..."],
    {
      cwd: fileURLToPath(new URL("./golangci-lint/", import.meta.url)),
      env,
      encoding: "utf8",
    },
  ).split("\n");
  assert.equal(
    packages.includes("github.com/stolostron/ocm-fleet/tools/golangci-lint"),
    true,
  );
});

test("builds a real module for the host with the module's compiler pin", async (t) => {
  const options = await fixture(t);
  const previous = process.env.GOFLAGS;
  process.env.GOFLAGS = "-invalid-ambient-flag";
  t.after(() => {
    if (previous === undefined) delete process.env.GOFLAGS;
    else process.env.GOFLAGS = previous;
  });
  const env = await toolEnvironment(options.directory);
  assert.equal(env.GOTOOLCHAIN, "go1.26.6");
  assert.equal(env.GOWORK, "off");
  assert.equal(env.CGO_ENABLED, "0");
  await buildGolangciLint(options);
  assert.equal(execFileSync(options.output, { encoding: "utf8" }), "fixture\n");
  assert.equal(await readFile(join(options.directory, "go.sum"), "utf8"), "");
});

test("failed compilation preserves the last working executable", async (t) => {
  const options = await fixture(t);
  await buildGolangciLint(options);
  await writeFile(
    join(options.directory, "main.go"),
    "package main\nnot valid Go\n",
  );
  await assert.rejects(buildGolangciLint(options));
  assert.equal(execFileSync(options.output, { encoding: "utf8" }), "fixture\n");
});

test("ordinary builds reject missing checksums without changing module files", async (t) => {
  const options = await fixture(t);
  const path = join(options.directory, "go.mod");
  const source =
    (await readFile(path, "utf8")) + "\nrequire golang.org/x/mod v0.42.0\n";
  await writeFile(path, source);
  await writeFile(
    join(options.directory, "main.go"),
    'package main\nimport "golang.org/x/mod/semver"\nfunc main() { println(semver.IsValid("v1.0.0")) }\n',
  );
  await assert.rejects(buildGolangciLint(options));
  assert.equal(await readFile(path, "utf8"), source);
  assert.equal(await readFile(join(options.directory, "go.sum"), "utf8"), "");
});

test("updates require an explicit stable v2 release", async () => {
  for (const version of [
    undefined,
    "latest",
    "main",
    "v2.16.0-rc1",
    "v1.64.0",
    "v2.01.0",
    "v2.15.0; echo bad",
  ]) {
    await assert.rejects(
      updateGolangciLint({ version, directory: "/missing-module" }),
      /stable v2 version/,
    );
  }
});

test("failed dependency resolution preserves both module files", async (t) => {
  const options = await fixture(t);
  const previous = process.env.GOPROXY;
  process.env.GOPROXY = "off";
  t.after(() => {
    if (previous === undefined) delete process.env.GOPROXY;
    else process.env.GOPROXY = previous;
  });
  const original = await readFile(join(options.directory, "go.mod"), "utf8");
  await assert.rejects(
    updateGolangciLint({
      version: "v2.999999.0",
      directory: options.directory,
    }),
  );
  assert.equal(
    await readFile(join(options.directory, "go.mod"), "utf8"),
    original,
  );
  assert.equal(await readFile(join(options.directory, "go.sum"), "utf8"), "");
});

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), "ocm-fleet-tool-build-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await writeFile(
    join(directory, "go.mod"),
    "module lintfixture\n\ngo 1.26.0\n\ntoolchain go1.26.6\n",
  );
  await writeFile(join(directory, "go.sum"), "");
  await writeFile(
    join(directory, "main.go"),
    'package main\nimport "fmt"\nfunc main() { fmt.Println("fixture") }\n',
  );
  return { directory, output: join(directory, "bin", "fixture") };
}
