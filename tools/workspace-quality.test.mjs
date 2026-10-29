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
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const run = promisify(execFile);
const repository = fileURLToPath(new URL("../", import.meta.url));
const nx = join(repository, "node_modules/nx/dist/bin/nx.js");

test("workspace Nx configuration matches the installed schema", async () => {
  // Resolve the schema and validator from Nx so they follow its pinned version.
  const requireNx = createRequire(import.meta.resolve("nx/package.json"));
  const Ajv = requireNx("ajv");
  const ajv = new Ajv({ allErrors: true, strict: false });
  const validate = ajv.compile(requireNx("./schemas/nx-schema.json"));
  assert.equal(
    validate(await json("nx.json")),
    true,
    ajv.errorsText(validate.errors, { separator: "\n" }),
  );
});

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

test("format checks catch configuration and documentation drift without rewriting files", async (t) => {
  const { root, task } = await fixture(t);
  await task("fmt:workspace");
  await task("fmt", "cli");
  for (const [project, files] of [
    ["cli", [["client/cli/settings.json", '{"value":1}\n']]],
    [
      "ocm-fleet",
      [
        ["docs/example.md", "#  Example\n"],
        [".github/workflows/example.yaml", "name:   Example\n"],
      ],
    ],
  ]) {
    for (const [path, source] of files) {
      await mkdir(dirname(join(root, path)), { recursive: true });
      await writeFile(join(root, path), source);
    }
    await assert.rejects(task("lint:format", project));
    for (const [path, source] of files) {
      assert.equal(await readFile(join(root, path), "utf8"), source);
    }
    await task(project === "ocm-fleet" ? "fmt:workspace" : "fmt", project);
    await task("lint:format", project);
    // A prior success must not hide a subsequent formatting regression.
    const [path, source] = files[0];
    await writeFile(join(root, path), source);
    await assert.rejects(task("lint:format", project));
  }
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
  await task("fmt");
  await writeFile(path, "export const = ;\n");
  await assert.rejects(task("fmt", "cli"), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /Parsing error|SyntaxError/);
    return true;
  });
  await writeFile(path, "export const value: any = 'hello'\n");
  await assert.rejects(task("fix", "cli"), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /no-explicit-any/);
    return true;
  });
  await writeFile(path, 'export const value = "hello";\n');
  await writeFile(
    join(root, "client/cli/src/example.css"),
    "a { color: #zzzzzz; }\n",
  );
  await assert.rejects(task("fix", "cli"), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /color-no-invalid-hex/);
    return true;
  });
});

test("project unit tests stay scoped and invalidate their cache for source and dependency changes", async (t) => {
  const { root, task } = await fixture(t);
  await cp(
    join(repository, "vitest.config.ts"),
    join(root, "vitest.config.ts"),
  );
  for (const [name, directory] of [
    ["common", "sdk/common"],
    ["build-utils", "sdk/build-utils"],
  ]) {
    const project = await json(`${directory}/project.json`);
    await mkdir(join(root, directory, "src/__tests__"), { recursive: true });
    await writeJSON(root, `${directory}/project.json`, {
      name,
      root: directory,
      tags: project.tags,
      implicitDependencies: name === "build-utils" ? ["common"] : [],
      targets: { test: project.targets.test, "refresh:inputs": {} },
    });
    await writeFile(
      join(root, directory, "src/__tests__/scope.test.ts"),
      `
import { appendFileSync } from "node:fs";
import { expect, test } from "vitest";
import { value } from ${JSON.stringify(join(root, "sdk/common/src/value.ts"))};
test("reads real source", () => {
  expect(value).toBeGreaterThan(0);
  appendFileSync(${JSON.stringify(join(root, `${name}-tested`))}, "passed");
});
`,
    );
  }
  const source = join(root, "sdk/common/src/value.ts");
  const marker = (name) => readFile(join(root, `${name}-tested`), "utf8");
  await writeFile(source, "export const value = 1;\n");
  await task("test", "common");
  assert.equal(await marker("common"), "passed");
  await assert.rejects(marker("build-utils"), { code: "ENOENT" });
  await task("test", "common");
  assert.equal(
    await marker("common"),
    "passed",
    "unchanged tests must reuse the cache",
  );
  await writeFile(source, "export const value = 2;\n");
  await task("refresh:inputs", "common");
  await task("test", "common");
  assert.equal(
    await marker("common"),
    "passedpassed",
    "source changes must rerun tests",
  );
  await task("test", "build-utils");
  assert.equal(await marker("build-utils"), "passed");
  await writeFile(source, "export const value = 3;\n");
  await task("refresh:inputs", "common");
  await task("test", "build-utils");
  assert.equal(
    await marker("build-utils"),
    "passedpassed",
    "dependency changes must rerun consumers' tests",
  );
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
  for (const target of [
    "lint",
    "lint:format",
    "lint:css",
    "test",
    "test:ct",
    "typecheck",
  ]) {
    assert.ok(dependencies["cli:check"].includes(`cli:${target}`));
  }
  for (const project of ["server", "proto", "cli", "e2e-cli"]) {
    assert.ok(dependencies["ocm-fleet:check"].includes(`${project}:check`));
  }
  assert.ok(dependencies["ocm-fleet:check"].includes("ocm-fleet:lint:format"));
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
  for (const name of ["golangci-lint", "goimports", "format-go"]) {
    await symlink(
      join(repository, "bin/tools", name),
      join(root, "bin/tools", name),
    );
  }
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
      "lint:format",
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
    [
      "fmt",
      "verify",
      "fix",
      "fix:lint",
      "refresh:inputs",
      "check",
      "lint",
      "lint:format",
    ].map((name) => [name, cli.targets[name]]),
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
    task: (target, project = "ocm-fleet") =>
      runNx(["run", `${project}:${target}`, "--outputStyle=static"]),
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
