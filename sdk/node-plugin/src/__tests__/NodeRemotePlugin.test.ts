/* eslint-disable playwright/no-standalone-expect -- These are Vitest tests. */
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
});
