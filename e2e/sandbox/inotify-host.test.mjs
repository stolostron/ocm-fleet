import { describe, expect, it } from "vitest";

import { ensureCiInotifyLimits } from "./inotify-host.mjs";

const PROC_INSTANCES = "/proc/sys/fs/inotify/max_user_instances";

// Model the kernel's effective sysctl value, including rejected or ineffective
// writes, without changing the developer's host configuration.
function inMemoryInotifyHost(initial, { rejectWrite, ignoreWrite } = {}) {
  let current = String(initial);
  const writes = [];
  const logs = [];
  return {
    logs,
    writes,
    readFile(path) {
      if (path !== PROC_INSTANCES) throw new Error(`unexpected path: ${path}`);
      return `${current}\n`;
    },
    run(command, args) {
      if (command !== "sudo" || args[0] !== "sysctl" || args[1] !== "-w") {
        throw new Error(`unexpected command: ${command} ${args.join(" ")}`);
      }
      const [key, value] = args[2].split("=");
      if (key !== "fs.inotify.max_user_instances") {
        throw new Error(`unexpected sysctl: ${key}`);
      }
      writes.push(Number(value));
      if (rejectWrite) return { status: 1, stderr: "permission denied" };
      if (!ignoreWrite) current = value;
      return { status: 0, stderr: "" };
    },
    log(message) {
      logs.push(message);
    },
  };
}

describe("ensureCiInotifyLimits", () => {
  it("raises the Ubuntu 26.04 default of 128 to 1280 and logs the effective value", () => {
    const host = inMemoryInotifyHost(128);
    ensureCiInotifyLimits(host);
    expect(host.readFile(PROC_INSTANCES)).toBe("1280\n");
    expect(host.writes).toEqual([1280]);
    expect(host.logs).toEqual(["inotify: fs.inotify.max_user_instances=1280"]);
  });

  it.each([1280, 4096])("preserves an existing limit of %i", (limit) => {
    const host = inMemoryInotifyHost(limit);
    ensureCiInotifyLimits(host);
    expect(host.writes).toEqual([]);
    expect(host.logs).toEqual([
      `inotify: fs.inotify.max_user_instances=${limit}`,
    ]);
  });

  it("fails setup with command output when the kernel rejects the write", () => {
    const host = inMemoryInotifyHost(128, { rejectWrite: true });
    expect(() => ensureCiInotifyLimits(host)).toThrow(
      /fs\.inotify\.max_user_instances[\s\S]*permission denied/,
    );
  });

  it("fails setup when a successful command leaves the effective limit too low", () => {
    const host = inMemoryInotifyHost(128, { ignoreWrite: true });
    expect(() => ensureCiInotifyLimits(host)).toThrow(
      /fs\.inotify\.max_user_instances is 128, required >= 1280/,
    );
    expect(host.logs).toEqual([]);
  });

  it("rejects an invalid effective value before writing", () => {
    const host = inMemoryInotifyHost("unknown");
    expect(() => ensureCiInotifyLimits(host)).toThrow(/invalid sysctl value/);
    expect(host.writes).toEqual([]);
  });
});
