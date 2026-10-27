import { createHash } from "node:crypto";
import { mkdtemp, readFile, rename, rm, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const platforms = [
  "darwin-amd64",
  "darwin-arm64",
  "linux-amd64",
  "linux-arm64",
];
const defaultPinPath = fileURLToPath(
  new URL("./golangci-lint-release.json", import.meta.url),
);

// Prepare one reviewable pin change without extracting or executing a release.
// The configurable origins let tests exercise the same checks over loopback HTTP.
export async function updateGolangciLint({
  version,
  pinPath = defaultPinPath,
  apiBaseUrl = "https://api.github.com",
  releaseBaseUrl = "https://github.com/golangci/golangci-lint/releases/download",
} = {}) {
  if (
    typeof version !== "string" ||
    !/^v?2\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)
  ) {
    throw new Error("Provide a stable v2 version with --release=v2.X.Y");
  }
  version = version.replace(/^v/, "");
  const original = await readFile(pinPath, "utf8");
  const current = JSON.parse(original);
  const metadata = await (
    await request(
      `${apiBaseUrl}/repos/golangci/golangci-lint/releases/tags/v${version}`,
    )
  ).json();
  if (
    metadata.tag_name !== `v${version}` ||
    metadata.draft !== false ||
    metadata.prerelease !== false
  ) {
    throw new Error(`Expected a stable published release for v${version}`);
  }

  const checksumsAsset = releaseAsset(
    metadata,
    `golangci-lint-${version}-checksums.txt`,
    version,
    releaseBaseUrl,
  );
  const archives = platforms.map((host) =>
    releaseAsset(
      metadata,
      `golangci-lint-${version}-${host}.tar.gz`,
      version,
      releaseBaseUrl,
    ),
  );
  const document = Buffer.from(
    await (await request(checksumsAsset.url)).arrayBuffer(),
  );
  if (
    createHash("sha256").update(document).digest("hex") !==
    checksumsAsset.sha256
  ) {
    throw new Error("Checksum document mismatch with GitHub release metadata");
  }
  const checksums = parseChecksums(document.toString("utf8"));
  const next = { version, sha256: {} };
  for (const [index, archive] of archives.entries()) {
    if (checksums.get(archive.name) !== archive.sha256) {
      throw new Error(
        `Checksum mismatch or missing checksum for ${archive.name}`,
      );
    }
    next.sha256[platforms[index]] = archive.sha256;
  }

  const sameVersion = current.version === version;
  if (
    sameVersion &&
    platforms.some((host) => current.sha256[host] !== next.sha256[host])
  ) {
    throw new Error(
      `Version v${version} is already pinned with different hashes; investigate the changed assets`,
    );
  }

  // These sources share upstream trust. Agreement catches inconsistent publishing
  // and corrupted downloads; it does not authenticate a compromised publisher.
  await Promise.all(
    archives.map(async (archive) => {
      const response = await request(archive.url);
      const hash = createHash("sha256");
      for await (const chunk of response.body) hash.update(chunk);
      if (hash.digest("hex") !== archive.sha256) {
        throw new Error(`Archive checksum mismatch for ${archive.name}`);
      }
    }),
  );

  if (!sameVersion) {
    // Stage beside the pin so the final rename publishes the complete update.
    const staging = await mkdtemp(
      join(dirname(pinPath), ".golangci-lint-update-"),
    );
    try {
      const stagedPin = join(staging, "release.json");
      await writeFile(stagedPin, JSON.stringify(next, null, 2) + "\n");
      if ((await readFile(pinPath, "utf8")) !== original) {
        throw new Error(
          "The release pin changed during the update; retry after reviewing that edit",
        );
      }
      await rename(stagedPin, pinPath);
    } finally {
      await rm(staging, { recursive: true, force: true });
    }
  }
  return { changed: !sameVersion, previousVersion: current.version, version };
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const { values } = parseArgs({ options: { release: { type: "string" } } });
    console.log(
      "Checking upstream metadata, checksums, and all four platform archives...",
    );
    const result = await updateGolangciLint({ version: values.release });
    console.log(
      result.changed
        ? `Updated golangci-lint pin: v${result.previousVersion} -> v${result.version}`
        : `Verified golangci-lint v${result.version}; pin unchanged`,
    );
    console.log(
      `Release notes: https://github.com/golangci/golangci-lint/releases/tag/v${result.version}`,
    );
    if (result.changed)
      console.log(
        "Review the release notes and pin diff before committing; run the setup target to install.",
      );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

function releaseAsset(metadata, name, version, releaseBaseUrl) {
  const matches = metadata.assets?.filter((asset) => asset.name === name) ?? [];
  const asset = matches[0];
  const url = `${releaseBaseUrl}/v${version}/${name}`;
  if (
    matches.length !== 1 ||
    asset.state !== "uploaded" ||
    asset.browser_download_url !== url
  ) {
    throw new Error(`Missing or unexpected release asset: ${name}`);
  }
  if (!/^sha256:[a-f0-9]{64}$/.test(asset.digest ?? "")) {
    throw new Error(`Missing GitHub SHA-256 digest for ${name}`);
  }
  return { name, url, sha256: asset.digest.slice(7) };
}

function parseChecksums(document) {
  const checksums = new Map();
  for (const line of document.trim().split(/\r?\n/)) {
    const match = /^([a-f0-9]{64})\s+\*?(\S+)$/.exec(line);
    if (!match) throw new Error("Malformed release checksum document");
    if (checksums.has(match[2]))
      throw new Error(`Duplicate checksum for ${match[2]}`);
    checksums.set(match[2], match[1]);
  }
  return checksums;
}

async function request(url) {
  const response = await fetch(url, {
    headers: { "User-Agent": "ocm-fleet-tool-update" },
    signal: AbortSignal.timeout(120_000),
  });
  if (!response.ok)
    throw new Error(`Download failed: ${response.status} ${url}`);
  return response;
}
