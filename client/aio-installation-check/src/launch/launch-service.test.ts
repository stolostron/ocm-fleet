/* eslint playwright/expect-expect: off, playwright/no-conditional-in-test: off -- These are Vitest unit tests, not Playwright e2e tests. */
import assert from "node:assert/strict";

import { test } from "vitest";

import type { LaunchPlan } from "../contracts";
import {
  buildLaunchPlan,
  createDataDirectory,
  executeLaunch,
  KIND_PRIVILEGE_EXPLANATION,
  validateDataPath,
} from "./index";
import { createLaunchService } from "./launch-service";

function directoryStats(isDirectory: boolean): { isDirectory(): boolean } {
  return { isDirectory: () => isDirectory };
}

function fsError(code: string): NodeJS.ErrnoException {
  return Object.assign(new Error(code), { code });
}

test("buildLaunchPlan covers runtime, mode, persistence and socket variants", () => {
  for (const runtime of ["podman", "docker"] as const) {
    for (const mode of ["basic", "kind"] as const) {
      for (const persistent of [false, true]) {
        const plan = createLaunchService().buildLaunchPlan({
          runtime,
          mode,
          ...(persistent ? { dataPath: "/var/lib/fleet shift/data" } : {}),
          ...(mode === "kind"
            ? { engineSocketPath: "/run/user/1000/podman.sock" }
            : {}),
        });

        assert.equal(plan.executable, runtime);
        assert.deepEqual(plan.args.slice(0, 2), ["run", "--pull=always"]);
        assert.ok(plan.args.includes("127.0.0.1:8085:8085"));
        assert.ok(plan.args.includes("127.0.0.1:50051:50051"));
        assert.ok(plan.args.includes("quay.io/stolostron/fleetshift:latest"));
        assert.equal(
          plan.args.includes("/var/lib/fleet shift/data:/data"),
          persistent,
        );

        if (mode === "basic") {
          assert.ok(!plan.args.includes("--privileged"));
          assert.ok(!plan.args.includes("/tmp:/tmp"));
          assert.ok(!plan.args.includes("--network"));
          assert.equal(
            plan.args.some((arg) => arg.includes("/data")),
            persistent,
          );
        } else {
          assert.ok(plan.args.includes("--privileged"));
          assert.ok(plan.args.includes("/tmp:/tmp"));
          assert.ok(
            plan.args.includes(
              "/run/user/1000/podman.sock:/var/run/docker.sock",
            ),
          );
          assert.ok(
            plan.args.includes("CONTAINER_HOST=unix:///var/run/docker.sock"),
          );
          assert.ok(plan.displayCommand.includes("--privileged"));

          if (runtime === "podman") {
            assert.ok(plan.args.includes("kind:alias=fleetshift"));
            assert.ok(!plan.args.includes("--network-alias"));
          } else {
            assert.ok(plan.args.includes("--network-alias"));
            assert.ok(plan.args.includes("fleetshift"));
            assert.ok(plan.args.includes("kind"));
            assert.ok(!plan.args.includes("kind:alias=fleetshift"));
          }
        }

        assert.equal(plan.environment.CONTAINER_HOST, undefined);
      }
    }
  }
});

test("launch module exposes required API and UI privilege explanation", () => {
  assert.equal(typeof buildLaunchPlan, "function");
  assert.equal(typeof validateDataPath, "function");
  assert.equal(typeof createDataDirectory, "function");
  assert.equal(typeof executeLaunch, "function");
  assert.match(
    KIND_PRIVILEGE_EXPLANATION,
    /local\/development.*broad host access/,
  );
});

test("Kind mode refuses absent engine mount socket rather than defaulting to a host socket", () => {
  assert.throws(
    () =>
      createLaunchService().buildLaunchPlan({
        runtime: "podman",
        mode: "kind",
      }),
    /engineSocketPath/,
  );
  assert.throws(
    () =>
      createLaunchService().buildLaunchPlan({
        runtime: "docker",
        mode: "kind",
      }),
    /engineSocketPath/,
  );
});

test("Kind mode uses explicit engine socket source and container destination", () => {
  const plan = createLaunchService().buildLaunchPlan({
    runtime: "docker",
    mode: "kind",
    engineSocketPath: "/var/run/docker.sock",
  });

  assert.ok(plan.args.includes("/var/run/docker.sock:/var/run/docker.sock"));
  assert.ok(plan.args.includes("CONTAINER_HOST=unix:///var/run/docker.sock"));
});

test("display command shell-quotes paths while execution retains original argv", async () => {
  const path = `/tmp/fleet shift/it's; $(touch nope)`;
  const invocations: Array<{ executable: string; args: string[] }> = [];
  const service = createLaunchService({
    runCommand: async (executable, args) => {
      invocations.push({ executable, args });
      return { code: 0 };
    },
  });
  const plan = service.buildLaunchPlan({
    runtime: "podman",
    mode: "basic",
    dataPath: path,
  });

  assert.equal(invocations.length, 0);
  assert.ok(
    plan.displayCommand.includes(
      "'/tmp/fleet shift/it'\\''s; $(touch nope):/data'",
    ),
  );
  assert.deepEqual(await service.executeLaunch(plan), { started: true });
  assert.deepEqual(invocations, [{ executable: "podman", args: plan.args }]);
  assert.ok(invocations[0]?.args.includes(`${path}:/data`));
});

test("basic mode omits persistence when skipped", () => {
  const plan = createLaunchService().buildLaunchPlan({
    runtime: "podman",
    mode: "basic",
  });

  assert.ok(!plan.args.some((arg) => arg.endsWith(":/data")));
  assert.ok(!plan.displayCommand.includes("/data"));
});

test("validateDataPath differentiates missing, non-directory and permission failures", async () => {
  const missingService = createLaunchService({
    stat: async () => {
      throw fsError("ENOENT");
    },
  });
  assert.equal(
    (await missingService.validateDataPath("/missing")).status,
    "missing",
  );

  const notDirectoryService = createLaunchService({
    stat: async () => directoryStats(false),
  });
  assert.equal(
    (await notDirectoryService.validateDataPath("/file")).status,
    "not-directory",
  );

  const deniedService = createLaunchService({
    stat: async () => directoryStats(true),
    checkPathAccess: async () => false,
  });
  assert.equal(
    (await deniedService.validateDataPath("/private")).status,
    "permission-denied",
  );

  const usableService = createLaunchService({
    stat: async () => directoryStats(true),
    checkPathAccess: async () => true,
  });
  assert.equal(
    (await usableService.validateDataPath("/writable")).status,
    "usable",
  );
});

test("validateDataPath maps non-directory ancestors and rejects empty input", async () => {
  const service = createLaunchService({
    stat: async () => {
      throw fsError("ENOTDIR");
    },
  });

  assert.equal(
    (await service.validateDataPath("/file/child")).status,
    "not-directory",
  );
  assert.equal((await service.validateDataPath("")).status, "missing");
});

test("createDataDirectory only reaches filesystem mkdir when explicitly called", async () => {
  const calls: Array<{ path: string; options: { recursive: boolean } }> = [];
  const service = createLaunchService({
    mkdir: async (path, options) => {
      calls.push({ path, options: options as { recursive: boolean } });
      return undefined;
    },
  });

  assert.deepEqual(calls, []);
  await service.createDataDirectory("/new/data");
  assert.deepEqual(calls, [
    { path: "/new/data", options: { recursive: true } },
  ]);
});

test("executeLaunch reports pull or startup failures as not started", async () => {
  const plan: LaunchPlan = {
    executable: "docker",
    args: ["run", "--pull=always", "quay.io/stolostron/fleetshift:latest"],
    environment: {},
    displayCommand: "this display text must not be executed",
  };
  let invoked: { executable: string; args: string[] } | undefined;
  const service = createLaunchService({
    runCommand: async (executable, args) => {
      invoked = { executable, args };
      return { code: 1 };
    },
  });

  const result = await service.executeLaunch(plan);
  assert.equal(result.started, false);
  assert.match(result.error ?? "", /AIO was not started/);
  assert.deepEqual(invoked, { executable: plan.executable, args: plan.args });
});

test("executeLaunch maps spawn errors without claiming startup", async () => {
  const service = createLaunchService({
    runCommand: async () => ({
      code: null,
      error: new Error("runtime missing"),
    }),
  });
  const result = await service.executeLaunch({
    executable: "podman",
    args: ["run"],
    environment: {},
    displayCommand: "podman run",
  });

  assert.deepEqual(result, { started: false, error: "runtime missing" });
});
