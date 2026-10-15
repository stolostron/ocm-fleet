import type {
  CheckResult,
  RuntimeDiscovery,
  RuntimeKind,
} from "../contracts.js";

const runtimeDocs: Record<RuntimeKind, string> = {
  podman: "https://podman.io/docs/installation",
  docker: "https://docs.docker.com/get-started/get-docker/",
};

function versionSummary(discovery: RuntimeDiscovery): string {
  const serverLabel = discovery.runtime === "docker" ? "engine" : "server";
  const versions = [
    discovery.clientVersion ? `client ${discovery.clientVersion}` : undefined,
    discovery.serverVersion
      ? `${serverLabel} ${discovery.serverVersion}`
      : undefined,
  ].filter(Boolean);
  return versions.length
    ? ` Detected versions: ${versions.join(", ")}.`
    : " Version information unavailable.";
}

export function checkSelectedRuntime(discovery: RuntimeDiscovery): CheckResult {
  const title = `${discovery.runtime === "podman" ? "Podman" : "Docker"} runtime`;
  const remediation = {
    explanation:
      "Install the runtime or start its local engine, then rerun this check.",
    documentationUrl: runtimeDocs[discovery.runtime],
  };

  if (!discovery.detected) {
    return {
      id: "selected-runtime",
      title,
      status: "error",
      reason: `${title} client was not detected. A usable Podman or Docker runtime is required.`,
      blocking: true,
      remediation,
    };
  }

  if (!discovery.ready) {
    return {
      id: "selected-runtime",
      title,
      status: "error",
      reason: `${title} client is installed, but its engine did not return valid readiness information.${versionSummary(discovery)}`,
      blocking: true,
      remediation,
    };
  }

  if (!discovery.local) {
    return {
      id: "selected-runtime",
      title,
      status: "warning",
      reason: `${title} responds, but its active target is remote or cannot be confirmed local. Remote engines are outside first-release local AIO support; local-AIO compatibility is not established.${versionSummary(discovery)}`,
      blocking: true,
      remediation: {
        explanation:
          "Select the local engine or the managed local Podman machine, then rerun preflight.",
        documentationUrl: runtimeDocs[discovery.runtime],
      },
    };
  }

  return {
    id: "selected-runtime",
    title,
    status: "pass",
    reason: `Local ${discovery.runtime} engine is ready.${versionSummary(discovery)}`,
    blocking: false,
  };
}

export function checkDockerValidation(
  discovery: RuntimeDiscovery,
): CheckResult | undefined {
  if (
    discovery.runtime !== "docker" ||
    !discovery.detected ||
    !discovery.ready ||
    !discovery.local
  ) {
    return undefined;
  }
  return {
    id: "docker-project-validation",
    title: "Docker project validation",
    status: "warning",
    reason:
      "Docker is usable, but this AIO path is not yet fully validated by the project; no specific Docker defect is known.",
    blocking: false,
    remediation: {
      explanation:
        "Continue with Docker if preferred, or use Podman, the project-tested runtime.",
      documentationUrl:
        "https://docs.example.invalid/fleetshift/aio-installation-check#runtime-support",
    },
  };
}
