import { spawn } from "node:child_process";
import { createServer } from "node:net";

import { probeUnixSocket } from "./unix-socket-probe.js";

export interface CommandResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

export interface UnixSocketProbeResult {
  isSocket: boolean;
  responds: boolean;
}

export interface CheckDependencies {
  platform: string;
  env: {
    CONTAINER_HOST: string;
    DOCKER_CONTEXT: string;
    DOCKER_HOST: string;
    XDG_RUNTIME_DIR: string;
    CONTAINER_CONNECTION?: string;
    PODMAN_SOCKET?: string;
  };
  runCommand(command: string, args: string[]): Promise<CommandResult>;
  isPortAvailable(host: string, port: number): Promise<boolean>;
  probeUnixSocket(path: string): Promise<UnixSocketProbeResult>;
}

const runCommand: CheckDependencies["runCommand"] = (command, args) =>
  new Promise((resolve) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"] });
    const stdout: string[] = [];
    const stderr: string[] = [];

    child.stdout.on("data", (chunk: Buffer) => stdout.push(String(chunk)));
    child.stderr.on("data", (chunk: Buffer) => stderr.push(String(chunk)));
    child.on("error", (error: Error) => {
      resolve({
        exitCode: 127,
        stdout: stdout.join(""),
        stderr: error.message,
      });
    });
    child.on("close", (code: number | null) => {
      resolve({
        exitCode: code ?? 1,
        stdout: stdout.join(""),
        stderr: stderr.join(""),
      });
    });
  });

const isPortAvailable: CheckDependencies["isPortAvailable"] = (host, port) =>
  new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", (error: NodeJS.ErrnoException) => {
      if (error.code === "EADDRINUSE") resolve(false);
      else reject(new Error(error.message));
    });
    server.listen({ host, port }, () => {
      server.close(() => resolve(true));
    });
  });

export const defaultDependencies: CheckDependencies = {
  platform: process.platform,
  env: {
    CONTAINER_HOST: process.env.CONTAINER_HOST || "",
    DOCKER_CONTEXT: process.env.DOCKER_CONTEXT || "",
    DOCKER_HOST: process.env.DOCKER_HOST || "",
    XDG_RUNTIME_DIR: process.env.XDG_RUNTIME_DIR || "",
    CONTAINER_CONNECTION: process.env.CONTAINER_CONNECTION || "",
    PODMAN_SOCKET: process.env.PODMAN_SOCKET || "",
  },
  runCommand,
  isPortAvailable,
  probeUnixSocket,
};

export function resolveDependencies(
  overrides: Partial<CheckDependencies> = {},
): CheckDependencies {
  return { ...defaultDependencies, ...overrides };
}
