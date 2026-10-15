/* eslint-disable playwright/expect-expect -- node:test assertions use node:assert. */
import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { fs } from "zx";

import { normalizeSystemLibraries } from "./llvm-config-adapter.mjs";
import { resolveSystemLibraries } from "./prepare-linux-toolchain.mjs";

test("normalization replaces only complete configured tokens, preserving whitespace, flags and valid paths", () => {
  const mappings = [
    { original: "/foreign/libzstd.a", name: "zstd", file: "/host/libzstd.so" },
  ];
  assert.equal(
    normalizeSystemLibraries(
      '  -lzstd\t"/foreign/libzstd.a" /valid/libz.a /foreign/libzstd.a.extra -Wl,/foreign/libzstd.a\n',
      mappings,
    ),
    "  -lzstd\t-lzstd /valid/libz.a /foreign/libzstd.a.extra -Wl,/foreign/libzstd.a\n",
  );
  assert.equal(
    normalizeSystemLibraries("/foreign/libzstd.a\n", [
      { ...mappings[0], file: "/host/libzstd.a" },
    ]),
    "-l:libzstd.a\n",
  );
});

test("resolver validates host files, ignores valid absolute paths and flags, deduplicates missing libraries", async (t) => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "aio-system-libs-"));
  t.after(() => fs.remove(root));
  const valid = path.join(root, "libvalid.a");
  const host = path.join(root, "libzstd.so");
  await fs.writeFile(valid, "archive");
  await fs.writeFile(host, "shared");
  const calls = [];
  const result = await resolveSystemLibraries(
    `-lzstd "${valid}" /foreign/libzstd.a /foreign/libzstd.a`,
    {
      env: { CC: "host-cc" },
      run: async (command, args) => {
        calls.push({ command, args });
        return `${host}\n`;
      },
    },
  );
  assert.deepEqual(calls, [
    { command: "host-cc", args: ["-print-file-name=libzstd.so"] },
  ]);
  assert.equal(result.length, 1);
  assert.equal(result[0].file, host);
  assert.equal(result[0].original, "/foreign/libzstd.a");
  await assert.rejects(
    resolveSystemLibraries("/foreign/libzstd.a", {
      env: {},
      run: async (_command, args) => args[0].split("=")[1],
    }),
    /libzstd-devel/,
  );
});
