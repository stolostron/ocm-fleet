/* eslint-disable playwright/no-standalone-expect */
import { describe, expect, it, vi } from "vitest";

import { loadPluginCommand, pluginManifestLocation } from "../plugins/runtime";

describe("CLI plugin runtime", () => {
  it("resolves a plugin manifest under the injectable base URL", () => {
    expect(
      pluginManifestLocation("http://127.0.0.1:4000/plugins", "fixture"),
    ).toBe("http://127.0.0.1:4000/plugins/fixture-manifest.json");
  });

  it("rejects plugin names that could escape the configured asset path", () => {
    expect(() =>
      pluginManifestLocation("http://127.0.0.1/plugins/", "../fixture"),
    ).toThrow('Invalid plugin name "../fixture"');
  });

  it.each([null, undefined])(
    "rejects nullish command module result %s",
    async (result) => {
      const runtime = {
        getModule: vi.fn().mockResolvedValue(result),
        dispose: vi.fn(),
      };
      await expect(
        loadPluginCommand(runtime, "fixture", "command", false),
      ).rejects.toThrow(
        'Plugin "fixture" module "command" did not export a command component.',
      );
    },
  );

  it("retries a failed module load in the same runtime when requested", async () => {
    const command = () => null;
    const runtime = {
      getModule: vi
        .fn()
        .mockRejectedValueOnce(new Error("entry unavailable"))
        .mockResolvedValueOnce(command),
      dispose: vi.fn(),
    };

    await expect(
      loadPluginCommand(runtime, "fixture", "command", true),
    ).resolves.toBe(command);
    expect(runtime.getModule).toHaveBeenCalledTimes(2);
  });
});
