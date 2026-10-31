/* eslint playwright/no-standalone-expect: off -- Vitest unit tests. */
import { describe, expect, it, vi } from "vitest";

import type { CheckDependencies } from "./dependencies.js";
import { checkKindSocket } from "./kind.js";
import { discoverRuntime } from "./runtime-discovery.js";

const hostSocket = "/var/folders/example/podman-api.sock";
const rootlessSocket = "/run/user/1234/podman/podman.sock";
const rootSocket = "/run/podman/podman.sock";

function fixture({
  root = false,
  mountExists = true,
  infoPath = root ? rootSocket : rootlessSocket,
  env = {},
  platform = "darwin",
  port = 54321,
}: {
  root?: boolean;
  mountExists?: boolean;
  infoPath?: string;
  env?: Partial<CheckDependencies["env"]>;
  platform?: string;
  port?: number;
} = {}) {
  const uri = `ssh://${root ? "root" : "core"}@127.0.0.1:54321${root ? rootSocket : rootlessSocket}`;
  const runCommand = vi.fn(async (_command: string, args: string[]) => {
    let value: unknown;
    switch (args.join(" ")) {
      case "--version":
        return { exitCode: 0, stdout: "podman version 5.8.1", stderr: "" };
      case "version --format json":
        value = { Client: { Version: "5.8.1" } };
        break;
      case "info --format json":
        value = {
          host: {
            remoteSocket: { path: infoPath ? `unix://${infoPath}` : "" },
          },
        };
        break;
      case "system connection list --format json":
        value = [
          {
            Name: root ? "chosen-root" : "chosen",
            URI: platform === "linux" ? `unix://${rootlessSocket}` : uri,
            Default: true,
            IsMachine: platform === "darwin",
            Identity: "/keys/chosen",
          },
        ];
        break;
      case "machine list --format json":
        value = [{ Name: "unrelated" }, { Name: "chosen" }];
        break;
      case "machine inspect unrelated":
        value = [
          {
            Name: "unrelated",
            SSHConfig: { Port: 12345, IdentityPath: "/keys/other" },
            ConnectionInfo: { PodmanSocket: { Path: "/wrong.sock" } },
            Rootful: root,
          },
        ];
        break;
      case "machine inspect chosen":
        value = [
          {
            Name: "chosen",
            SSHConfig: { Port: port, IdentityPath: "/keys/chosen" },
            ConnectionInfo: { PodmanSocket: { Path: hostSocket } },
            Rootful: root,
          },
        ];
        break;
      default:
        return {
          exitCode:
            args[0] === "machine" && args[1] === "ssh" && mountExists ? 0 : 1,
          stdout: "",
          stderr: "",
        };
    }
    return { exitCode: 0, stdout: JSON.stringify(value), stderr: "" };
  });
  return {
    platform,
    env: {
      CONTAINER_HOST: "",
      DOCKER_HOST: "",
      DOCKER_CONTEXT: "",
      XDG_RUNTIME_DIR: "",
      ...env,
    },
    runCommand,
    probeUnixSocket: vi.fn(async () => ({ isSocket: true, responds: true })),
    isPortAvailable: vi.fn(async () => true),
  } satisfies CheckDependencies;
}

describe("Kind socket address spaces", () => {
  it.each([false, true])(
    "maps selected rootful=%s connection to its actual machine and engine socket",
    async (root) => {
      const dependencies = fixture({ root });
      const discovery = await discoverRuntime("podman", dependencies);
      expect(discovery).toMatchObject({
        local: true,
        hostSocketPath: hostSocket,
        engineSocketPath: root ? rootSocket : rootlessSocket,
        podmanMachineName: "chosen",
      });
      expect((await checkKindSocket(discovery, dependencies)).status).toBe(
        "pass",
      );
      expect(dependencies.probeUnixSocket).toHaveBeenCalledWith(hostSocket);
      expect(dependencies.runCommand).toHaveBeenCalledWith("podman", [
        "machine",
        "ssh",
        "chosen",
        `test -S '${root ? rootSocket : rootlessSocket}'`,
      ]);
    },
  );

  it("blocks Kind when host ping passes but VM mount socket is absent", async () => {
    const dependencies = fixture({ mountExists: false });
    const discovery = await discoverRuntime("podman", dependencies);
    expect(await checkKindSocket(discovery, dependencies)).toMatchObject({
      status: "error",
      blocking: true,
      reason: expect.stringContaining("engine"),
    });
    expect(dependencies.probeUnixSocket).toHaveBeenCalledWith(hostSocket);
  });

  it("normalizes Linux unix prefix for both probe and mount", async () => {
    const dependencies = fixture({ platform: "linux" });
    expect(await discoverRuntime("podman", dependencies)).toMatchObject({
      hostSocketPath: rootlessSocket,
      engineSocketPath: rootlessSocket,
    });
  });

  it.each([false, true])(
    "uses Linux info fallback only for a native engine (serviceIsRemote=%s)",
    async (remote) => {
      const dependencies = fixture({ platform: "linux" });
      const command = dependencies.runCommand;
      dependencies.runCommand = vi.fn(
        async (executable: string, args: string[]) => {
          if (args[0] === "info")
            return {
              exitCode: 0,
              stdout: JSON.stringify({
                host: {
                  serviceIsRemote: remote,
                  remoteSocket: { path: `unix://${rootlessSocket}` },
                },
              }),
              stderr: "",
            };
          if (args[0] === "system")
            return { exitCode: 0, stdout: "[]", stderr: "" };
          return command(executable, args);
        },
      );
      const discovery = await discoverRuntime("podman", dependencies);
      expect(discovery.local).toBe(!remote);
      expect(discovery.engineSocketPath).toBe(
        remote ? undefined : rootlessSocket,
      );
      expect(discovery.hostSocketPath).toBe(
        remote ? undefined : rootlessSocket,
      );
    },
  );

  it("refuses non-loopback SSH even when connection claims IsMachine", async () => {
    const dependencies = fixture();
    const command = dependencies.runCommand;
    dependencies.runCommand = vi.fn(
      async (executable: string, args: string[]) => {
        if (args[0] === "system")
          return {
            exitCode: 0,
            stdout: JSON.stringify([
              {
                Name: "remote",
                URI: `ssh://core@remote.example:54321${rootlessSocket}`,
                Identity: "/keys/chosen",
                Default: true,
                IsMachine: true,
              },
            ]),
            stderr: "",
          };
        return command(executable, args);
      },
    );
    expect(await discoverRuntime("podman", dependencies)).toMatchObject({
      local: false,
    });
    expect(dependencies.runCommand).not.toHaveBeenCalledWith("podman", [
      "machine",
      "inspect",
      "chosen",
    ]);
  });

  it("blocks missing engine path instead of using forwarded host path", async () => {
    const dependencies = fixture({ infoPath: "" });
    const discovery = await discoverRuntime("podman", dependencies);
    expect(discovery.engineSocketPath).toBeUndefined();
    expect((await checkKindSocket(discovery, dependencies)).status).toBe(
      "error",
    );
  });

  it("refuses machine identity mismatch rather than picking first machine", async () => {
    const discovery = await discoverRuntime("podman", fixture({ port: 9999 }));
    expect(discovery.local).toBe(false);
    expect(discovery.engineSocketPath).toBeUndefined();
  });

  it("does not let mount override make remote endpoint local", async () => {
    const discovery = await discoverRuntime(
      "podman",
      fixture({
        env: {
          CONTAINER_HOST: "ssh://core@remote.example/run/podman/podman.sock",
          PODMAN_SOCKET: rootSocket,
        },
      }),
    );
    expect(discovery.local).toBe(false);
    expect(discovery.engineSocketPath).toBeUndefined();
  });

  it("uses PODMAN_SOCKET only as engine-side override", async () => {
    const discovery = await discoverRuntime(
      "podman",
      fixture({ env: { PODMAN_SOCKET: "unix:///custom/engine.sock" } }),
    );
    expect(discovery).toMatchObject({
      hostSocketPath: hostSocket,
      engineSocketPath: "/custom/engine.sock",
    });
  });

  it("returns a blocker when Linux mount override probe throws", async () => {
    const dependencies = fixture({
      platform: "linux",
      env: { PODMAN_SOCKET: "/custom/engine.sock" },
    });
    dependencies.probeUnixSocket = vi.fn(async (path?: string) => {
      if (path === "/custom/engine.sock") throw new Error("probe failed");
      return { isSocket: true, responds: true };
    });
    expect(
      await checkKindSocket(
        await discoverRuntime("podman", dependencies),
        dependencies,
      ),
    ).toMatchObject({ status: "error", blocking: true });
  });

  it("recognizes CONTAINER_HOST forwarded socket as selected machine endpoint", async () => {
    const discovery = await discoverRuntime(
      "podman",
      fixture({ env: { CONTAINER_HOST: `unix://${hostSocket}` } }),
    );
    expect(discovery).toMatchObject({
      local: true,
      hostSocketPath: hostSocket,
      engineSocketPath: rootlessSocket,
      podmanMachineName: "chosen",
    });
  });

  it("honors named selected connection, and refuses unknown names", async () => {
    expect(
      await discoverRuntime(
        "podman",
        fixture({ root: true, env: { CONTAINER_CONNECTION: "chosen-root" } }),
      ),
    ).toMatchObject({
      engineSocketPath: rootSocket,
      podmanMachineName: "chosen",
    });
    expect(
      await discoverRuntime(
        "podman",
        fixture({ env: { CONTAINER_CONNECTION: "missing" } }),
      ),
    ).toMatchObject({ local: false });
  });

  it("refuses a forwarded API that disagrees with selected SSH engine", async () => {
    const discovery = await discoverRuntime(
      "podman",
      fixture({ root: true, infoPath: rootlessSocket }),
    );
    expect(discovery.hostSocketPath).toBeUndefined();
    expect(discovery.engineSocketPath).toBeUndefined();
  });

  it("does not infer VM mount paths for unmanaged macOS sockets", async () => {
    const discovery = await discoverRuntime(
      "podman",
      fixture({
        env: {
          CONTAINER_HOST: "unix:///custom/host.sock",
          PODMAN_SOCKET: rootSocket,
        },
      }),
    );
    expect(discovery.hostSocketPath).toBe("/custom/host.sock");
    expect(discovery.engineSocketPath).toBeUndefined();
  });

  it.each(["ssh://remote/socket", "relative.sock"])(
    "rejects invalid mount overrides %s",
    async (override) => {
      const discovery = await discoverRuntime(
        "podman",
        fixture({ env: { PODMAN_SOCKET: override } }),
      );
      expect(discovery.engineSocketPath).toBeUndefined();
    },
  );
});

describe("Docker socket address spaces", () => {
  function dockerFixture(
    os: string,
    endpoint = "unix:///Users/test/.docker/run/docker.sock",
  ) {
    const dependencies = fixture();
    dependencies.runCommand = vi.fn(
      async (_command: string, args: string[]) => ({
        exitCode: 0,
        stderr: "",
        stdout:
          args[0] === "context" && args[1] === "show"
            ? "desktop-linux"
            : JSON.stringify(
                args[0] === "context"
                  ? [{ Endpoints: { docker: { Host: endpoint } } }]
                  : { OperatingSystem: os },
              ),
      }),
    );
    return dependencies;
  }
  it("uses documented VM daemon socket only for recognized Docker Desktop", async () => {
    expect(
      await discoverRuntime("docker", dockerFixture("Docker Desktop")),
    ).toMatchObject({
      hostSocketPath: "/Users/test/.docker/run/docker.sock",
      engineSocketPath: "/var/run/docker.sock",
    });
  });
  it("blocks Kind for unrecognized macOS provider rather than mounting host forwarding socket", async () => {
    const dependencies = dockerFixture("Other Linux VM");
    const discovery = await discoverRuntime("docker", dependencies);
    expect(discovery.engineSocketPath).toBeUndefined();
    expect((await checkKindSocket(discovery, dependencies)).status).toBe(
      "error",
    );
  });
  it("refuses remote Docker Desktop even when server reports Desktop", async () => {
    expect(
      await discoverRuntime(
        "docker",
        dockerFixture("Docker Desktop", "ssh://user@remote.example"),
      ),
    ).toMatchObject({ local: false });
    expect(
      (
        await discoverRuntime(
          "docker",
          dockerFixture("Docker Desktop", "ssh://user@remote.example"),
        )
      ).engineSocketPath,
    ).toBeUndefined();
  });
});
