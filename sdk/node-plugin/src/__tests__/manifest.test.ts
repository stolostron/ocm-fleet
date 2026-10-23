/* eslint-disable playwright/no-standalone-expect -- These are Vitest tests. */
import { describe, expect, it } from "vitest";

import { createNodeRemoteManifest, identifyRemoteEntry } from "../manifest.ts";

describe("Node remote manifest", () => {
  it("preserves SDK metadata and identifies the entry independently of order", () => {
    const extensions = [
      {
        type: "example.command",
        properties: { id: "inspect", nested: { labels: ["safe", "opaque"] } },
      },
    ];
    const manifest = createNodeRemoteManifest({
      name: "sample",
      version: "1.0.0",
      remoteEntryFilename: "remoteEntry.cjs",
      loadScripts: ["remoteEntry.cjs", "runtime.js", "chunks/exposed.js"],
      extensions,
    });

    expect(manifest.registrationMethod).toBe("custom");
    expect(manifest.baseURL).toBe("auto");
    expect(manifest.extensions).toEqual(extensions);
    expect(manifest.remoteEntry).toBe("remoteEntry.cjs");
    expect(manifest.runtimeDependencies).toEqual([
      "runtime.js",
      "chunks/exposed.js",
    ]);
    expect(manifest.loadScripts).toEqual([
      "runtime.js",
      "chunks/exposed.js",
      "remoteEntry.cjs",
    ]);
    expect(
      identifyRemoteEntry(manifest.loadScripts, manifest.remoteEntry),
    ).toBe("remoteEntry.cjs");
  });

  it("preserves host-transformed absolute paths and query strings", () => {
    expect(
      identifyRemoteEntry(
        [
          "https://assets.example/crd/runtime.js?v=17",
          "/crd/plugins/sample/remoteEntry.cjs?sha=abc",
        ],
        "plugins/sample/remoteEntry.cjs",
      ),
    ).toBe("/crd/plugins/sample/remoteEntry.cjs?sha=abc");
  });

  it("rejects missing and ambiguous entry assets", () => {
    expect(() =>
      identifyRemoteEntry(["runtime.js"], "remoteEntry.cjs"),
    ).toThrow(/not present/);
    expect(() =>
      identifyRemoteEntry(
        ["plugins/a/remoteEntry.cjs", "plugins/b/remoteEntry.cjs"],
        "remoteEntry.cjs",
      ),
    ).toThrow(/ambiguous/);
  });

  it("rejects a manifest with no matching emitted entry asset", () => {
    expect(() =>
      createNodeRemoteManifest({
        name: "sample",
        version: "1.0.0",
        remoteEntryFilename: "missing.cjs",
        loadScripts: ["runtime.js"],
        extensions: [],
      }),
    ).toThrow(/not present/);
  });
});
