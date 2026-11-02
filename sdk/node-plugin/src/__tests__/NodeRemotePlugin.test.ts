import rspack from "@rspack/core";
import { describe, expect, it } from "vitest";

import { NodeRemotePlugin } from "../NodeRemotePlugin.ts";

const validOptions = {
  pluginMetadata: {
    name: "example",
    version: "1.0.0",
    exposedModules: { Page: "./src/page.ts" },
  },
  extensions: [],
};

describe("NodeRemotePlugin options", () => {
  it("requires SDK-shaped plugin metadata and exposed modules", () => {
    expect(() => new NodeRemotePlugin({} as never)).toThrow(/pluginMetadata/);
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          pluginMetadata: { ...validOptions.pluginMetadata, name: " " },
        }),
    ).toThrow(/pluginMetadata.name/);
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          pluginMetadata: { name: "example", version: "1.0.0" },
        }),
    ).toThrow(/at least one module/);
  });

  it("rejects invalid or ambiguous producer filenames and module names", () => {
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          entryScriptFilename: "remoteEntry.[contenthash].cjs",
        }),
    ).toThrow(/templates are unsupported/);
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          pluginMetadata: {
            ...validOptions.pluginMetadata,
            exposedModules: {
              Page: "./src/page.ts",
              "./Page": "./src/other.ts",
            },
          },
        }),
    ).toThrow(/Duplicate Node remote exposed module/);
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          pluginMetadata: {
            ...validOptions.pluginMetadata,
            exposedModules: { Page: " " },
          },
        }),
    ).toThrow(/names and paths/);
  });

  it("rejects browser targets with a clear Node target requirement", () => {
    const plugin = new NodeRemotePlugin(validOptions);

    expect(() => plugin.apply({ options: { target: "web" } } as never)).toThrow(
      /requires Rspack target/,
    );
  });

  it.each([
    [{ optimization: { runtimeChunk: "single" } }, /runtimeChunk: false/],
    [
      { optimization: { runtimeChunk: { name: "runtime" } } },
      /runtimeChunk: false/,
    ],
    [{ output: { module: true } }, /CommonJS output/],
    [{ output: { filename: "[name].js" } }, /end in .cjs/],
    [{ output: { chunkFilename: "[name].js" } }, /end in .cjs/],
    [{ output: { chunkLoading: "require" } }, /async-node chunkLoading/],
    [{ output: { chunkLoading: false } }, /async-node chunkLoading/],
    [{ output: { chunkFormat: "module" } }, /commonjs chunkFormat/],
    [{ output: { chunkFormat: false } }, /commonjs chunkFormat/],
  ] as const)(
    "rejects incompatible compiler options %j",
    (options, message) => {
      expect(() =>
        rspack({
          target: "async-node",
          entry: {},
          ...options,
          plugins: [new NodeRemotePlugin(validOptions)],
        }),
      ).toThrow(message);
    },
  );

  it("rejects non-CommonJS remote entry extensions", () => {
    expect(
      () =>
        new NodeRemotePlugin({
          ...validOptions,
          entryScriptFilename: "remoteEntry.js",
        }),
    ).toThrow(/must end in .cjs/);
  });
});
