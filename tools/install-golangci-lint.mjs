import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import {
  chmod,
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { join, resolve } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import pinnedRelease from "./golangci-lint-release.json" with { type: "json" };

const run = promisify(execFile);
const workspaceRoot = fileURLToPath(new URL("../", import.meta.url));

// The optional release and origin let tests use real archives over loopback HTTP.
// The CLI always uses the checked-in release pin and upstream release assets.
export async function installGolangciLint({
  installDir = join(workspaceRoot, "bin/tools"),
  release = pinnedRelease,
  platform = process.platform,
  arch = process.arch,
  releaseBaseUrl = "https://github.com/golangci/golangci-lint/releases/download",
} = {}) {
  const goArch = { x64: "amd64", arm64: "arm64" }[arch];
  const host = `${platform}-${goArch}`;
  const archiveSha256 = release.sha256[host];
  if (!archiveSha256) {
    throw new Error(
      `Unsupported platform for golangci-lint: ${platform}-${arch}`,
    );
  }

  const binaryPath = join(installDir, "golangci-lint");
  const stampPath = join(installDir, ".golangci-lint.json");
  const releaseIdentity = { version: release.version, host, archiveSha256 };
  if (await isInstalled(binaryPath, stampPath, releaseIdentity))
    return binaryPath;

  await mkdir(installDir, { recursive: true });
  const staging = await mkdtemp(join(installDir, ".golangci-lint-"));
  try {
    const name = `golangci-lint-${release.version}-${host}`;
    const url = `${releaseBaseUrl}/v${release.version}/${name}.tar.gz`;
    const response = await fetch(url, { signal: AbortSignal.timeout(120_000) });
    if (!response.ok)
      throw new Error(`Download failed: ${response.status} ${url}`);

    const archivePath = join(staging, "release.tar.gz");
    await pipeline(
      Readable.fromWeb(response.body),
      createWriteStream(archivePath),
    );
    if ((await sha256(archivePath)) !== archiveSha256) {
      throw new Error(`Checksum mismatch for ${url}`);
    }

    // Extract only the executable, after checking the archive against our pin.
    await run("tar", [
      "-xzf",
      archivePath,
      "-C",
      staging,
      "--strip-components=1",
      `${name}/golangci-lint`,
    ]);
    const stagedBinary = join(staging, "golangci-lint");
    await chmod(stagedBinary, 0o755);
    await writeFile(
      join(staging, "stamp.json"),
      JSON.stringify({
        ...releaseIdentity,
        binarySha256: await sha256(stagedBinary),
      }),
    );

    // Publish complete files so an interrupted install can be retried safely.
    await rename(stagedBinary, binaryPath);
    await rename(join(staging, "stamp.json"), stampPath);
    return binaryPath;
  } finally {
    await rm(staging, { recursive: true, force: true });
  }
}

async function isInstalled(binaryPath, stampPath, releaseIdentity) {
  try {
    const stamp = JSON.parse(await readFile(stampPath, "utf8"));
    const binary = await stat(binaryPath);
    return (
      stamp?.version === releaseIdentity.version &&
      stamp.host === releaseIdentity.host &&
      stamp.archiveSha256 === releaseIdentity.archiveSha256 &&
      binary.isFile() &&
      (binary.mode & 0o111) !== 0 &&
      stamp.binarySha256 === (await sha256(binaryPath))
    );
  } catch (error) {
    if (error.code === "ENOENT" || error instanceof SyntaxError) return false;
    throw error;
  }
}

async function sha256(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest("hex");
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  console.log(
    `Ensuring golangci-lint v${pinnedRelease.version} (downloads if needed)`,
  );
  try {
    console.log(`Ready: ${await installGolangciLint()}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
