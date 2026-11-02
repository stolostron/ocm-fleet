import { parseJson } from "@nx/devkit";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  chmod,
  cp,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  stat,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { verifyCommit } from "./verify-commit.mjs";

const exec = promisify(execFile);
const repository = fileURLToPath(new URL("../", import.meta.url));

test("fixes fully staged files without including unrelated edits or untracked files", async (t) => {
  const f = await fixture(t);
  const pathspecName = process.platform === "win32" ? null : ":(exclude)*.txt";
  await f.stage("space in name.txt", "needs-format\n");
  if (pathspecName) await f.stage(pathspecName, "needs-format\n");
  await writeFile(join(f.root, "notes.txt"), "unfinished notes\n");
  await writeFile(join(f.root, "untracked.txt"), "private work\n");
  await f.verify();
  assert.equal(await f.index("space in name.txt"), "formatted\n");
  assert.equal(await f.read("space in name.txt"), "formatted\n");
  if (pathspecName) assert.equal(await f.index(pathspecName), "formatted\n");
  assert.equal(await f.index("notes.txt"), "notes\n");
  assert.equal(await f.read("notes.txt"), "unfinished notes\n");
  assert.equal(await f.read("untracked.txt"), "private work\n");
  assert.equal(await f.git("stash", "list"), "");
});

test("partially staged files are checked as staged and never rewritten", async (t) => {
  const f = await fixture(t);
  await f.stage("input.txt", "formatted\n");
  await writeFile(join(f.root, "input.txt"), "FAIL in unfinished work\n");
  await f.verify();
  assert.equal(await f.read("input.txt"), "FAIL in unfinished work\n");
  assert.equal(await f.index("input.txt"), "formatted\n");

  await f.stage("input.txt", "needs-format\n");
  await writeFile(
    join(f.root, "input.txt"),
    "needs-format plus unfinished work\n",
  );
  const before = await f.git("write-tree");
  await assert.rejects(f.verify(), /checks failed/);
  assert.equal(await f.git("write-tree"), before);
  assert.equal(
    await f.read("input.txt"),
    "needs-format plus unfinished work\n",
  );
});

test("supports an initial commit and skips a commit with no staged source changes", async (t) => {
  const f = await fixture(t);
  await f.verify();
  assert.match(f.output.join("\n"), /no staged source changes/);
  await f.git("update-ref", "-d", "HEAD");
  await f.stage("input.txt", "needs-format\n");
  await f.verify();
  assert.equal(await f.index("input.txt"), "formatted\n");
  assert.equal(
    await f
      .git("worktree", "list", "--porcelain")
      .then((text) => text.match(/^worktree /gm).length),
    1,
  );
});

test("one unsafe fix discards the whole proposal and checks the original index", async (t) => {
  const f = await fixture(t);
  await f.stage("input.txt", "needs-format\n");
  await f.stage("other.txt", "needs-format\n");
  await writeFile(join(f.root, "other.txt"), "unfinished work\n");
  const before = await f.git("write-tree");
  await assert.rejects(f.verify(), /checks failed/);
  assert.equal(await f.git("write-tree"), before);
  assert.equal(await f.read("input.txt"), "needs-format\n");
  assert.equal(await f.read("other.txt"), "unfinished work\n");
  // The check reads disk in the private checkout, after the proposal was discarded.
  assert.match(f.output.join("\n"), /CHECK:needs-format/);
});

test("failing tests and formatter errors leave all real files and the index intact", async (t) => {
  const f = await fixture(t);
  for (const contents of ["needs-format\nFAIL\n", "FORMAT_ERROR\n"]) {
    await f.stage("input.txt", contents);
    const before = await f.git("write-tree");
    await assert.rejects(f.verify(), /checks failed/);
    assert.equal(await f.git("write-tree"), before);
    assert.equal(await f.read("input.txt"), contents);
  }
});

test("formatters cannot expand the commit to an unstaged file", async (t) => {
  const f = await fixture(t, { baseline: "needs-format\n" });
  await f.stage("other.txt", "formatted\n");
  await assert.rejects(f.verify(), /checks failed/);
  assert.equal(await f.index("input.txt"), "needs-format\n");
  assert.equal(await f.read("input.txt"), "needs-format\n");
});

test("read-only checks cannot silently rewrite and stage the candidate", async (t) => {
  const f = await fixture(t);
  await f.stage("input.txt", "CHECK_WRITES_SOURCE\n");
  const before = await f.git("write-tree");
  await assert.rejects(f.verify(), /read-only check changed source/);
  assert.equal(await f.git("write-tree"), before);
  assert.equal(await f.read("input.txt"), "CHECK_WRITES_SOURCE\n");
});

test("cancellation before publication leaves files and staging untouched", async (t) => {
  const f = await fixture(t);
  const controller = new AbortController();
  await f.stage("input.txt", "needs-format\n");
  const before = await f.git("write-tree");
  await assert.rejects(
    f.verify({
      signal: controller.signal,
      afterCheck: () => controller.abort(),
    }),
    { name: "AbortError" },
  );
  assert.equal(await f.git("write-tree"), before);
  assert.equal(await f.read("input.txt"), "needs-format\n");
});

test("refuses local npm links outside the staged repository", async (t) => {
  const f = await fixture(t);
  await f.stage("package.json", JSON.stringify({ name: "fixture" }));
  const packages = {
    "": { name: "fixture" },
    "node_modules/external": { resolved: "../external", link: true },
  };
  await f.stage(
    "package-lock.json",
    JSON.stringify({ lockfileVersion: 3, packages }),
  );
  await mkdir(join(f.root, "node_modules"));
  await writeFile(
    join(f.root, "node_modules/.package-lock.json"),
    JSON.stringify({ packages }),
  );
  const before = await f.git("write-tree");
  await assert.rejects(f.verify(), /external local npm links/);
  assert.equal(await f.git("write-tree"), before);
});

test("publication respects another Git writer's lock", async (t) => {
  const f = await fixture(t);
  await f.stage("input.txt", "needs-format\n");
  await assert.rejects(
    f.verify({
      afterCheck: () =>
        writeFile(join(f.root, ".git/index.lock"), "another writer"),
    }),
    /EEXIST/,
  );
  assert.equal(await f.index("input.txt"), "needs-format\n");
  assert.equal(await f.read("input.txt"), "needs-format\n");
  assert.equal(await f.read(".git/index.lock"), "another writer");
});

test(
  "a publication write failure rolls back earlier writes",
  { skip: process.platform === "win32" || process.getuid?.() === 0 },
  async (t) => {
    const f = await fixture(t);
    await mkdir(join(f.root, "locked"));
    await f.stage("input.txt", "needs-format\n");
    await f.stage("locked/other.txt", "needs-format\n");
    try {
      await assert.rejects(
        f.verify({ afterCheck: () => chmod(join(f.root, "locked"), 0o555) }),
        /EACCES/,
      );
    } finally {
      await chmod(join(f.root, "locked"), 0o755);
    }
    assert.equal(await f.index("input.txt"), "needs-format\n");
    assert.equal(await f.read("input.txt"), "needs-format\n");
    assert.equal(await f.read("locked/other.txt"), "needs-format\n");
  },
);

test("index or working-file edits during verification prevent publication", async (t) => {
  for (const change of ["index", "worktree"]) {
    const f = await fixture(t);
    await f.stage("input.txt", "needs-format\n");
    await assert.rejects(
      f.verify({
        afterCheck: async () => {
          if (change === "index")
            await f.stage("notes.txt", "new staged work\n");
          else await writeFile(join(f.root, "input.txt"), "new editor work\n");
        },
      }),
      /changed during verification/,
    );
    assert.equal(await f.index("input.txt"), "needs-format\n");
    assert.equal(
      await f.read("input.txt"),
      change === "index" ? "needs-format\n" : "new editor work\n",
    );
  }
});

test("deletions, renames and executable modes survive verification", async (t) => {
  const f = await fixture(t);
  await f.git("mv", "input.txt", "renamed.txt");
  await f.git("rm", "notes.txt");
  await f.stage("executable.txt", "needs-format\n");
  await chmod(join(f.root, "executable.txt"), 0o755);
  await f.git("add", "executable.txt");
  await f.stage("group-writable.txt", "needs-format\n");
  await chmod(join(f.root, "group-writable.txt"), 0o664);
  await f.verify();
  assert.match(
    await f.git("ls-files", "--stage", "executable.txt"),
    /^100755 /,
  );
  assert.equal(await f.index("executable.txt"), "formatted\n");
  assert.equal(await f.index("group-writable.txt"), "formatted\n");
  assert.equal(
    (await stat(join(f.root, "group-writable.txt"))).mode & 0o777,
    0o664,
  );
  assert.equal(await f.index("renamed.txt"), "formatted\n");
  assert.equal(await f.git("ls-files", "notes.txt", "input.txt"), "");
});

test("honors Git's alternate commit index without staging other work", async (t) => {
  const f = await fixture(t);
  await f.stage("input.txt", "formatted staged elsewhere\n");
  const alternate = join(f.root, ".git/alternate-index");
  await cp(join(f.root, ".git/index"), alternate);
  await f.git("reset", "--", "input.txt");
  await writeFile(join(f.root, "input.txt"), "needs-format\n");
  await exec("git", ["add", "input.txt"], {
    cwd: f.root,
    env: { ...process.env, GIT_INDEX_FILE: alternate },
  });
  const before = await f.git("write-tree");
  await f.verify({ indexFile: alternate });
  assert.equal(await f.git("write-tree"), before);
  assert.equal(
    (
      await exec("git", ["show", ":input.txt"], {
        cwd: f.root,
        env: { ...process.env, GIT_INDEX_FILE: alternate },
      })
    ).stdout,
    "formatted\n",
  );
});

test("a successful manual verify warms the hook cache and workspace imports stay staged", async (t) => {
  const f = await fixture(t);
  const config = parseJson(await readFile(join(repository, "nx.json"), "utf8"));
  const workspace = JSON.parse(
    await readFile(join(repository, "project.json"), "utf8"),
  );
  // Use the actual shared JS lifecycle and workspace targets. Only the sample
  // package's test differs: it records executions, then imports a real workspace
  // dependency so an incorrectly shared node_modules directory is observable.
  await writeFile(
    join(f.root, "nx.json"),
    JSON.stringify({
      plugins: config.plugins.filter(({ plugin }) => plugin === "@nx/docker"),
      namedInputs: config.namedInputs,
      targetDefaults: config.targetDefaults,
    }),
  );
  const targets = Object.fromEntries(
    [
      "verify",
      "fmt",
      "fmt:workspace",
      "lint",
      "lint:workspace",
      "lint:format",
      "check",
      "refresh:inputs",
    ].map((name) => [name, workspace.targets[name]]),
  );
  targets.check.dependsOn = [
    "lint:workspace",
    "lint:format",
    "test:commit-fixture",
    { projects: ["*", "!ocm-fleet"], target: "check" },
  ];
  targets.lint.dependsOn = targets.lint.dependsOn.filter(
    (value) => value !== "lint:nx-projects",
  );
  targets["test:commit-fixture"] = {
    executor: "nx:run-commands",
    options: { command: "node app/test.mjs" },
    inputs: [
      "{workspaceRoot}/app/**/*",
      "{workspaceRoot}/dependency/**/*",
      "jsRuntime",
    ],
    cache: true,
    outputs: [],
  };
  await writeFile(join(f.root, "Dockerfile"), "FROM scratch\n");
  await writeFile(
    join(f.root, "project.json"),
    JSON.stringify({ name: "ocm-fleet", root: ".", targets }),
  );
  for (const path of ["app", "dependency"])
    await mkdir(join(f.root, path), { recursive: true });
  await writeFile(
    join(f.root, "app/package.json"),
    JSON.stringify({
      name: "commit-test-app",
      version: "1.0.0",
      dependencies: { "commit-test-dependency": "*" },
    }),
  );
  await writeFile(
    join(f.root, "dependency/package.json"),
    JSON.stringify({
      name: "commit-test-dependency",
      version: "1.0.0",
      type: "module",
      exports: "./value.mjs",
    }),
  );
  await writeFile(
    join(f.root, "dependency/value.mjs"),
    'export default "staged";\n',
  );
  await writeFile(join(f.root, "app/source.mjs"), 'export default "ok";\n');
  await writeFile(
    join(f.root, "app/test.mjs"),
    `import assert from "node:assert/strict";
import { appendFileSync } from "node:fs";
import value from "commit-test-dependency";
appendFileSync(${JSON.stringify(join(f.root, ".checks-ran"))}, "ran:" + value + "\\n");
assert.equal(value, "staged");
`,
  );
  const project = {
    name: "app",
    root: "app",
    tags: ["type:js-project"],
    implicitDependencies: ["dependency"],
    targets: {
      fmt: {},
      "refresh:inputs": {},
      verify: {},
      check: {},
      "lint:format": {},
      lint: {
        executor: "nx:run-commands",
        options: { command: "eslint app" },
        inputs: ["default", "eslintConfig"],
        cache: true,
        outputs: [],
      },
      test: {
        executor: "nx:noop",
        dependsOn: [{ projects: ["ocm-fleet"], target: "test:commit-fixture" }],
        cache: false,
      },
    },
  };
  await writeFile(join(f.root, "app/project.json"), JSON.stringify(project));
  await writeFile(
    join(f.root, "dependency/project.json"),
    JSON.stringify({ name: "dependency", root: "dependency", targets: {} }),
  );
  await writeFile(
    join(f.root, "eslint.config.mjs"),
    `import config from ${JSON.stringify(join(repository, "eslint.config.mjs"))};\nexport default config;\n`,
  );
  await writeFile(
    join(f.root, ".gitignore"),
    "node_modules/\n.nx/\n**/.checks-ran\n",
  );
  await cp(
    join(repository, ".prettierignore"),
    join(f.root, ".prettierignore"),
  );
  // Reuse real external dependencies while giving this fixture its own npm
  // workspace links and install inventory, just as a normal npm ci would.
  const manifest = {
    name: "commit-fixture",
    private: true,
    workspaces: ["app", "dependency"],
  };
  await writeFile(join(f.root, "package.json"), JSON.stringify(manifest));
  const lock = JSON.parse(
    await readFile(join(repository, "package-lock.json"), "utf8"),
  );
  const installed = JSON.parse(
    await readFile(join(repository, "node_modules/.package-lock.json"), "utf8"),
  );
  for (const [path, info] of Object.entries(lock.packages)) {
    if (!path.includes("node_modules/") || info.link) {
      delete lock.packages[path];
      delete installed.packages[path];
    }
  }
  lock.packages[""] = manifest;
  for (const path of ["app", "dependency"]) {
    const entry = JSON.parse(
      await readFile(join(f.root, path, "package.json"), "utf8"),
    );
    lock.packages[path] = entry;
    const modulePath = `node_modules/${entry.name}`;
    lock.packages[modulePath] = installed.packages[modulePath] = {
      resolved: path,
      link: true,
    };
  }
  await writeFile(join(f.root, "package-lock.json"), JSON.stringify(lock));
  await mkdir(join(f.root, "node_modules"));
  for (const name of await (
    await import("node:fs/promises")
  ).readdir(join(repository, "node_modules"))) {
    if (!name.startsWith("."))
      await symlink(
        join(repository, "node_modules", name),
        join(f.root, "node_modules", name),
        "dir",
      );
  }
  await mkdir(join(f.root, "node_modules/.bin"));
  for (const name of ["nx", "prettier", "eslint"])
    await symlink(
      join(repository, "node_modules/.bin", name),
      join(f.root, "node_modules/.bin", name),
    );
  for (const path of ["app", "dependency"]) {
    const { name } = JSON.parse(
      await readFile(join(f.root, path, "package.json"), "utf8"),
    );
    await symlink(
      join(f.root, path),
      join(f.root, "node_modules", name),
      "dir",
    );
  }
  await writeFile(
    join(f.root, "node_modules/.package-lock.json"),
    JSON.stringify(installed),
  );
  await f.git("add", ".");
  const nx = join(repository, "node_modules/nx/dist/bin/nx.js");
  const env = {
    ...process.env,
    NX_DAEMON: "false",
    NX_DEFAULT_OUTPUT_STYLE: "summary",
  };
  await exec(process.execPath, [nx, "run", "ocm-fleet:verify"], {
    cwd: f.root,
    env,
    maxBuffer: 4 * 1024 * 1024,
  });
  assert.equal(await f.read(".checks-ran"), "ran:staged\n");
  await f.git("add", ".");
  await f.git("commit", "-qm", "formatted baseline");
  await f.stage("input.txt", "ready to commit\n");
  // Install a real hook which invokes the production adapter. It must read the
  // snapshot package, even though its source in this checkout now differs.
  const hookDirectory = join(f.root, ".git/hooks-enabled");
  await mkdir(hookDirectory);
  await writeFile(
    join(hookDirectory, "pre-commit"),
    `#!/bin/sh\nexec "${process.execPath}" "${join(repository, "tools/verify-commit.mjs")}"\n`,
    { mode: 0o755 },
  );
  await f.git("config", "core.hooksPath", hookDirectory);
  await writeFile(
    join(f.root, "dependency/value.mjs"),
    'export default "unstaged";\n',
  );
  const result = await exec("git", ["commit", "-m", "cached staged check"], {
    cwd: f.root,
    env,
    maxBuffer: 4 * 1024 * 1024,
  });
  assert.match(result.stdout + result.stderr, /staged verification passed/);
  assert.equal(await f.read(".checks-ran"), "ran:staged\n");
  // Force a cache miss and exercise Git's actual temporary --only index while
  // fixing source. Unrelated staged work and unstaged dependency edits survive.
  await f.stage("notes.txt", "separate staged work\n");
  await f.stage("app/source.mjs", "export default 'next'");
  await exec(
    "git",
    ["commit", "--only", "app/source.mjs", "-m", "format selected source"],
    { cwd: f.root, env, maxBuffer: 4 * 1024 * 1024 },
  );
  assert.equal(await f.read("app/source.mjs"), 'export default "next";\n');
  assert.equal(
    await f.git("show", "HEAD:app/source.mjs"),
    'export default "next";\n',
  );
  assert.equal(await f.index("notes.txt"), "separate staged work\n");
  assert.equal(await f.git("show", "HEAD:notes.txt"), "notes\n");
  assert.equal(
    await f.read("dependency/value.mjs"),
    'export default "unstaged";\n',
  );
  assert.equal(await f.read(".checks-ran"), "ran:staged\nran:staged\n");
  // A stale cache key or bad workspace link would either falsely pass or fail
  // for the wrong reason. Changing staged dependency source must run the test.
  await f.git("add", "dependency/value.mjs");
  await assert.rejects(
    exec("git", ["commit", "-m", "bad staged dependency"], {
      cwd: f.root,
      env,
      maxBuffer: 4 * 1024 * 1024,
    }),
    /Command failed/,
  );
  assert.equal(
    await f.read(".checks-ran"),
    "ran:staged\nran:staged\nran:unstaged\n",
  );
  assert.equal(
    await f.index("dependency/value.mjs"),
    'export default "unstaged";\n',
  );
  await f.git("config", "core.hooksPath", join(f.root, ".git/no-hooks"));
  const before = await f.git("write-tree");
  const alteredLock = {
    ...lock,
    packages: {
      ...lock.packages,
      "node_modules/nx": {
        ...lock.packages["node_modules/nx"],
        version: "0.0.0",
      },
    },
  };
  await f.stage("package-lock.json", JSON.stringify(alteredLock));
  await assert.rejects(
    verifyCommit({ root: f.root, log: () => {} }),
    /run npm ci/,
  );
  assert.notEqual(await f.git("write-tree"), before); // only our explicit stage changed the index.
  assert.equal(await f.index("package-lock.json"), JSON.stringify(alteredLock));
});

// These tests exercise real Git and real subprocesses; the task runner below
// isolates the Git transaction from Nx startup. The integration test uses Nx.
async function fixture(t, { baseline = "formatted\n" } = {}) {
  const root = await mkdtemp(join(tmpdir(), "ocm-fleet-commit-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const git = async (...args) =>
    (await exec("git", args, { cwd: root })).stdout;
  await git("init", "-q");
  await git("config", "user.name", "Commit test");
  await git("config", "user.email", "commit-test@example.invalid");
  await git("config", "commit.gpgsign", "false");
  await git("config", "core.hooksPath", join(root, ".git/no-hooks"));
  await writeFile(join(root, "input.txt"), baseline);
  await writeFile(join(root, "notes.txt"), "notes\n");
  await writeFile(join(root, ".gitignore"), "node_modules/\n.nx/\n");
  await writeFile(
    join(root, "task.mjs"),
    `
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
const target = process.argv[2];
const files = readdirSync(".", { recursive: true }).filter(f => f.endsWith(".txt"));
if (target === "fmt") {
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    writeFileSync(file, text.replaceAll("needs-format", "formatted"));
    if (text.includes("FORMAT_ERROR")) process.exit(1);
  }
} else {
  const text = files.map(f => readFileSync(f, "utf8")).join("");
  if (target === "check") console.log("CHECK:" + text);
  if (target === "check" && text.includes("CHECK_WRITES_SOURCE")) {
    writeFileSync("input.txt", "rewritten by check\\n");
    execFileSync("git", ["add", "input.txt"]);
  }
  if (text.includes("needs-format") || text.includes("FORMAT_ERROR") || (target === "check" && text.includes("FAIL"))) process.exit(1);
}
`,
  );
  await git("add", ".");
  await git("commit", "-qm", "baseline");
  const output = [];
  return {
    root,
    git,
    output,
    read: (path) => readFile(join(root, path), "utf8"),
    index: (path) => git("show", `:${path}`),
    stage: async (path, contents) => {
      await writeFile(join(root, path), contents);
      await git("--literal-pathspecs", "add", "--", path);
    },
    verify: ({ afterCheck, indexFile, signal } = {}) =>
      verifyCommit({
        root,
        signal,
        env: {
          ...process.env,
          ...(indexFile ? { GIT_INDEX_FILE: indexFile } : {}),
        },
        log: (message) => output.push(message),
        runTarget: async (target, cwd, env) => {
          const result = await exec(process.execPath, ["task.mjs", target], {
            cwd,
            env,
          }).then(
            ({ stdout }) => {
              output.push(stdout);
              return true;
            },
            ({ stdout }) => {
              output.push(stdout);
              return false;
            },
          );
          if (target === "check") await afterCheck?.();
          return result;
        },
      }),
  };
}
