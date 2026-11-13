// @vitest-environment jsdom
import { removeScalprum } from "@scalprum/core";
import { useScalprum } from "@scalprum/react-core";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  type ConsoleConfig,
  ConsoleProvider,
  useConsoleConfig,
} from "../index";
import { transformManifest } from "../manifest";

const config: ConsoleConfig = {
  schemaVersion: 1,
  auth: { type: "none" },
  catalog: {
    pluginEntries: [
      {
        key: "test",
        pluginManifest: {
          name: "test-plugin",
          version: "1.0.0",
          registrationMethod: "local",
          extensions: [{ type: "test.feature", properties: { title: "Test" } }],
        },
      },
    ],
  },
};

let root: Root;
let element: HTMLDivElement;
beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.stubGlobal("__webpack_share_scopes__", { default: {} });
  element = document.createElement("div");
  document.body.appendChild(element);
  root = createRoot(element);
});
afterEach(async () => {
  await act(() => root.unmount());
  removeScalprum();
  element.remove();
  vi.unstubAllGlobals();
});

function State() {
  const state = useConsoleConfig();
  return createElement(
    "div",
    { id: "state" },
    state.error?.message ??
      (state.loading ? "loading" : state.config?.auth.type),
  );
}
function PluginHarness() {
  const { pluginStore } = useScalprum();
  return createElement(
    "div",
    { id: "plugin" },
    `${pluginStore
      .getPluginInfo()
      .map((p) => `${p.manifest.name}:${p.status}`)
      .join(",")}|${pluginStore
      .getExtensions()
      .map((extension) => extension.type)
      .join(",")}`,
  );
}
function View() {
  const state = useConsoleConfig();
  return createElement(
    "div",
    null,
    createElement(State),
    state.config && !state.loading && !state.error
      ? createElement(PluginHarness)
      : null,
  );
}
async function render(source: Parameters<typeof ConsoleProvider>[0]["source"]) {
  await act(async () => {
    root.render(
      createElement(ConsoleProvider, { source, children: createElement(View) }),
    );
  });
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("ConsoleProvider", () => {
  it("loads a static catalog into the plugin harness", async () => {
    await render({ type: "static", config });
    expect(element.querySelector("#state")?.textContent).toBe("none");
    expect(element.querySelector("#plugin")?.textContent).toBe(
      "test-plugin:loaded|test.feature",
    );
  });

  it.each([
    [{ ...config, schemaVersion: 42 }, /schemaVersion/i],
    [{ ...config, auth: undefined }, /auth/i],
  ])("exposes invalid configuration errors", async (invalid, message) => {
    await render({
      type: "static",
      config: invalid as unknown as ConsoleConfig,
    });
    expect(element.querySelector("#state")?.textContent).toMatch(message);
    expect(element.querySelector("#plugin")).toBeNull();
  });

  it("reports URL errors and pending loading", async () => {
    const request = deferred<Response>();
    vi.stubGlobal(
      "fetch",
      vi.fn(() => request.promise),
    );
    await render({ type: "url", url: "/config" });
    expect(element.querySelector("#state")?.textContent).toBe("loading");
    await act(async () => request.resolve(new Response("", { status: 503 })));
    expect(element.querySelector("#state")?.textContent).toMatch(/503/);
  });

  it("loads config from a URL", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify(config), { status: 200 }),
        ),
    );
    await render({ type: "url", url: "/config" });
    expect(element.querySelector("#plugin")?.textContent).toBe(
      "test-plugin:loaded|test.feature",
    );
  });

  it("wires the transform into the real plugin loader", async () => {
    const scripts: string[] = [];
    const append = vi
      .spyOn(document.head, "appendChild")
      .mockImplementation((node) => {
        if (node instanceof HTMLScriptElement) {
          scripts.push(node.src);
          queueMicrotask(() => node.dispatchEvent(new Event("error")));
        }
        return node;
      });
    try {
      await render({
        type: "static",
        config: {
          ...config,
          catalog: {
            pluginEntries: [
              {
                key: "a",
                pluginManifest: {
                  name: "a",
                  version: "1",
                  registrationMethod: "custom",
                  baseURL: "https://assets.example/a",
                  loadScripts: ["main.js"],
                  extensions: [],
                },
              },
              {
                key: "b",
                pluginManifest: {
                  name: "b",
                  version: "1",
                  registrationMethod: "custom",
                  baseURL: "https://assets.example/b",
                  loadScripts: ["https://cdn.example/entry.js"],
                  extensions: [],
                },
              },
            ],
          },
        },
      });
      expect(
        scripts.map((src) => new URL(src).origin + new URL(src).pathname),
      ).toEqual([
        "https://assets.example/a/main.js",
        "https://cdn.example/entry.js",
      ]);
      expect(element.querySelector("#state")?.textContent).toMatch(/failed/);
    } finally {
      append.mockRestore();
    }
  });

  it("reports plugin load failures instead of rendering success", async () => {
    await render({
      type: "static",
      config: {
        ...config,
        catalog: {
          pluginEntries: [
            {
              key: "bad",
              pluginManifest: {
                name: "bad-plugin",
                version: "1",
                registrationMethod: "custom",
                baseURL: "/assets",
                loadScripts: [],
                extensions: [],
              },
            },
          ],
        },
      },
    });
    expect(element.querySelector("#state")?.textContent).toMatch(
      /Plugin bad-plugin failed/,
    );
  });

  it("rejects duplicate plugin keys and public auth secrets", async () => {
    await render({
      type: "static",
      config: {
        ...config,
        catalog: {
          pluginEntries: [
            config.catalog.pluginEntries[0],
            config.catalog.pluginEntries[0],
          ],
        },
      },
    });
    expect(element.querySelector("#state")?.textContent).toMatch(/unique/);
    await render({
      type: "static",
      config: {
        ...config,
        auth: { type: "none", clientSecret: "secret" } as ConsoleConfig["auth"],
      },
    });
    expect(element.querySelector("#state")?.textContent).toMatch(/auth/);
    expect(element.querySelector("#state")?.textContent).not.toContain(
      "secret",
    );
  });

  it("uses the latest factory result and ignores stale completions", async () => {
    const first = deferred<ConsoleConfig>();
    const second = deferred<ConsoleConfig>();
    await render({ type: "factory", load: () => first.promise });
    await render({ type: "factory", load: () => second.promise });
    await act(async () =>
      second.resolve({
        ...config,
        auth: {
          type: "oidc",
          authority: "https://issuer.example",
          clientId: "ui",
        },
      }),
    );
    await act(async () => first.resolve(config));
    expect(element.querySelector("#state")?.textContent).toBe("oidc");
  });

  it("reports factory failures", async () => {
    await render({
      type: "factory",
      load: () => Promise.reject(new Error("factory failed")),
    });
    expect(element.querySelector("#state")?.textContent).toMatch(
      /factory failed/,
    );
  });

  it("fails closed when a loaded catalog changes", async () => {
    await render({ type: "static", config });
    await render({
      type: "static",
      config: { ...config, catalog: { pluginEntries: [] } },
    });
    expect(element.querySelector("#state")?.textContent).toMatch(
      /requires a page reload/,
    );
  });
});

describe("manifest transform", () => {
  it("resolves each plugin's assets and preserves absolute script URLs", () => {
    const manifest = {
      name: "a",
      version: "1",
      registrationMethod: "custom" as const,
      baseURL: "https://assets.example/a/",
      extensions: [],
      loadScripts: [
        "main.js",
        "/chunk.js",
        "https://other.example/remote.js",
        "//cdn.example/a.js",
      ],
    };
    expect(transformManifest(manifest).loadScripts).toEqual([
      "https://assets.example/a/main.js",
      "https://assets.example/a/chunk.js",
      "https://other.example/remote.js",
      "//cdn.example/a.js",
    ]);
    expect(
      transformManifest({ ...manifest, baseURL: "https://assets.example/b" })
        .loadScripts[0],
    ).toBe("https://assets.example/b/main.js");
    expect(
      transformManifest({ ...manifest, baseURL: "/app/plugin" }).loadScripts[0],
    ).toBe(new URL("/app/plugin/main.js", document.baseURI).href);
    expect(manifest.loadScripts[0]).toBe("main.js");
  });
});
