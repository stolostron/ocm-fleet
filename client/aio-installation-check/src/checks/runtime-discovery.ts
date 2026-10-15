import type { RuntimeDiscovery, RuntimeKind } from "../contracts.js";
import {
  type CheckDependencies,
  type CommandResult,
  resolveDependencies,
} from "./dependencies.js";

type JsonRecord = Record<string, unknown>;

function parseJson(value: string): unknown | undefined {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return undefined;
  }
}

function asRecord(value: unknown): JsonRecord | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as JsonRecord)
    : undefined;
}

function records(value: unknown): JsonRecord[] {
  if (Array.isArray(value))
    return value.map(asRecord).filter((item): item is JsonRecord => !!item);
  const record = asRecord(value);
  if (!record) return [];
  for (const key of ["connections", "machines", "Networks"]) {
    if (Array.isArray(record[key])) return records(record[key]);
  }
  return [record];
}

function stringAt(value: unknown, ...keys: string[]): string | undefined {
  let current: unknown = value;
  for (const key of keys) {
    const record = asRecord(current);
    if (!record) return undefined;
    current = record[key];
  }
  return typeof current === "string" && current.length > 0
    ? current
    : undefined;
}

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function cliVersion(stdout: string, runtime: RuntimeKind): string | undefined {
  const escapedName = runtime === "podman" ? "podman" : "docker";
  const match = stdout
    .trim()
    .match(new RegExp(`^${escapedName}\\s+version\\s+v?([\\w.+-]+)`, "i"));
  return match?.[1];
}

function versionDetails(
  runtime: RuntimeKind,
  versions: { client?: string; server?: string },
): string {
  const serverLabel = runtime === "docker" ? "engine" : "server";
  const details = [
    versions.client ? `client ${versions.client}` : undefined,
    versions.server ? `${serverLabel} ${versions.server}` : undefined,
  ].filter(Boolean);
  return details.length ? ` (${details.join(", ")})` : "";
}

function structuredSuccess(result: CommandResult): unknown | undefined {
  if (result.exitCode !== 0) return undefined;
  return parseJson(result.stdout.trim());
}

function socketPathFromUri(uri?: string): string | undefined {
  if (!uri?.startsWith("unix://")) return undefined;
  return absoluteSocketPath(uri.slice("unix://".length));
}

function absoluteSocketPath(path?: string): string | undefined {
  const normalized = path?.replace(/^unix:\/\//, "");
  // Volume syntax has ':' separators; URI schemes and relative paths are not bind sources.
  return normalized?.startsWith("/") && !/[\r\n:]/.test(normalized)
    ? normalized
    : undefined;
}

function isLoopbackEndpoint(endpoint: string): boolean {
  return /^(localhost|127(?:\.\d{1,3}){3}|\[::1\])(?::\d+)?$/i.test(endpoint);
}

function localSocketFromEndpoint(endpoint?: string): {
  local: boolean;
  socketPath?: string;
} {
  if (!endpoint) return { local: false };
  const socketPath = socketPathFromUri(endpoint);
  if (socketPath) return { local: true, socketPath };
  if (endpoint.startsWith("tcp://")) {
    const host = endpoint.slice("tcp://".length).split("/")[0];
    return { local: isLoopbackEndpoint(host) };
  }
  return { local: false };
}

function podmanVersionInfo(value: unknown): {
  client?: string;
  server?: string;
} {
  const client =
    stringAt(value, "Client", "Version") ??
    stringAt(value, "Client", "version") ??
    stringAt(value, "client", "version");
  const server =
    stringAt(value, "Server", "Version") ??
    stringAt(value, "Server", "version") ??
    stringAt(value, "server", "version");
  return { client, server };
}

function dockerVersionInfo(value: unknown): {
  client?: string;
  server?: string;
} {
  return {
    client:
      stringAt(value, "Client", "Version") ??
      stringAt(value, "client", "version"),
    server:
      stringAt(value, "Server", "Version") ??
      stringAt(value, "server", "version") ??
      stringAt(value, "Engine", "Version"),
  };
}

async function safeRun(
  dependencies: CheckDependencies,
  command: string,
  args: string[],
): Promise<CommandResult> {
  try {
    return await dependencies.runCommand(command, args);
  } catch (error) {
    return {
      exitCode: 1,
      stdout: "",
      stderr: error instanceof Error ? error.message : String(error),
    };
  }
}

async function discoverPodman(
  dependencies: CheckDependencies,
): Promise<RuntimeDiscovery> {
  const [cliVersionResult, versionResult] = await Promise.all([
    safeRun(dependencies, "podman", ["--version"]),
    safeRun(dependencies, "podman", ["version", "--format", "json"]),
  ]);
  const versionJson = structuredSuccess(versionResult);
  const detected = cliVersionResult.exitCode === 0;
  const versions = podmanVersionInfo(versionJson);
  versions.client ??= cliVersion(cliVersionResult.stdout, "podman");
  const [infoResult, connectionsResult, machinesResult] = await Promise.all([
    safeRun(dependencies, "podman", ["info", "--format", "json"]),
    safeRun(dependencies, "podman", [
      "system",
      "connection",
      "list",
      "--format",
      "json",
    ]),
    dependencies.platform === "darwin"
      ? safeRun(dependencies, "podman", ["machine", "list", "--format", "json"])
      : Promise.resolve({ exitCode: 1, stdout: "", stderr: "Not applicable" }),
  ]);

  const info = structuredSuccess(infoResult);
  const ready = detected && !!asRecord(info);
  const connectionList = records(structuredSuccess(connectionsResult));
  const selectedConnection = dependencies.env.CONTAINER_HOST
    ? connectionList.find(
        (connection) => connection.URI === dependencies.env.CONTAINER_HOST,
      )
    : dependencies.env.CONTAINER_CONNECTION
      ? connectionList.find(
          (connection) =>
            connection.Name === dependencies.env.CONTAINER_CONNECTION,
        )
      : connectionList.find(
          (connection) =>
            connection.IsDefault === true || connection.Default === true,
        );
  const connectionUri =
    asString(dependencies.env.CONTAINER_HOST) ??
    asString(selectedConnection?.URI) ??
    asString(selectedConnection?.Uri) ??
    asString(selectedConnection?.uri);
  const isMachine =
    selectedConnection?.IsMachine === true ||
    selectedConnection?.isMachine === true;
  const machineList = records(structuredSuccess(machinesResult));
  const infoSocket = absoluteSocketPath(
    stringAt(info, "host", "remoteSocket", "path") ??
      stringAt(info, "Host", "RemoteSocket", "Path"),
  );
  const uriLocation = localSocketFromEndpoint(connectionUri);
  // Connection names (including '-root') are not machine identities. Match the
  // managed connection's loopback SSH port and identity to inspected machine data.
  const ssh = connectionUri?.match(
    /^ssh:\/\/([^@/]+)@(localhost|127(?:\.\d{1,3}){3}|\[::1\]):(\d+)(\/[^?#]*)$/i,
  );
  const matchingMachines: JsonRecord[] = [];
  if (
    dependencies.platform === "darwin" &&
    ((isMachine && ssh) || uriLocation.socketPath)
  ) {
    for (const machine of machineList) {
      const name = asString(machine.Name) ?? asString(machine.name);
      if (!name) continue;
      const result = await safeRun(dependencies, "podman", [
        "machine",
        "inspect",
        name,
      ]);
      const inspected = records(structuredSuccess(result))[0];
      if (!inspected) continue;
      const forwarded = absoluteSocketPath(
        stringAt(inspected, "ConnectionInfo", "PodmanSocket", "Path"),
      );
      const sshConfig = asRecord(inspected.SSHConfig);
      const identity = asString(selectedConnection?.Identity);
      const matchesSsh =
        !!ssh &&
        isMachine &&
        sshConfig?.Port === Number(ssh[3]) &&
        !!identity &&
        sshConfig?.IdentityPath === identity;
      if (
        matchesSsh ||
        (uriLocation.socketPath && forwarded === uriLocation.socketPath)
      ) {
        matchingMachines.push(inspected);
      }
    }
  }
  const inspected =
    matchingMachines.length === 1 ? matchingMachines[0] : undefined;
  const machineName = asString(inspected?.Name);
  let local =
    dependencies.platform === "darwin"
      ? !!machineName || uriLocation.local
      : uriLocation.local;
  const rootlessSocket =
    dependencies.platform === "linux" && dependencies.env.XDG_RUNTIME_DIR
      ? `${dependencies.env.XDG_RUNTIME_DIR}/podman/podman.sock`
      : undefined;
  let hostSocketPath = uriLocation.socketPath;
  let engineSocketPath: string | undefined;
  if (machineName) {
    // The forwarding socket serves the machine's configured rootful/rootless
    // API. A different selected SSH API must not be probed through that socket.
    const sameApi =
      !ssh ||
      (inspected?.Rootful === (ssh[1] === "root") &&
        (!infoSocket || infoSocket === ssh[4]));
    if (sameApi) {
      hostSocketPath = absoluteSocketPath(
        stringAt(inspected, "ConnectionInfo", "PodmanSocket", "Path"),
      );
      engineSocketPath = infoSocket;
    }
  } else if (
    dependencies.platform === "linux" &&
    !connectionUri &&
    infoSocket &&
    !dependencies.env.CONTAINER_CONNECTION &&
    asRecord(asRecord(info)?.host)?.serviceIsRemote !== true &&
    asRecord(asRecord(info)?.Host)?.ServiceIsRemote !== true
  ) {
    local = true;
    hostSocketPath = infoSocket;
  }
  if (
    dependencies.platform === "linux" &&
    local &&
    (!connectionUri || uriLocation.socketPath)
  ) {
    hostSocketPath ??= infoSocket ?? rootlessSocket;
    engineSocketPath = hostSocketPath;
  }
  if (
    local &&
    (machineName || dependencies.platform === "linux") &&
    dependencies.env.PODMAN_SOCKET
  ) {
    engineSocketPath = absoluteSocketPath(dependencies.env.PODMAN_SOCKET);
  }

  const summary = !detected
    ? "Podman client not detected"
    : !ready
      ? `Podman client detected${versionDetails("podman", versions)}, but engine is not ready`
      : local
        ? `Local Podman engine ready${versionDetails("podman", versions)}`
        : `Podman engine reachable${versionDetails("podman", versions)}, target is not confirmed local`;

  const discovery: RuntimeDiscovery = {
    runtime: "podman",
    detected,
    ready,
    local,
    preferred: true,
    summary,
  };
  if (versions.client) discovery.clientVersion = versions.client;
  if (versions.server) discovery.serverVersion = versions.server;
  if (hostSocketPath) discovery.hostSocketPath = hostSocketPath;
  if (engineSocketPath) discovery.engineSocketPath = engineSocketPath;
  if (machineName) discovery.podmanMachineName = machineName;
  return discovery;
}

async function discoverDocker(
  dependencies: CheckDependencies,
): Promise<RuntimeDiscovery> {
  const [cliVersionResult, versionResult] = await Promise.all([
    safeRun(dependencies, "docker", ["--version"]),
    safeRun(dependencies, "docker", ["version", "--format", "{{json .}}"]),
  ]);
  const versionJson = structuredSuccess(versionResult);
  const detected = cliVersionResult.exitCode === 0;
  const versions = dockerVersionInfo(versionJson);
  versions.client ??= cliVersion(cliVersionResult.stdout, "docker");
  const [infoResult, contextResult] = await Promise.all([
    safeRun(dependencies, "docker", ["info", "--format", "{{json .}}"]),
    safeRun(dependencies, "docker", ["context", "show"]),
  ]);
  const info = structuredSuccess(infoResult);
  const ready = detected && !!asRecord(info);
  const contextName =
    dependencies.env.DOCKER_CONTEXT.trim() || contextResult.stdout.trim();
  const hostOverride = dependencies.env.DOCKER_CONTEXT
    ? ""
    : dependencies.env.DOCKER_HOST;
  const contextResultForTarget = hostOverride
    ? undefined
    : contextName
      ? await safeRun(dependencies, "docker", [
          "context",
          "inspect",
          contextName,
        ])
      : undefined;
  const contextJson = contextResultForTarget
    ? structuredSuccess(contextResultForTarget)
    : undefined;
  const context = Array.isArray(contextJson) ? contextJson[0] : contextJson;
  const contextEndpoint =
    stringAt(context, "Endpoints", "docker", "Host") ??
    stringAt(context, "endpoints", "docker", "host");
  const endpoint = hostOverride || contextEndpoint;
  const location = localSocketFromEndpoint(endpoint);

  const summary = !detected
    ? "Docker client not detected"
    : !ready
      ? `Docker client detected${versionDetails("docker", versions)}, but engine is not ready`
      : location.local
        ? `Local Docker engine ready${versionDetails("docker", versions)}`
        : `Docker engine reachable${versionDetails("docker", versions)}, target is remote or unconfirmed`;

  const discovery: RuntimeDiscovery = {
    runtime: "docker",
    detected,
    ready,
    local: location.local,
    preferred: false,
    summary,
  };
  if (versions.client) discovery.clientVersion = versions.client;
  if (versions.server) discovery.serverVersion = versions.server;
  if (location.socketPath) {
    discovery.hostSocketPath = location.socketPath;
    if (dependencies.platform === "linux")
      discovery.engineSocketPath = location.socketPath;
    // Docker Desktop's host Unix socket is forwarded into its Linux VM.
    // Only recognized Desktop engines use the documented daemon-side socket;
    // unknown macOS providers remain Kind-blocked rather than guessing.
    if (
      dependencies.platform === "darwin" &&
      stringAt(info, "OperatingSystem") === "Docker Desktop"
    ) {
      discovery.engineSocketPath = "/var/run/docker.sock";
    }
  }
  return discovery;
}

export async function discoverRuntimes(
  overrides: Partial<CheckDependencies> = {},
): Promise<RuntimeDiscovery[]> {
  const dependencies = resolveDependencies(overrides);
  const podman = await discoverPodman(dependencies);
  const docker = await discoverDocker(dependencies);
  return [podman, docker];
}

export async function discoverRuntime(
  runtime: RuntimeKind,
  overrides: Partial<CheckDependencies> = {},
): Promise<RuntimeDiscovery> {
  const dependencies = resolveDependencies(overrides);
  return runtime === "podman"
    ? discoverPodman(dependencies)
    : discoverDocker(dependencies);
}
