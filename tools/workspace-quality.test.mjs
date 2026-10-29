import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import {
  cp,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const run = promisify(execFile);
const repository = fileURLToPath(new URL("../", import.meta.url));
const nx = join(repository, "node_modules/nx/dist/bin/nx.js");

test("workspace verification formats Go, TypeScript, styles and protobuf before checks", async (t) => {
  const { root, task } = await fixture(t);
  await task("verify");
  assert.match(
    await readFile(join(root, "server/example.go"), "utf8"),
    /func Example\(\) int \{ return 1 \}/,
  );
  assert.match(
    await readFile(join(root, "client/cli/src/example.ts"), "utf8"),
    /export const value = "hello";/,
  );
  assert.match(
    await readFile(join(root, "client/cli/src/example.css"), "utf8"),
    /color: red;/,
  );
  assert.match(
    await readFile(join(root, "client/cli/src/exports.ts"), "utf8"),
    /node:fs[\s\S]*node:path/,
  );
  assert.match(
    await readFile(join(root, "proto/example/v1/greeting.proto"), "utf8"),
    /  string name = 1;/,
  );
  for (const marker of ["tested", "component-tested", "typechecked"]) {
    assert.equal(await readFile(join(root, marker), "utf8"), "passed");
  }
  // A second check should reuse successful checks of the rewritten inputs.
  await task("check");
  assert.equal(await readFile(join(root, "tested"), "utf8"), "passed");
});

test("workspace lint is read-only and propagates findings", async (t) => {
  const { root, task } = await fixture(t);
  const path = join(root, "client/cli/src/example.ts");
  const source = await readFile(path, "utf8");
  await assert.rejects(task("lint"));
  assert.equal(await readFile(path, "utf8"), source);
});

test("fmt leaves broader fixes for fix and ignores generated files", async (t) => {
  const { root, task } = await fixture(t);
  const path = join(root, "client/cli/src/example.ts");
  await writeFile(path, "export let value='hello'\n");
  const styles = join(root, "client/cli/src/example.css");
  await writeFile(styles, "a{color:#ffffff}\n");
  const generated = join(root, "client/cli/dist/example.js");
  await mkdir(dirname(generated));
  await writeFile(generated, "export let value='generated'\n");
  await task("fmt");
  assert.match(await readFile(path, "utf8"), /export let value = "hello";/);
  assert.match(await readFile(styles, "utf8"), /#ffffff/);
  await assert.rejects(task("check"));
  await task("fix");
  assert.match(await readFile(path, "utf8"), /export const value = "hello";/);
  assert.match(await readFile(styles, "utf8"), /#fff;/);
  assert.equal(
    await readFile(generated, "utf8"),
    "export let value='generated'\n",
  );
});

test("formatter parse failures and unfixable lint findings remain failures", async (t) => {
  const { root, task } = await fixture(t);
  const path = join(root, "client/cli/src/example.ts");
  await writeFile(path, "export const = ;\n");
  await assert.rejects(task("fmt"), { code: 1 });
  await writeFile(path, "export const value: any = 'hello'\n");
  await assert.rejects(task("fix"), { code: 1 });
  await writeFile(path, 'export const value = "hello";\n');
  await writeFile(
    join(root, "client/cli/src/example.css"),
    "a { color: #zzzzzz; }\n",
  );
  await assert.rejects(task("fix"), { code: 1 });
});

test("Nx checks include component tests and omit inferred end-to-end suites", async (t) => {
  const { root, graph } = await fixture(t);
  await mkdir(join(root, "e2e/cli"), { recursive: true });
  await writeJSON(
    root,
    "e2e/cli/project.json",
    await json("e2e/cli/project.json"),
  );
  await cp(
    join(repository, "e2e/cli/playwright.config.ts"),
    join(root, "e2e/cli/playwright.config.ts"),
  );
  const { tasks, dependencies } = await graph("check");
  for (const target of ["lint", "lint:css", "test", "test:ct", "typecheck"]) {
    assert.ok(dependencies["cli:check"].includes(`cli:${target}`));
  }
  for (const project of ["server", "proto", "cli", "e2e-cli"]) {
    assert.ok(dependencies["ocm-fleet:check"].includes(`${project}:check`));
  }
  assert.equal(Object.hasOwn(tasks, "e2e-cli:test:ct"), false);
  for (const task of Object.values(tasks)) {
    assert.equal(
      ["test-integration", "test:e2e"].includes(task.target.target),
      false,
    );
  }
});

// Exercise production Nx definitions with real tools and disposable source files.
// The normal and component checks observe the formatted files on disk; no test
// depends on Nx's console output or replaces a formatter/linter implementation.
async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "ocm-fleet-workspace-quality-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const directory of [
    "bin/tools",
    "client/cli/src",
    "server",
    "proto/example/v1",
  ]) {
    await mkdir(join(root, directory), { recursive: true });
  }
  await symlink(
    join(repository, "node_modules"),
    join(root, "node_modules"),
    "dir",
  );
  await symlink(
    join(repository, "bin/tools/golangci-lint"),
    join(root, "bin/tools/golangci-lint"),
  );
  for (const path of [".golangci.yaml", ".prettierignore"]) {
    await cp(join(repository, path), join(root, path));
  }
  await writeFile(
    join(root, ".gitignore"),
    "node_modules/\n.nx/\nbin/\n**/dist/\ntested\ncomponent-tested\ntypechecked\n",
  );
  const config = await json("nx.json");
  await writeJSON(root, "nx.json", {
    plugins: config.plugins.filter(
      ({ plugin }) => plugin === "@nx/playwright/plugin",
    ),
    targetDefaults: config.targetDefaults,
    namedInputs: {
      ...config.namedInputs,
      goLintConfig: ["{workspaceRoot}/.golangci.yaml"],
    },
  });
  await writeJSON(root, "package.json", {
    name: "workspace-quality-fixture",
    private: true,
    type: "module",
  });
  await writeFile(
    join(root, "eslint.config.mjs"),
    `import config from ${JSON.stringify(join(repository, "eslint.config.mjs"))};\nexport default [...config, {files: ["**/*.ts"], rules: {"prefer-const": "error"}}];\n`,
  );
  await writeFile(
    join(root, "stylelint.config.mjs"),
    'export default { rules: { "color-no-invalid-hex": true, "color-hex-length": "short" } };\n',
  );
  await writeFile(
    join(root, "buf.yaml"),
    "version: v2\nmodules:\n  - path: proto\n",
  );
  const workspace = await json("project.json");
  const targets = Object.fromEntries(
    [
      "fmt",
      "verify",
      "fix",
      "lint",
      "check",
      "fmt:workspace",
      "fix:workspace",
      "lint:workspace",
      "refresh:inputs",
    ].map((name) => [name, workspace.targets[name]]),
  );
  // Tool setup is already done by this suite's Nx prerequisite. Keep config
  // verification real without copying the repository's self-testing targets.
  targets["lint:go:config"] = {
    executor: "nx:run-commands",
    options: {
      command: "bin/tools/golangci-lint config verify --config .golangci.yaml",
    },
    cache: false,
  };
  await writeJSON(root, "project.json", {
    name: "ocm-fleet",
    root: ".",
    targets,
  });
  const go = await json("server/project.json");
  await writeJSON(root, "server/project.json", {
    name: "server",
    tags: go.tags,
    targets: Object.fromEntries(
      [
        "fmt",
        "verify",
        "fix",
        "fix:lint",
        "refresh:inputs",
        "lint",
        "check",
      ].map((name) => [name, go.targets[name]]),
    ),
  });
  await writeFile(
    join(root, "server/go.mod"),
    "module example.com/server\n\ngo 1.26.0\n",
  );
  await writeFile(
    join(root, "server/example.go"),
    "package server\nfunc Example() int {return 1}\n",
  );
  const cli = await json("client/cli/project.json");
  const cliTargets = Object.fromEntries(
    ["fmt", "verify", "fix", "fix:lint", "refresh:inputs", "check", "lint"].map(
      (name) => [name, cli.targets[name]],
    ),
  );
  cliTargets["lint:css"] = {
    executor: "nx:run-commands",
    options: {
      command:
        'stylelint "src/**/*.css" --config "$NX_WORKSPACE_ROOT/stylelint.config.mjs"',
      cwd: "client/cli",
    },
  };
  cliTargets["fix:css"] = {};
  for (const [target, marker] of [
    ["test", "tested"],
    ["test:ct", "component-tested"],
    ["typecheck", "typechecked"],
  ]) {
    cliTargets[target] = {
      executor: "nx:run-commands",
      options: { command: `node check.mjs ${marker}` },
      cache: true,
      inputs: [
        "{workspaceRoot}/server/**/*.go",
        "{workspaceRoot}/client/cli/src/**/*",
        "{workspaceRoot}/proto/**/*.proto",
      ],
      outputs: [],
    };
  }
  await writeJSON(root, "client/cli/project.json", {
    name: "cli",
    root: "client/cli",
    tags: cli.tags,
    targets: cliTargets,
  });
  await writeFile(
    join(root, "client/cli/src/example.ts"),
    "export const value='hello'\n",
  );
  await writeFile(
    join(root, "client/cli/src/exports.ts"),
    'export { basename } from "node:path";\nexport { stat } from "node:fs";\n',
  );
  await writeFile(join(root, "client/cli/src/example.css"), "a{color:red}\n");
  await writeJSON(root, "proto/project.json", await json("proto/project.json"));
  await writeFile(
    join(root, "proto/example/v1/greeting.proto"),
    'syntax="proto3";package example.v1;message Greeting{string name=1;}\n',
  );
  await writeFile(
    join(root, "check.mjs"),
    `import assert from "node:assert/strict";
import { readFile, appendFile } from "node:fs/promises";
assert.match(await readFile("server/example.go", "utf8"), /func Example\\(\\) int \\{ return 1 \\}/);
assert.match(await readFile("client/cli/src/example.ts", "utf8"), /value = "hello";/);
assert.match(await readFile("proto/example/v1/greeting.proto", "utf8"), /  string name = 1;/);
await appendFile(process.argv[2], "passed");
`,
  );
  const runNx = (args) =>
    run(process.execPath, [nx, ...args], {
      cwd: root,
      env: {
        ...process.env,
        NX_DAEMON: "false",
        NX_ISOLATE_PLUGINS: "false",
        NX_NO_CLOUD: "true",
      },
      maxBuffer: 8 * 1024 * 1024,
    });
  return {
    root,
    task: (target) =>
      runNx(["run", `ocm-fleet:${target}`, "--outputStyle=static"]),
    graph: async (target) => {
      const path = join(root, ".nx", "quality-task-graph.json");
      await mkdir(dirname(path), { recursive: true });
      await runNx(["run", `ocm-fleet:${target}`, `--graph=${path}`]);
      return JSON.parse(await readFile(path, "utf8")).tasks;
    },
  };
}

async function json(path) {
  return JSON.parse(await readFile(join(repository, path), "utf8"));
}

async function writeJSON(root, path, value) {
  await writeFile(join(root, path), JSON.stringify(value));
}
