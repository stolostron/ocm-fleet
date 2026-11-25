import { EventEmitter } from "node:events";

import { afterEach, describe, expect, it, vi } from "vitest";

const { spawn } = vi.hoisted(() => ({ spawn: vi.fn() }));
vi.mock("node:child_process", () => ({ spawn }));

import { defaultDependencies } from "./dependencies.js";

function child() {
  return Object.assign(new EventEmitter(), {
    stdout: new EventEmitter(),
    stderr: new EventEmitter(),
    kill: vi.fn(),
  });
}

afterEach(() => {
  vi.useRealTimers();
  spawn.mockReset();
});

describe("check subprocesses", () => {
  it("times out and kills a hanging command even if close never arrives", async () => {
    vi.useFakeTimers();
    const process = child();
    spawn.mockReturnValue(process);
    const result = defaultDependencies.runCommand("docker", ["info"]);
    process.stdout.emit("data", "partial");
    await vi.advanceTimersByTimeAsync(15_000);
    expect(await result).toMatchObject({
      exitCode: 1,
      stdout: "partial",
      stderr: expect.stringContaining("timed out"),
    });
    expect(process.kill).toHaveBeenCalledWith("SIGKILL");
    expect(spawn).toHaveBeenCalledWith("docker", ["info"], {
      stdio: ["ignore", "pipe", "pipe"],
      timeout: 15_000,
      killSignal: "SIGKILL",
    });
  });

  it("preserves normal output and clears its deadline", async () => {
    vi.useFakeTimers();
    const process = child();
    spawn.mockReturnValue(process);
    const result = defaultDependencies.runCommand("docker", ["info"]);
    process.stdout.emit("data", "out");
    process.stderr.emit("data", "err");
    process.emit("close", 0);
    expect(await result).toEqual({ exitCode: 0, stdout: "out", stderr: "err" });
    await vi.advanceTimersByTimeAsync(15_000);
    expect(process.kill).not.toHaveBeenCalled();
  });

  it("clears the deadline when spawning fails", async () => {
    vi.useFakeTimers();
    const process = child();
    spawn.mockReturnValue(process);
    const result = defaultDependencies.runCommand("missing", []);
    process.emit("error", new Error("not found"));
    expect(await result).toMatchObject({ exitCode: 127, stderr: "not found" });
    await vi.advanceTimersByTimeAsync(15_000);
    expect(process.kill).not.toHaveBeenCalled();
  });

  it("kills a real hanging subprocess at the deadline", async () => {
    const actual =
      await vi.importActual<typeof import("node:child_process")>(
        "node:child_process",
      );
    vi.useFakeTimers();
    const process = actual.spawn("/bin/sleep", ["60"], {
      stdio: ["ignore", "pipe", "pipe"],
    });
    spawn.mockReturnValue(process);
    const exited = new Promise<string | null>((resolve) => {
      process.on("exit", (_code, signal) => resolve(signal));
    });
    const result = defaultDependencies.runCommand("/bin/sleep", ["60"]);
    await vi.advanceTimersByTimeAsync(15_000);
    expect((await result).exitCode).toBe(1);
    expect(await exited).toBe("SIGKILL");
  });
});
