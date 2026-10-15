import type { CheckResult, RuntimeDiscovery } from "../contracts.js";
import { type CheckDependencies, resolveDependencies } from "./dependencies.js";

function result(
  id: string,
  title: string,
  status: "pass" | "error",
  reason: string,
  explanation: string,
  command?: string,
): CheckResult {
  const check = {
    id,
    title,
    status,
    reason,
    blocking: status === "error",
  };
  if (status !== "error") return check;

  const remediation: NonNullable<CheckResult["remediation"]> = {
    explanation,
    documentationUrl:
      "https://docs.example.invalid/fleetshift/aio-installation-check#kind-mode",
  };
  if (command) {
    remediation.actions = [
      {
        id: `${id}-manual-fix`,
        title: "Review manual setup command",
        description: explanation,
        command,
        requiresPrivilege: false,
        manual: true,
      },
    ];
  }
  return { ...check, remediation };
}

export async function checkKindSocket(
  discovery: RuntimeDiscovery,
  overrides: Partial<CheckDependencies> = {},
): Promise<CheckResult> {
  const dependencies = resolveDependencies(overrides);
  const runtimeName = discovery.runtime === "podman" ? "Podman" : "Docker";
  const rootlessSocketCommand =
    discovery.runtime === "podman" && dependencies.platform === "linux"
      ? "systemctl --user enable --now podman.socket"
      : undefined;
  if (
    !discovery.local ||
    !discovery.hostSocketPath ||
    !discovery.engineSocketPath
  ) {
    return result(
      "kind-api-socket",
      "Kind API socket",
      "error",
      `Both a host API probe socket and an engine-side mount socket must be identified for the selected local ${runtimeName} target.`,
      "Configure the selected local engine API socket. On macOS Podman, use a matching managed machine; PODMAN_SOCKET overrides only the engine-side bind source.",
      rootlessSocketCommand,
    );
  }

  let probe;
  try {
    probe = await dependencies.probeUnixSocket(discovery.hostSocketPath);
  } catch (error) {
    return result(
      "kind-api-socket",
      "Kind API socket",
      "error",
      `Could not probe ${discovery.hostSocketPath}: ${error instanceof Error ? error.message : String(error)}.`,
      "Start or configure the selected local engine API socket and rerun the Kind checks.",
    );
  }

  if (!probe.isSocket) {
    return result(
      "kind-api-socket",
      "Kind API socket",
      "error",
      `${discovery.hostSocketPath} is missing or is not a Unix socket.`,
      "Start or configure the selected local engine API socket and rerun the Kind checks.",
      rootlessSocketCommand,
    );
  }
  if (!probe.responds) {
    return result(
      "kind-api-socket",
      "Kind API socket",
      "error",
      `The Unix socket at ${discovery.hostSocketPath} did not respond to the Engine API /_ping request.`,
      "Start or configure the selected local engine API socket, then rerun the Kind checks.",
      rootlessSocketCommand,
    );
  }

  // Host ping alone cannot validate a bind source in Podman's VM. No curl or
  // container/image dependency is needed: SSH performs a read-only socket test.
  if (discovery.podmanMachineName) {
    let mountExists = false;
    try {
      const command = await dependencies.runCommand("podman", [
        "machine",
        "ssh",
        discovery.podmanMachineName,
        `test -S '${discovery.engineSocketPath.replaceAll("'", "'\\''")}'`,
      ]);
      mountExists = command.exitCode === 0;
    } catch {
      mountExists = false;
    }
    if (!mountExists)
      return result(
        "kind-api-socket",
        "Kind API socket",
        "error",
        `The engine-side socket at ${discovery.engineSocketPath} is missing or could not be validated in Podman machine ${discovery.podmanMachineName}.`,
        "Start or configure the selected machine API socket, then rerun the Kind checks.",
      );
  } else if (
    discovery.engineSocketPath !== discovery.hostSocketPath &&
    dependencies.platform === "linux"
  ) {
    let mountProbe;
    try {
      mountProbe = await dependencies.probeUnixSocket(
        discovery.engineSocketPath,
      );
    } catch {
      mountProbe = { isSocket: false, responds: false };
    }
    if (!mountProbe.isSocket || !mountProbe.responds)
      return result(
        "kind-api-socket",
        "Kind API socket",
        "error",
        `The engine-side socket at ${discovery.engineSocketPath} is not a responsive Unix socket.`,
        "Configure PODMAN_SOCKET to a live local engine API socket, then rerun the Kind checks.",
        rootlessSocketCommand,
      );
  }

  return result(
    "kind-api-socket",
    "Kind API socket",
    "pass",
    `The selected ${runtimeName} host API socket at ${discovery.hostSocketPath} responds; Kind binds the engine-side socket ${discovery.engineSocketPath}.`,
    "",
  );
}

export async function checkKindNetwork(
  discovery: RuntimeDiscovery,
  overrides: Partial<CheckDependencies> = {},
): Promise<CheckResult> {
  const dependencies = resolveDependencies(overrides);
  const args =
    discovery.runtime === "podman"
      ? ["network", "inspect", "kind"]
      : ["network", "inspect", "kind"];
  let commandResult;
  try {
    commandResult = await dependencies.runCommand(discovery.runtime, args);
  } catch (error) {
    commandResult = {
      exitCode: 1,
      stdout: "",
      stderr: error instanceof Error ? error.message : String(error),
    };
  }

  let validJson = false;
  if (commandResult.exitCode === 0) {
    try {
      const parsed: unknown = JSON.parse(commandResult.stdout.trim());
      validJson = Array.isArray(parsed)
        ? parsed.length > 0
        : parsed !== null && typeof parsed === "object";
    } catch {
      validJson = false;
    }
  }

  if (!validJson) {
    const command =
      discovery.runtime === "podman"
        ? "podman network create kind"
        : "docker network create kind";
    return result(
      "kind-network",
      "Kind network",
      "error",
      `The selected ${discovery.runtime} engine does not report a usable kind network.`,
      `Create the local kind network with the runtime-specific command, then rerun the Kind checks.`,
      command,
    );
  }

  return result(
    "kind-network",
    "Kind network",
    "pass",
    `The selected ${discovery.runtime} engine has a kind network.`,
    "",
  );
}
