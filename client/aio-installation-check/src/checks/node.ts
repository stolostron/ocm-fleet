import type { CheckResult } from "../contracts.js";
import { type CheckDependencies, resolveDependencies } from "./dependencies.js";

function parseNodeMajor(version: string): number | undefined {
  const match = version.trim().match(/^v?(\d+)\./);
  return match ? Number(match[1]) : undefined;
}

export async function checkFleetctlNode(
  overrides: Partial<CheckDependencies> = {},
): Promise<CheckResult> {
  const dependencies = resolveDependencies(overrides);
  let result;
  try {
    result = await dependencies.runCommand("node", ["--version"]);
  } catch (error) {
    result = {
      exitCode: 1,
      stdout: "",
      stderr: error instanceof Error ? error.message : String(error),
    };
  }

  const version = result.exitCode === 0 ? result.stdout.trim() : "";
  const major = parseNodeMajor(version);
  if (major !== undefined && major >= 24) {
    return {
      id: "fleetctl-node-version",
      title: "Node.js for fleetctl",
      status: "pass",
      reason: `Node.js ${version} meets fleetctl's Node 24 minimum.`,
      blocking: false,
    };
  }

  const reason = !version
    ? "Node.js was not detected. Node.js is required to run fleetctl; its absence does not block AIO."
    : major !== undefined && major < 24
      ? `Node.js ${version} is unsupported by fleetctl, which specifies Node 24 as its minimum; this does not prove Node ${major} cannot execute fleetctl. AIO can continue without upgrading.`
      : `Could not determine a supported Node.js version from ${JSON.stringify(version)}. fleetctl specifies Node 24 as its minimum; AIO can continue.`;

  return {
    id: "fleetctl-node-version",
    title: "Node.js for fleetctl",
    status: "warning",
    reason,
    blocking: false,
    remediation: {
      explanation:
        "Install or upgrade Node.js to version 24 or later for supported fleetctl use, or skip this optional setup.",
      documentationUrl: "https://nodejs.org/en/download/",
      actions: [
        {
          id: "node-downloads",
          title: "Node.js downloads",
          description: "Install a supported Node.js version for fleetctl.",
          command: "https://nodejs.org/en/download/",
          requiresPrivilege: false,
          manual: true,
        },
        {
          id: "nvm",
          title: "nvm version manager",
          description: "Manage multiple Node.js versions with nvm.",
          command: "https://github.com/nvm-sh/nvm",
          requiresPrivilege: false,
          manual: true,
        },
      ],
    },
  };
}
