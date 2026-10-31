import assert from "node:assert/strict";
import {
  mkdtemp,
  mkdir,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  createPerryBuildEnvironment,
  preparePerryBuild,
  perryCompilerInvocation,
} from "./build-perry.mjs";

// eslint-disable-next-line playwright/expect-expect -- node:test assertions use node:assert.
test("Linux runs prepared source compiler; macOS runs npm Node wrapper", () => {
  const require = { resolve: () => "/npm/perry.js" };
  assert.deepEqual(
    perryCompilerInvocation(
      { compiler: "/managed/compiler/perry" },
      require,
      "linux",
    ),
    { command: "/managed/compiler/perry", args: [] },
  );
  assert.deepEqual(perryCompilerInvocation({}, require, "darwin"), {
    command: process.execPath,
    args: ["/npm/perry.js"],
  });
  assert.throws(
    () => perryCompilerInvocation({}, require, "linux"),
    /prepared.*compiler/,
  );
});

// eslint-disable-next-line playwright/expect-expect -- This node:test uses node:assert.
test("Perry environment isolates Rust homes and required compiler flags", () => {
  const env = createPerryBuildEnvironment(
    {
      HOME: "/home/tester",
      PATH: "/usr/bin",
      PERRY_NO_AUTO_OPTIMIZE: "1",
      PERRY_NO_TELEMETRY: "0",
      CARGO_HOME: "/unrelated/cargo",
      RUSTUP_HOME: "/unrelated/rustup",
      PERRY_BUILD_COMMIT: "forged",
      GIT_DIR: "/wrong/git",
      GIT_WORK_TREE: "/wrong/source",
      GIT_INDEX_FILE: "/wrong/index",
    },
    "/project/.native-toolchain/perry",
    "nightly-2026-08-20",
  );

  assert.equal(env.CARGO_HOME, "/project/.native-toolchain/cargo");
  assert.equal(env.RUSTUP_HOME, "/project/.native-toolchain/rustup");
  assert.equal(env.RUSTUP_TOOLCHAIN, "nightly-2026-08-20");
  assert.equal(env.PERRY_WORKSPACE_ROOT, "/project/.native-toolchain/perry");
  assert.equal(env.PERRY_NO_TELEMETRY, "1");
  assert.equal(env.PERRY_NO_AUTO_OPTIMIZE, undefined);
  assert.equal(env.PERRY_BUILD_COMMIT, undefined);
  assert.equal(
    Object.keys(env).some((key) => key.startsWith("GIT_")),
    false,
  );
  assert.equal(
    env.PATH.split(path.delimiter)[0],
    "/project/.native-toolchain/cargo/bin",
  );
});

// eslint-disable-next-line playwright/expect-expect -- This node:test uses node:assert.
test("staged Ink resolves local Yoga while hoisted Ink keeps upstream Yoga", async () => {
  const workspace = await realpath(
    await mkdtemp(path.join(os.tmpdir(), "aio-perry-resolution-")),
  );
  const root = path.join(workspace, "client/aio");
  async function fixture(relative, contents) {
    const file = path.join(workspace, relative);
    await mkdir(path.dirname(file), { recursive: true });
    await writeFile(file, contents);
  }
  try {
    await fixture("client/aio/package.json", '{"type":"module"}');
    await fixture("client/aio/src/main.tsx", 'import "ink";');
    await fixture(
      "client/aio/src/native/yoga-layout/package.json",
      '{"name":"yoga-layout","main":"./index.ts","exports":"./index.ts"}',
    );
    await fixture(
      "client/aio/src/native/yoga-layout/index.ts",
      "export default {};",
    );
    await fixture(
      "node_modules/ink/package.json",
      '{"name":"ink","exports":"./build/index.js"}',
    );
    await fixture("node_modules/ink/build/index.js", 'import "yoga-layout";');
    // Nested upstream Yoga must not shadow the staged replacement either.
    await fixture(
      "node_modules/ink/node_modules/yoga-layout/index.js",
      "upstream nested Yoga",
    );
    await fixture("node_modules/yoga-layout/index.js", "upstream Yoga");
    await fixture("node_modules/react/index.js", "upstream React");

    const entry = await preparePerryBuild(root);
    const inkEntry = createRequire(entry).resolve("ink");
    const fromInk = createRequire(inkEntry);
    assert.equal(
      fromInk.resolve("yoga-layout"),
      path.join(root, "dist/perry/node_modules/yoga-layout/index.ts"),
    );
    assert.equal(
      fromInk.resolve("react"),
      path.join(workspace, "node_modules/react/index.js"),
    );
    const upstreamInk = createRequire(path.join(root, "package.json")).resolve(
      "ink",
    );
    assert.equal(
      createRequire(upstreamInk).resolve("yoga-layout"),
      path.join(
        workspace,
        "node_modules/ink/node_modules/yoga-layout/index.js",
      ),
    );
    assert.equal(await readFile(upstreamInk, "utf8"), 'import "yoga-layout";');
    assert.equal(
      await readFile(
        path.join(workspace, "node_modules/yoga-layout/index.js"),
        "utf8",
      ),
      "upstream Yoga",
    );

    await fixture("client/aio/dist/perry/src/removed.ts", "stale source");
    await fixture("client/aio/src/main.tsx", 'import "ink"; // changed');
    await preparePerryBuild(root);
    assert.equal(await readFile(entry, "utf8"), 'import "ink"; // changed');
    await assert.rejects(
      readFile(path.join(root, "dist/perry/src/removed.ts")),
      { code: "ENOENT" },
    );
  } finally {
    await rm(workspace, { recursive: true, force: true });
  }
});
