import { spawn } from "node:child_process";
import { mkdir, stat } from "node:fs/promises";

import type {
  DataPathValidation,
  LaunchOptions,
  LaunchPlan,
  LaunchResult,
} from "../contracts";

const AIO_IMAGE = "quay.io/stolostron/fleetshift:latest";
const CONTAINER_SOCKET_PATH = "/var/run/docker.sock";

interface CommandResult {
  code: number | null;
  signal?: NodeJS.Signals | null;
  error?: Error;
}

interface LaunchDependencies {
  runCommand: (
    executable: string,
    args: string[],
    environment: Record<string, string>,
  ) => Promise<CommandResult>;
  stat: (path: string) => Promise<{ isDirectory(): boolean }>;
  checkPathAccess: (path: string) => Promise<boolean>;
  mkdir: (path: string, options: { recursive: boolean }) => Promise<unknown>;
}

export interface LaunchService {
  buildLaunchPlan(options: LaunchOptions): LaunchPlan;
  validateDataPath(path: string): Promise<DataPathValidation>;
  createDataDirectory(path: string): Promise<void>;
  executeLaunch(plan: LaunchPlan): Promise<LaunchResult>;
}

function quoteForShell(value: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(value)) {
    return value;
  }

  return `'${value.replaceAll("'", `'\\''`)}'`;
}

function renderCommand(executable: string, args: string[]): string {
  return [executable, ...args].map(quoteForShell).join(" ");
}

function runCommand(
  executable: string,
  args: string[],
  environment: Record<string, string>,
): Promise<CommandResult> {
  return new Promise((resolve) => {
    let child: ReturnType<typeof spawn>;

    try {
      child = spawn(executable, args, {
        stdio: "inherit",
        env: { ...process.env, ...environment },
      });
    } catch (error) {
      resolve({
        code: null,
        error: error instanceof Error ? error : new Error(String(error)),
      });
      return;
    }

    let settled = false;
    const finish = (result: CommandResult) => {
      if (!settled) {
        settled = true;
        resolve(result);
      }
    };

    child.once("error", (error) => finish({ code: null, error }));
    child.once("close", (code, signal) => finish({ code, signal }));
  });
}

function statusForFsError(path: string, error: unknown): DataPathValidation {
  const code = (error as NodeJS.ErrnoException | undefined)?.code;

  if (code === "ENOENT") {
    return { status: "missing", path, reason: "Path does not exist." };
  }

  if (code === "ENOTDIR") {
    return {
      status: "not-directory",
      path,
      reason: "Path or one of its parent components is not a directory.",
    };
  }

  if (code === "EACCES" || code === "EPERM") {
    return {
      status: "permission-denied",
      path,
      reason: "Path cannot be accessed with current permissions.",
    };
  }

  return {
    status: "permission-denied",
    path,
    reason:
      error instanceof Error ? error.message : "Path could not be accessed.",
  };
}

export function createLaunchService(
  overrides: Partial<LaunchDependencies> = {},
): LaunchService {
  const dependencies: LaunchDependencies = {
    runCommand,
    stat: async (path) => {
      const result = await stat(path);
      return { isDirectory: () => result.isDirectory() };
    },
    checkPathAccess: async (path) => {
      const result = await runCommand(
        "/bin/sh",
        ["-c", 'test -r "$1" && test -w "$1"', "aio-installation-check", path],
        {},
      );
      return result.code === 0 && result.error === undefined;
    },
    mkdir: async (path) => mkdir(path, { recursive: true }),
    ...overrides,
  };

  return {
    buildLaunchPlan(options) {
      if (options.dataPath === "") {
        throw new TypeError(
          "dataPath must be omitted or contain a directory path",
        );
      }

      const executable = options.runtime;
      const args = ["run", "--pull=always"];

      args.push("-p", "127.0.0.1:8085:8085", "-p", "127.0.0.1:50051:50051");

      if (options.dataPath !== undefined) {
        args.push("-v", `${options.dataPath}:/data`);
      }

      if (options.mode === "kind") {
        const socketPath = options.engineSocketPath;
        if (!socketPath?.startsWith("/") || /[\r\n:]/.test(socketPath)) {
          throw new TypeError(
            "Kind launch requires an absolute engineSocketPath from validated discovery",
          );
        }

        args.push(
          "--privileged",
          "-v",
          "/tmp:/tmp",
          "-v",
          `${socketPath}:${CONTAINER_SOCKET_PATH}`,
          "-e",
          `CONTAINER_HOST=unix://${CONTAINER_SOCKET_PATH}`,
        );

        if (options.runtime === "podman") {
          args.push("--network", "kind:alias=fleetshift");
        } else {
          args.push("--network", "kind", "--network-alias", "fleetshift");
        }
      }

      args.push(AIO_IMAGE);

      const plan: LaunchPlan = {
        executable,
        args,
        environment: {},
        displayCommand: renderCommand(executable, args),
      };

      return plan;
    },

    async validateDataPath(path) {
      if (path.length === 0) {
        return { status: "missing", path, reason: "Enter a directory path." };
      }

      let pathStats: Awaited<ReturnType<LaunchDependencies["stat"]>>;
      try {
        pathStats = await dependencies.stat(path);
      } catch (error) {
        return statusForFsError(path, error);
      }

      if (!pathStats.isDirectory()) {
        return {
          status: "not-directory",
          path,
          reason: "Path exists but is not a directory.",
        };
      }

      try {
        // Check effective read/write access for the current account.
        if (!(await dependencies.checkPathAccess(path))) {
          return {
            status: "permission-denied",
            path,
            reason:
              "Directory is not readable and writable with current permissions.",
          };
        }
      } catch (error) {
        return statusForFsError(path, error);
      }

      return {
        status: "usable",
        path,
        reason: "Directory exists and is readable and writable.",
      };
    },

    async createDataDirectory(path) {
      if (path.length === 0) {
        throw new TypeError("path must contain a directory path");
      }

      // Caller must obtain explicit user consent before invoking this helper.
      await dependencies.mkdir(path, { recursive: true });
    },

    async executeLaunch(plan) {
      let result: CommandResult;
      try {
        // Execution deliberately receives the plan's executable and argument vector;
        // displayCommand is presentation-only and is never parsed or executed.
        result = await dependencies.runCommand(
          plan.executable,
          plan.args,
          plan.environment,
        );
      } catch (error) {
        return {
          started: false,
          error: error instanceof Error ? error.message : String(error),
        };
      }

      if (result.error) {
        return { started: false, error: result.error.message };
      }

      if (result.code !== 0) {
        const outcome = result.signal
          ? `terminated by signal ${result.signal}`
          : `exited with status ${String(result.code)}`;
        return {
          started: false,
          error: `${plan.executable} ${outcome}; AIO was not started. The image pull or container startup may have failed.`,
        };
      }

      const success: LaunchResult = { started: true };
      return success;
    },
  };
}
