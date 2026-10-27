/* eslint playwright/expect-expect: ["error", { "assertFunctionNames": ["equal", "deepEqual", "ok", "rejects"] }] */
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { serveFixtures } from "./test-support/http-server.mjs";
import { updateGolangciLint } from "./update-golangci-lint.mjs";

test("updates all four pins after verifying the release archives", async (t) => {
  const { options, expected, requests, directory } = await fixture(t);
  const result = await updateGolangciLint(options);

  assert.equal(result.changed, true);
  assert.deepEqual(
    JSON.parse(await readFile(options.pinPath, "utf8")),
    expected,
  );
  assert.equal(requests.length, 6);
  assert.deepEqual(await readdir(directory), ["release.json"]);
});

test("verifies an already pinned version without rewriting the file", async (t) => {
  const { options, expected } = await fixture(t);
  const original = JSON.stringify(expected);
  await writeFile(options.pinPath, original);

  assert.equal((await updateGolangciLint(options)).changed, false);
  assert.equal(await readFile(options.pinPath, "utf8"), original);
});

test("rejects implicit versions, prereleases, and unsupported major versions before fetching", async (t) => {
  const { options, requests } = await fixture(t);
  for (const version of [
    undefined,
    "latest",
    "2.16.0-beta.1",
    "3.0.0",
    "../../other",
  ]) {
    await assert.rejects(
      updateGolangciLint({ ...options, version }),
      /stable v2 version/i,
    );
  }
  assert.deepEqual(requests, []);
});

test("rejects draft, prerelease, and mismatched releases", async (t) => {
  for (const fields of [
    { draft: true },
    { prerelease: true },
    { tag_name: "v2.17.0" },
  ]) {
    const { options, metadata, publishMetadata, original } = await fixture(t);
    Object.assign(metadata, fields);
    publishMetadata();
    await assert.rejects(
      updateGolangciLint(options),
      /stable published release/i,
    );
    assert.equal(await readFile(options.pinPath, "utf8"), original);
  }
});

test("requires each platform archive and its GitHub SHA-256 digest", async (t) => {
  for (const remove of [
    (metadata) => metadata.assets.pop(),
    (metadata) => {
      delete metadata.assets.at(-1).digest;
    },
  ]) {
    const { options, metadata, publishMetadata, original } = await fixture(t);
    remove(metadata);
    publishMetadata();
    await assert.rejects(updateGolangciLint(options), /asset|digest/i);
    assert.equal(await readFile(options.pinPath, "utf8"), original);
  }
});

test("rejects a checksum document that disagrees with GitHub metadata", async (t) => {
  const { options, responses, checksumsPath, original } = await fixture(t);
  responses.set(checksumsPath, "changed checksum document\n");
  await assert.rejects(updateGolangciLint(options), /checksum.*mismatch/i);
  assert.equal(await readFile(options.pinPath, "utf8"), original);
});

test("rejects unexpected asset download URLs", async (t) => {
  const { options, metadata, publishMetadata, requests, original } =
    await fixture(t);
  metadata.assets.at(-1).browser_download_url =
    "https://example.invalid/archive.tar.gz";
  publishMetadata();
  await assert.rejects(
    updateGolangciLint(options),
    /unexpected release asset/i,
  );
  assert.equal(requests.length, 1);
  assert.equal(await readFile(options.pinPath, "utf8"), original);
});

test("rejects incomplete, duplicate, or inconsistent archive checksums", async (t) => {
  for (const change of [
    (lines) => lines.slice(1),
    (lines) => [...lines, lines[0]],
    (lines) => ["0".repeat(64) + lines[0].slice(64), ...lines.slice(1)],
  ]) {
    const {
      options,
      metadata,
      publishMetadata,
      responses,
      checksumsPath,
      original,
    } = await fixture(t);
    const document =
      change(responses.get(checksumsPath).trim().split("\n")).join("\n") + "\n";
    responses.set(checksumsPath, document);
    metadata.assets[0].digest = `sha256:${digest(document)}`;
    publishMetadata();
    await assert.rejects(updateGolangciLint(options), /checksum|duplicate/i);
    assert.equal(await readFile(options.pinPath, "utf8"), original);
  }
});

test("rejects changed archive bytes without partially updating the pin", async (t) => {
  const { options, responses, metadata, original, directory } =
    await fixture(t);
  const path = new URL(metadata.assets.at(-1).browser_download_url).pathname;
  responses.set(path, "changed archive");

  await assert.rejects(updateGolangciLint(options), /checksum.*mismatch/i);
  assert.equal(await readFile(options.pinPath, "utf8"), original);
  assert.deepEqual(await readdir(directory), ["release.json"]);
});

test("refuses to change the hashes for an already pinned version", async (t) => {
  const { options, original } = await fixture(t);
  const pin = JSON.parse(original);
  pin.version = "2.16.0";
  const sameVersionPin = JSON.stringify(pin);
  await writeFile(options.pinPath, sameVersionPin);

  await assert.rejects(
    updateGolangciLint(options),
    /already pinned.*different/i,
  );
  assert.equal(await readFile(options.pinPath, "utf8"), sameVersionPin);
});

test("leaves the pin untouched when the release is unavailable", async (t) => {
  const { options, responses, original } = await fixture(t);
  responses.clear();
  await assert.rejects(updateGolangciLint(options), /404/);
  assert.equal(await readFile(options.pinPath, "utf8"), original);
});

test("preserves edits made while the upstream release is being verified", async (t) => {
  const { options, responses, metadata, original, directory } =
    await fixture(t);
  const path = new URL(metadata.assets.at(-1).browser_download_url).pathname;
  const archive = responses.get(path);
  const concurrentEdit = original + "\n";
  responses.set(path, async () => {
    await writeFile(options.pinPath, concurrentEdit);
    return archive;
  });

  await assert.rejects(
    updateGolangciLint(options),
    /pin changed during the update/i,
  );
  assert.equal(await readFile(options.pinPath, "utf8"), concurrentEdit);
  assert.deepEqual(await readdir(directory), ["release.json"]);
});

async function fixture(t) {
  const directory = await mkdtemp(join(tmpdir(), "golangci-lint-update-test-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const http = await serveFixtures(t);
  const version = "2.16.0";
  const assetPath = `/download/v${version}`;
  const platforms = [
    "darwin-amd64",
    "darwin-arm64",
    "linux-amd64",
    "linux-arm64",
  ];
  const expected = { version, sha256: {} };
  const metadata = {
    tag_name: `v${version}`,
    draft: false,
    prerelease: false,
    assets: [],
  };
  const checksumLines = [];
  for (const host of platforms) {
    const name = `golangci-lint-${version}-${host}.tar.gz`;
    const body = Buffer.from(`fixture archive ${host}`);
    expected.sha256[host] = digest(body);
    checksumLines.push(`${digest(body)}  ${name}`);
    http.responses.set(`${assetPath}/${name}`, body);
    metadata.assets.push({
      name,
      state: "uploaded",
      digest: `sha256:${digest(body)}`,
      browser_download_url: `${http.baseUrl}${assetPath}/${name}`,
    });
  }
  const checksumsName = `golangci-lint-${version}-checksums.txt`;
  const checksumsPath = `${assetPath}/${checksumsName}`;
  const checksums = checksumLines.join("\n") + "\n";
  http.responses.set(checksumsPath, checksums);
  metadata.assets.unshift({
    name: checksumsName,
    state: "uploaded",
    digest: `sha256:${digest(checksums)}`,
    browser_download_url: `${http.baseUrl}${checksumsPath}`,
  });
  const publishMetadata = () =>
    http.responses.set(
      `/repos/golangci/golangci-lint/releases/tags/v${version}`,
      JSON.stringify(metadata),
    );
  publishMetadata();
  const pinPath = join(directory, "release.json");
  const original = JSON.stringify({
    version: "2.15.0",
    sha256: Object.fromEntries(platforms.map((host) => [host, "0".repeat(64)])),
  });
  await writeFile(pinPath, original);
  return {
    ...http,
    directory,
    expected,
    metadata,
    publishMetadata,
    checksumsPath,
    original,
    options: {
      version: `v${version}`,
      pinPath,
      apiBaseUrl: http.baseUrl,
      releaseBaseUrl: `${http.baseUrl}/download`,
    },
  };
}

function digest(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}
