/* eslint playwright/no-standalone-expect: off -- Vitest suites are not Playwright tests. */
import { describe, expect, it, vi } from "vitest";

import type { CheckDependencies, CommandResult } from "./dependencies.js";
import { discoverRuntimes, runPreflightChecks } from "./index.js";

function commandResult(
  stdout: string,
  exitCode = 0,
  stderr = "",
): CommandResult {
  return { stdout, exitCode, stderr };
}

function fakeCommand(command: string, args: string[]): CommandResult {
  const argsText = args.join(" ");
  if (command === "podman" && argsText === "--version")
    return commandResult("podman version 5.8.1\n");
  if (command === "podman" && argsText === "version --format json") {
    return commandResult(
      JSON.stringify({
        Client: { Version: "5.8.1" },
        Server: { Version: "5.8.5" },
      }),
    );
  }
  if (command === "podman" && argsText === "info --format json") {
    return commandResult(
      JSON.stringify({
        host: { remoteSocket: { path: "/run/user/1000/podman/podman.sock" } },
      }),
    );
  }
  if (
    command === "podman" &&
    argsText === "system connection list --format json"
  ) {
    return commandResult(
      JSON.stringify([
        {
          Name: "rootless",
          URI: "unix:///run/user/1000/podman/podman.sock",
          IsDefault: true,
          IsMachine: false,
        },
      ]),
    );
  }
  if (command === "docker" && argsText === "--version")
    return commandResult("Docker version 28.0.4, build test\n");
  if (command === "docker" && argsText === "version --format {{json .}}") {
    return commandResult(
      JSON.stringify({
        Client: { Version: "28.0.4" },
        Server: { Version: "28.0.4" },
      }),
    );
  }
  if (command === "docker" && argsText === "info --format {{json .}}") {
    return commandResult(JSON.stringify({ ServerVersion: "28.0.4" }));
  }
  if (command === "docker" && argsText === "context show")
    return commandResult("desktop-linux\n");
  if (command === "docker" && argsText === "context inspect desktop-linux") {
    return commandResult(
      JSON.stringify([
        {
          Endpoints: {
            docker: { Host: "unix:///Users/test/.docker/run/docker.sock" },
          },
        },
      ]),
    );
  }
  if (argsText === "network inspect kind")
    return commandResult(JSON.stringify([{ Name: "kind" }]));
  if (command === "node" && argsText === "--version")
    return commandResult("v22.15.0\n");
  return commandResult("", 127, `Unexpected command: ${command} ${argsText}`);
}

function fakeDependencies(overrides: Partial<CheckDependencies> = {}) {
  const runCommand = vi.fn(async (command: string, args: string[]) =>
    fakeCommand(command, args),
  );

  return {
    platform: "linux",
    env: {
      CONTAINER_HOST: "",
      DOCKER_CONTEXT: "",
      DOCKER_HOST: "",
      XDG_RUNTIME_DIR: "",
    },
    runCommand,
    isPortAvailable: vi.fn(async (_host: string, _port: number) => true),
    probeUnixSocket: vi.fn(async () => ({ isSocket: true, responds: true })),
    ...overrides,
  } satisfies CheckDependencies;
}

describe("runtime discovery", () => {
  it("reports Podman preference, client/server versions, and local versus remote targets", async () => {
    const dependencies = fakeDependencies();

    const result = await discoverRuntimes(dependencies);

    expect(result).toEqual([
      expect.objectContaining({
        runtime: "podman",
        detected: true,
        ready: true,
        local: true,
        preferred: true,
        clientVersion: "5.8.1",
        serverVersion: "5.8.5",
        hostSocketPath: "/run/user/1000/podman/podman.sock",
        engineSocketPath: "/run/user/1000/podman/podman.sock",
      }),
      expect.objectContaining({
        runtime: "docker",
        detected: true,
        ready: true,
        local: true,
        preferred: false,
        clientVersion: "28.0.4",
        serverVersion: "28.0.4",
        hostSocketPath: "/Users/test/.docker/run/docker.sock",
      }),
    ]);
    expect(result[1].summary).toContain("client 28.0.4, engine 28.0.4");
  });

  it("classifies a configured non-loopback Docker context as remote", async () => {
    const dependencies = fakeDependencies({
      runCommand: vi.fn(async (command: string, args: string[]) => {
        if (
          command === "docker" &&
          args.join(" ") === "context inspect desktop-linux"
        ) {
          return commandResult(
            JSON.stringify([
              { Endpoints: { docker: { Host: "tcp://10.24.0.8:2376" } } },
            ]),
          );
        }
        return fakeCommand(command, args);
      }),
    });

    const docker = (await discoverRuntimes(dependencies)).find(
      (item) => item.runtime === "docker",
    );

    expect(docker).toMatchObject({
      ready: true,
      local: false,
    });
    expect(Object.prototype.hasOwnProperty.call(docker, "hostSocketPath")).toBe(
      false,
    );
  });

  it("distinguishes a user-configured Podman SSH connection from a managed local machine", async () => {
    const dependencies = fakeDependencies({
      runCommand: vi.fn(async (command: string, args: string[]) => {
        if (
          command === "podman" &&
          args.join(" ") === "system connection list --format json"
        ) {
          return commandResult(
            JSON.stringify([
              {
                Name: "remote-builder",
                URI: "ssh://builder@10.24.0.9/run/podman/podman.sock",
                IsDefault: true,
                IsMachine: false,
              },
            ]),
          );
        }
        return fakeCommand(command, args);
      }),
    });

    const podman = (await discoverRuntimes(dependencies)).find(
      (item) => item.runtime === "podman",
    );

    expect(podman).toMatchObject({
      ready: true,
      local: false,
    });
    expect(Object.prototype.hasOwnProperty.call(podman, "hostSocketPath")).toBe(
      false,
    );
  });

  it("keeps installed-client detection separate from engine readiness", async () => {
    const dependencies = fakeDependencies({
      runCommand: vi.fn(async (command: string, args: string[]) => {
        if (command === "podman" && args.join(" ") === "info --format json") {
          return commandResult("engine stopped", 1);
        }
        return fakeCommand(command, args);
      }),
    });

    const podman = (await discoverRuntimes(dependencies)).find(
      (item) => item.runtime === "podman",
    );

    expect(podman).toMatchObject({
      detected: true,
      ready: false,
      clientVersion: "5.8.1",
    });
  });
});

describe("preflight checks", () => {
  it("keeps basic mode free of Kind-only socket and network failures", async () => {
    const dependencies = fakeDependencies({
      isPortAvailable: vi.fn(
        async (_host: string, port: number) => port !== 8085,
      ),
    });

    const results = await runPreflightChecks(
      { runtime: "podman", mode: "basic" },
      dependencies,
    );

    expect(results.map(({ id }) => id)).not.toContain("kind-api-socket");
    expect(results.map(({ id }) => id)).not.toContain("kind-network");
    expect(results.find(({ id }) => id === "aio-port-8085")).toMatchObject({
      status: "error",
      blocking: true,
    });
    expect(results.find(({ id }) => id === "aio-port-50051")).toMatchObject({
      status: "pass",
      blocking: false,
    });
    expect(
      results.find(({ id }) => id === "fleetctl-node-version"),
    ).toMatchObject({ status: "warning", blocking: false });
    expect(dependencies.probeUnixSocket).not.toHaveBeenCalled();
    expect(dependencies.runCommand).not.toHaveBeenCalledWith("podman", [
      "network",
      "inspect",
      "kind",
    ]);
  });

  it("runs Kind socket/network probes only in Kind mode", async () => {
    const dependencies = fakeDependencies();

    const results = await runPreflightChecks(
      { runtime: "podman", mode: "kind" },
      dependencies,
    );

    expect(results.find(({ id }) => id === "kind-api-socket")).toMatchObject({
      status: "pass",
      blocking: false,
    });
    expect(results.find(({ id }) => id === "kind-network")).toMatchObject({
      status: "pass",
      blocking: false,
    });
    expect(dependencies.probeUnixSocket).toHaveBeenCalledWith(
      "/run/user/1000/podman/podman.sock",
    );
    expect(dependencies.runCommand).toHaveBeenCalledWith("podman", [
      "network",
      "inspect",
      "kind",
    ]);
  });

  it("treats a missing Kind socket and network as blockers only in Kind mode", async () => {
    const dependencies = fakeDependencies({
      probeUnixSocket: vi.fn(async () => ({
        isSocket: false,
        responds: false,
      })),
      runCommand: vi.fn(async (command: string, args: string[]) => {
        if (args.join(" ") === "network inspect kind") {
          return commandResult("network not found", 1);
        }
        return fakeCommand(command, args);
      }),
    });

    const results = await runPreflightChecks(
      { runtime: "podman", mode: "kind" },
      dependencies,
    );

    expect(results.find(({ id }) => id === "kind-api-socket")).toMatchObject({
      status: "error",
      blocking: true,
    });
    expect(results.find(({ id }) => id === "kind-network")).toMatchObject({
      status: "error",
      blocking: true,
    });
    expect(
      results.find(({ id }) => id === "kind-api-socket")?.remediation
        ?.actions?.[0]?.command,
    ).toBe("systemctl --user enable --now podman.socket");
  });

  it("omits unavailable optional remediation actions", async () => {
    const dependencies = fakeDependencies({
      env: {
        CONTAINER_HOST: "",
        DOCKER_CONTEXT: "",
        DOCKER_HOST: "tcp://127.0.0.1:2375",
        XDG_RUNTIME_DIR: "",
      },
    });

    const results = await runPreflightChecks(
      { runtime: "docker", mode: "kind" },
      dependencies,
    );
    const socketResult = results.find(({ id }) => id === "kind-api-socket");

    expect(socketResult?.remediation).toBeDefined();
    expect(
      socketResult?.remediation && "actions" in socketResult.remediation,
    ).toBe(false);
  });

  it("keeps remote target as warning and blocker, without claiming local compatibility", async () => {
    const dependencies = fakeDependencies({
      runCommand: vi.fn(async (command: string, args: string[]) => {
        if (
          command === "docker" &&
          args.join(" ") === "context inspect desktop-linux"
        ) {
          return commandResult(
            JSON.stringify([
              { Endpoints: { docker: { Host: "tcp://10.24.0.8:2376" } } },
            ]),
          );
        }
        return fakeCommand(command, args);
      }),
    });

    const results = await runPreflightChecks(
      { runtime: "docker", mode: "basic" },
      dependencies,
    );

    expect(results.find(({ id }) => id === "selected-runtime")).toMatchObject({
      status: "warning",
      blocking: true,
    });
    expect(
      results.find(({ id }) => id === "selected-runtime")?.reason,
    ).toContain("local-AIO compatibility is not established");
  });

  it("reports Docker's limited project validation as a non-blocking warning", async () => {
    const dependencies = fakeDependencies();

    const results = await runPreflightChecks(
      { runtime: "docker", mode: "basic" },
      dependencies,
    );

    expect(
      results.find(({ id }) => id === "docker-project-validation"),
    ).toMatchObject({
      status: "warning",
      blocking: false,
    });
  });

  it("reports unsupported host and stops before probing runtime or host resources", async () => {
    const dependencies = fakeDependencies({ platform: "win32" });

    const results = await runPreflightChecks(
      { runtime: "podman", mode: "kind" },
      dependencies,
    );

    expect(results).toHaveLength(1);
    expect(results[0]).toMatchObject({
      id: "host-platform",
      status: "error",
      blocking: true,
    });
    expect(dependencies.runCommand).not.toHaveBeenCalled();
    expect(dependencies.isPortAvailable).not.toHaveBeenCalled();
  });
});
