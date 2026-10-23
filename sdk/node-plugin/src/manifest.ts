/** Manifest fields consumed by SDK-shaped plugin registries and Node hosts. */
export type NodeRemoteManifest = {
  name: string;
  version: string;
  registrationMethod: "custom";
  baseURL: "auto";
  extensions: unknown[];
  loadScripts: string[];
  remoteEntry: string;
  runtimeDependencies: string[];
};

export type CreateNodeRemoteManifestOptions = {
  name: string;
  version: string;
  remoteEntryFilename: string;
  loadScripts: string[];
  extensions: unknown[];
};

const normalizeAssetPath = (assetPath: string): string =>
  assetPath
    .split(/[?#]/, 1)[0]
    .replaceAll("\\", "/")
    .replace(/^\.\//, "")
    .replace(/^\/+/, "");

export type RemoteEntryMatch =
  | { status: "matched"; script: string }
  | { status: "missing" }
  | { status: "ambiguous" };

/** Match without relying on exception text or script ordering. */
export const findRemoteEntry = (
  loadScripts: string[],
  remoteEntry: string,
): RemoteEntryMatch => {
  const normalizedEntry = normalizeAssetPath(remoteEntry);
  if (!normalizedEntry) return { status: "missing" };

  const matches = loadScripts.filter((script) => {
    const normalizedScript = normalizeAssetPath(script);
    return (
      normalizedScript === normalizedEntry ||
      normalizedScript.endsWith(`/${normalizedEntry}`)
    );
  });

  if (matches.length === 1) return { status: "matched", script: matches[0] };
  return { status: matches.length === 0 ? "missing" : "ambiguous" };
};

/**
 * Find the declared remote entry among loadScripts, including after a host has
 * prefixed each load script with its asset base path.
 */
export const identifyRemoteEntry = (
  loadScripts: string[],
  remoteEntry: string,
): string => {
  if (!normalizeAssetPath(remoteEntry)) {
    throw new Error("Node remote manifest must declare a remoteEntry");
  }
  const match = findRemoteEntry(loadScripts, remoteEntry);
  if (match.status === "missing") {
    throw new Error(
      `Node remote entry "${remoteEntry}" is not present in loadScripts`,
    );
  }
  if (match.status === "ambiguous") {
    throw new Error(
      `Node remote entry "${remoteEntry}" is ambiguous in loadScripts`,
    );
  }
  return match.script;
};

/** Create a plugin-loader-compatible manifest from emitted entrypoint assets. */
export const createNodeRemoteManifest = ({
  name,
  version,
  remoteEntryFilename,
  loadScripts,
  extensions,
}: CreateNodeRemoteManifestOptions): NodeRemoteManifest => {
  if (!name.trim()) throw new Error("Node remote name must not be empty");
  if (!version.trim()) throw new Error("Node remote version must not be empty");

  const scripts = loadScripts.filter((script) => /\.(?:c|m)?js$/i.test(script));
  const remoteEntry = identifyRemoteEntry(scripts, remoteEntryFilename);
  const runtimeDependencies = scripts.filter(
    (script) => script !== remoteEntry,
  );

  return {
    name,
    version,
    registrationMethod: "custom",
    baseURL: "auto",
    extensions,
    loadScripts: [...runtimeDependencies, remoteEntry],
    remoteEntry,
    runtimeDependencies,
  };
};
