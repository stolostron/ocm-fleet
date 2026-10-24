/* eslint-disable playwright/no-standalone-expect */
import { describe, expect, it } from "vitest";

import type {
  CheckResult,
  DataPathValidation,
  LaunchOptions,
  LaunchPlan,
  PreflightOptions,
  RuntimeDiscovery,
} from "../contracts";
import type { Terminal } from "../ui/terminal";
import {
  type InstallationServices,
  runInstallationCheck,
} from "./orchestrator";

class TestTerminal implements Terminal {
  platform = "darwin";
  lines: string[] = [];
  choices: number[] = [];
  inputs: string[] = [];
  waits: number[] = [];
  commands: string[] = [];

  write(message: string): void {
    this.lines.push(message);
  }

  async input(): Promise<string> {
    return this.inputs.shift() ?? "";
  }

  async choose(prompt: string, choices: string[]): Promise<number> {
    this.lines.push(`${prompt} ${choices.join(" | ")}`);
    return this.choices.shift() ?? 0;
  }

  async wait(milliseconds: number): Promise<void> {
    this.waits.push(milliseconds);
  }

  async runCommand(command: string): Promise<boolean> {
    this.commands.push(command);
    return true;
  }

  async copy(): Promise<boolean> {
    return true;
  }
}

function runtime(
  runtimeKind: "podman" | "docker",
  preferred = false,
): RuntimeDiscovery {
  return {
    runtime: runtimeKind,
    detected: true,
    ready: true,
    local: true,
    preferred,
    summary: "local runtime ready",
  };
}

function result(overrides: Partial<CheckResult> = {}): CheckResult {
  return {
    id: "runtime-ready",
    title: "Container engine",
    status: "pass",
    reason: "Engine reachable",
    blocking: true,
    ...overrides,
  };
}

function services(
  runtimes: RuntimeDiscovery[],
  checks: (options: PreflightOptions) => CheckResult[],
): InstallationServices {
  return {
    async discoverRuntimes() {
      return runtimes;
    },
    async runPreflightChecks(options) {
      return checks(options);
    },
    buildLaunchPlan(options: LaunchOptions): LaunchPlan {
      return {
        executable: options.runtime,
        args: ["run"],
        environment: {},
        displayCommand: `${options.runtime} run image`,
      };
    },
    async validateDataPath(path): Promise<DataPathValidation> {
      return { status: "usable", path, reason: "usable" };
    },
    async createDataDirectory() {},
    async executeLaunch() {
      return { started: true };
    },
  };
}

describe("installation orchestration", () => {
  it("hands launch the refreshed discovery snapshot used by final preflight", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1, 1, 1];
    const initial = {
      ...runtime("podman", true),
      hostSocketPath: "/var/folders/host.sock",
      engineSocketPath: "/stale/engine.sock",
    };
    const current = {
      ...initial,
      engineSocketPath: "/run/user/1234/podman/podman.sock",
    };
    let discoveryCalls = 0;
    let checked: RuntimeDiscovery | undefined;
    let launch: LaunchOptions | undefined;
    const deps = services([initial], (options) => {
      checked = options.discovery;
      return [result()];
    });
    deps.discoverRuntimes = async () => [
      discoveryCalls++ === 0 ? initial : current,
    ];
    deps.buildLaunchPlan = (options) => {
      launch = options;
      return {
        executable: "podman",
        args: [],
        environment: {},
        displayCommand: "podman run",
      };
    };
    await runInstallationCheck(["--with-kind"], deps, terminal);
    expect(checked).toBe(current);
    expect(launch).toEqual({
      runtime: "podman",
      mode: "kind",
      engineSocketPath: current.engineSocketPath,
    });
    expect(discoveryCalls).toBe(2);
  });

  it("defaults to preferred Podman when both runtimes are detected", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1];
    let selectedRuntime = "";
    const deps = services(
      [runtime("podman", true), runtime("docker")],
      (options) => {
        selectedRuntime = options.runtime;
        return [result()];
      },
    );

    await runInstallationCheck([], deps, terminal);

    expect(selectedRuntime).toBe("podman");
    expect(terminal.lines.join("\n")).toContain("Host compatible.");
  });

  it("offers basic AIO fallback when only Kind checks block readiness", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1];
    const deps = services([runtime("podman", true)], (options) =>
      options.mode === "kind"
        ? [
            result({
              id: "kind-network",
              title: "Kind network",
              status: "error",
              reason: "Missing kind network",
            }),
          ]
        : [result()],
    );

    await runInstallationCheck(["--with-kind"], deps, terminal);

    expect(terminal.lines.join("\n")).toContain(
      "Disable Kind and continue with basic AIO",
    );
    expect(terminal.lines.join("\n")).toContain("Host compatible.");
  });

  it("skips path prompt and /data volume when persistence is skipped", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1, 1];
    const deps = services([runtime("podman", true)], () => [result()]);

    await runInstallationCheck([], deps, terminal);

    expect(terminal.lines.join("\n")).toContain("podman run image");
    expect(terminal.lines.join("\n")).not.toContain(
      "Host directory path for AIO /data",
    );
  });

  it("asks before manual fixes and polls affected check at fixed two-second intervals", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1];
    terminal.inputs = [""];
    let checkCalls = 0;
    const failed = result({
      status: "error",
      reason: "Socket inactive",
      remediation: {
        explanation: "Start local socket.",
        actions: [
          {
            id: "enable-socket",
            title: "Enable socket",
            description: "Enable user socket.",
            command: "systemctl --user enable --now podman.socket",
            requiresPrivilege: true,
            manual: true,
          },
        ],
      },
    });
    const deps = services([runtime("podman", true)], () => {
      checkCalls += 1;
      return [
        checkCalls >= 2 ? result({ remediation: failed.remediation }) : failed,
      ];
    });

    await runInstallationCheck(["--with-kind"], deps, terminal);

    expect(terminal.commands).toEqual([]);
    expect(terminal.waits).toEqual([2_000]);
    expect(checkCalls).toBe(2);
    expect(terminal.lines.join("\n")).toContain("Host compatible.");
  });

  it("offers Retry and Stop after exactly 100 manual polling attempts", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [0, 1];
    terminal.inputs = [""];
    let checkCalls = 0;
    const failed = result({
      id: "kind-api-socket",
      title: "Kind API socket",
      status: "error",
      reason: "Socket inactive",
      remediation: {
        explanation: "Start local socket.",
        actions: [
          {
            id: "enable-socket",
            title: "Enable socket",
            description: "Enable user socket.",
            command: "systemctl --user enable --now podman.socket",
            requiresPrivilege: true,
            manual: true,
          },
        ],
      },
    });
    const deps = services([runtime("podman", true)], () => {
      checkCalls += 1;
      return [failed];
    });

    await runInstallationCheck(["--with-kind"], deps, terminal);

    expect(terminal.waits).toHaveLength(100);
    expect(terminal.waits.every((interval) => interval === 2_000)).toBe(true);
    expect(checkCalls).toBe(102);
    expect(terminal.lines.join("\n")).toContain("AIO is not ready");
  });

  it("reports unsupported operating systems without reporting compatibility", async () => {
    const terminal = new TestTerminal();
    terminal.platform = "win32";

    await runInstallationCheck(
      [],
      services([], () => []),
      terminal,
    );

    expect(terminal.lines.join("\n")).toContain(
      "Unsupported host platform: win32",
    );
    expect(terminal.lines.join("\n")).not.toContain("Host compatible");
  });

  it("keeps missing Node.js guidance optional and does not run its URL as a command", async () => {
    const terminal = new TestTerminal();
    terminal.choices = [2, 1];
    const nodeWarning = result({
      id: "fleetctl-node-version",
      title: "Node.js for fleetctl",
      status: "warning",
      blocking: false,
      reason: "Node.js was not detected.",
      remediation: {
        explanation: "Install Node.js 24+ for supported fleetctl use.",
        documentationUrl: "https://nodejs.org/en/download/",
        actions: [
          {
            id: "node-downloads",
            title: "Node.js downloads",
            description: "Install Node.js for fleetctl.",
            command: "https://nodejs.org/en/download/",
            requiresPrivilege: false,
            manual: true,
          },
        ],
      },
    });
    const deps = services([runtime("podman", true)], () => [
      result(),
      nodeWarning,
    ]);

    await runInstallationCheck([], deps, terminal);

    expect(terminal.commands).toEqual([]);
    expect(terminal.lines.join("\n")).toContain(
      "Host compatible with warnings.",
    );
    expect(terminal.lines.join("\n")).toContain(
      "https://github.com/nvm-sh/nvm",
    );
  });
});
