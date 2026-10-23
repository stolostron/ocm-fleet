import { once } from "node:events";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { execa } from "execa";
import { load as loadYaml } from "js-yaml";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

const binary = fileURLToPath(new URL("../../bin/fleetctl", import.meta.url));
const deployments = [{ name: "example", sourceManifest: "a".repeat(200) }];
const server = createServer((request, response) => {
  if (request.url !== "/v1alpha/deployments") {
    response.writeHead(404).end();
    return;
  }
  response.setHeader("Content-Type", "application/json");
  response.end(JSON.stringify({ deployments }));
});
let directory: string;
let address: string;

beforeAll(async () => {
  directory = await mkdtemp(path.join(tmpdir(), "fleetctl-output-"));
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const listening = server.address();
  if (!listening || typeof listening === "string")
    throw new Error("Missing server port");
  address = `http://127.0.0.1:${listening.port}`;
});

afterAll(async () => {
  if (server.listening) {
    server.close();
    await once(server, "close");
  }
  if (directory) await rm(directory, { recursive: true, force: true });
});

function run(args: string[], ci: string, columns = "20") {
  return execa(process.execPath, [binary, "--config-dir", directory, ...args], {
    env: {
      CI: ci,
      FORCE_COLOR: undefined,
      NO_COLOR: "1",
      DEBUG: undefined,
      COLUMNS: columns,
    },
    stripFinalNewline: false,
  });
}

describe.each(["true", "false"])("redirected CLI output with CI=%s", (ci) => {
  it("prints one complete JSON document and one final newline", async () => {
    const result = await run(
      ["deployment", "list", "--server", address, "--output", "json"],
      ci,
    );

    expect(result.stdout).toBe(`${JSON.stringify(deployments, null, 2)}\n`);
    expect(result.stderr).toBe("");
  });

  it("prints one complete YAML document", async () => {
    const result = await run(
      ["deployment", "list", "--server", address, "--output", "yaml"],
      ci,
    );

    expect(loadYaml(result.stdout)).toEqual(deployments);
    expect(result.stderr).toBe("");
  });

  it("serializes a message as a JSON string", async () => {
    const result = await run(["auth", "logout", "--output", "json"], ci);

    expect(result.stdout).toBe('"Logged out."\n');
    expect(result.stderr).toBe("");
  });

  it("still renders the deployment component for human output", async () => {
    const result = await run(
      ["deployment", "list", "--server", address],
      ci,
      "160",
    );

    expect(result.stdout).toContain("UNKNOWN");
    expect(result.stdout).toContain(
      "Use --output json or yaml for full details.",
    );
    expect(result.stderr).toBe("");
  });
});

it("rejects unsupported output values", async () => {
  await expect(
    run(["auth", "logout", "--output", "table"], "true"),
  ).rejects.toMatchObject({
    exitCode: 1,
    stderr: expect.stringContaining(
      "Supported output formats: component, json, yaml",
    ),
  });
});

it("keeps debug diagnostics on stderr", async () => {
  const result = await run(
    ["auth", "logout", "--output", "json", "--debug"],
    "true",
  );

  expect(result.stdout).toBe('"Logged out."\n');
  expect(result.stderr).toMatch(
    /^\[debug\] fleetctl completed in [\d.]+ ms\n$/,
  );
});
