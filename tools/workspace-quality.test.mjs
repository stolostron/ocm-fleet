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
import { basename, dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
// Nx has no public fileset-expansion API; use the pinned implementation so
// these fileset checks use named inputs and exclusions as Nx reads them.
import {
  filterUsingGlobPatterns,
  getTargetInputs,
} from "nx/src/hasher/task-hasher";

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

test("summary output suppresses task output and preserves nested failure logs", async (t) => {
  const { root, runNx } = await fixture(t);
  const project = JSON.parse(
    await readFile(join(root, "client/cli/project.json"), "utf8"),
  );
  project.targets["summary-inner"] = {
    executor: "nx:run-commands",
    options: { command: "node fail.mjs" },
  };
  project.targets["summary-outer"] = {
    executor: "nx:run-commands",
    options: { command: "npx nx run cli:summary-inner" },
  };
  project.targets["summary-success"] = {
    executor: "nx:run-commands",
    options: { command: "node success.mjs" },
  };
  await writeJSON(root, "client/cli/project.json", project);
  await writeFile(
    join(root, "success.mjs"),
    'console.log("success output");\n',
  );
  await writeFile(
    join(root, "fail.mjs"),
    'console.error("specific nested failure"); process.exit(1);\n',
  );
  const env = {
    NX_DEFAULT_OUTPUT_STYLE: "summary",
    FORCE_COLOR: "0",
  };
  const error = await runNx(
    [
      "run-many",
      "--projects=cli",
      "--targets=summary-success,summary-outer",
      "--nxBail=false",
    ],
    env,
  ).then(
    () => assert.fail("nested task should fail"),
    (failure) => failure,
  );
  assert.equal(error.code, 1);
  const output = error.stdout + error.stderr;
  assert.match(output, /1 succeeded/);
  assert.doesNotMatch(output, /success output/);
  assert.doesNotMatch(output, /specific nested failure/);
  const outerLog = output.match(/full log: ([^\r\n]+)/);
  assert.ok(outerLog, output);
  const nestedOutput = await readFile(outerLog[1].trim(), "utf8");
  const innerLog = nestedOutput.match(/full log: ([^\r\n]+)/);
  assert.ok(innerLog, nestedOutput);
  assert.match(
    await readFile(innerLog[1].trim(), "utf8"),
    /specific nested failure/,
  );
});

test("tooling input filesets include required files and exclude unrelated files", async () => {
  const config = await json("nx.json");
  const workspace = await json("project.json");
  const tooling = await json("tools/golangci-lint/project.json");
  // These examples check selected fileset boundaries, not dependency discovery
  // or complete task hashes and cache hits.
  const cases = [
    ["test:workspace-quality", "vitest.config.ts", true],
    ["test:workspace-quality", "sdk/common/project.json", true],
    ["test:workspace-quality", "e2e/cli/playwright.config.ts", true],
    ["test:workspace-quality", "client/web/project.json", false],
    ["test:go-quality", "deploy/aio/project.json", true],
    ["test:go-quality", "tools/golangci-lint/project.json", true],
    ["test:go-quality", "client/cli/project.json", false],
    ["test:go-quality", "tools/golangci-lint/README.md", false],
    ["setup:golangci-lint", "tools/golangci-lint/main.go", true],
    ["setup:golangci-lint", "tools/golangci-lint/go.sum", true],
    ["setup:golangci-lint", "tools/golangci-lint/cmd/new/analyzer.go", true],
    ["setup:golangci-lint", "tools/golangci-lint/cmd/new/embedded.txt", true],
    ["setup:golangci-lint", "tools/golangci-lint/README.md", false],
    ["setup:golangci-lint", "tools/golangci-lint/project.json", false],
    [
      "setup:golangci-lint",
      "tools/golangci-lint/cmd/format-go/main_test.go",
      false,
    ],
    [
      "test:golangci-lint-module",
      "tools/golangci-lint/cmd/format-go/main_test.go",
      true,
    ],
    [
      "test:golangci-lint-module",
      "tools/golangci-lint/testdata/input.go",
      true,
    ],
    ["lint", "tools/golangci-lint/cmd/format-go/main_test.go", true],
    ["test:golangci-lint-config", "nx.json", false],
    ["test:golangci-lint-config", "project.json", false],
    ["test:golangci-lint-config", "tools/golangci-lint/README.md", false],
    ["test:golangci-lint-config", "tools/golangci-lint/project.json", false],
  ];
  for (const [target, path, expected] of cases) {
    const project = target === "lint" ? tooling : workspace;
    const { selfInputs } = getTargetInputs(config, { data: project }, target);
    const patterns = selfInputs.map((input) =>
      input.replaceAll("{workspaceRoot}", "."),
    );
    assert.equal(
      filterUsingGlobPatterns(project.root, [{ file: path }], patterns).length >
        0,
      expected,
      `${target}: fileset must ${expected ? "include" : "exclude"} ${path}`,
    );
  }
});

test("workspace verification formats Go, TypeScript, styles and protobuf before checks", async (t) => {
  const { root, task, tasks } = await fixture(t, { workspaceChecks: true });
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

  // Reuse the clean workspace rather than formatting a second fixture just to
  // establish the baseline for read-only formatting and cache checks.
  await t.test(
    "format checks catch configuration and documentation drift without rewriting files",
    async () => {
      const files = [
        ["client/cli/settings.json", '{"value":1}\n'],
        ["docs/example.md", "#  Example\n"],
        [".github/workflows/example.yaml", "name:   Example\n"],
      ];
      const rejectDrift = () =>
        assert.rejects(tasks("lint:format", ["cli", "ocm-fleet"]), (error) => {
          assert.equal(error.code, 1);
          for (const [path] of files)
            assert.ok(
              (error.stdout + error.stderr).includes(basename(path)),
              path,
            );
          return true;
        });
      for (const [path, source] of files) {
        await mkdir(dirname(join(root, path)), { recursive: true });
        await writeFile(join(root, path), source);
      }
      await rejectDrift();
      for (const [path, source] of files) {
        assert.equal(await readFile(join(root, path), "utf8"), source);
      }
      await task("fmt", "cli");
      await task("fmt:workspace");
      await tasks("lint:format", ["cli", "ocm-fleet"]);
      // A prior success must not hide subsequent project or workspace drift.
      for (const [path, source] of files)
        await writeFile(join(root, path), source);
      await task("refresh:inputs");
      await rejectDrift();
    },
  );
});

test("project lint is read-only and propagates findings", async (t) => {
  const { root, task } = await fixture(t);
  const path = join(root, "client/cli/src/example.ts");
  const source = await readFile(path, "utf8");
  await assert.rejects(task("lint", "cli"));
  assert.equal(await readFile(path, "utf8"), source);
});

test("fmt leaves broader fixes for fix and ignores generated files", async (t) => {
  const { root, task, tasks } = await fixture(t);
  const path = join(root, "client/cli/src/example.ts");
  await writeFile(path, "export let value='hello'\n");
  const styles = join(root, "client/cli/src/example.css");
  await writeFile(styles, "a{color:#ffffff}\n");
  const generated = join(root, "client/cli/dist/example.js");
  await mkdir(dirname(generated));
  await writeFile(generated, "export let value='generated'\n");
  await task("fmt", "cli");
  assert.match(await readFile(path, "utf8"), /export let value = "hello";/);
  assert.match(await readFile(styles, "utf8"), /#ffffff/);
  await assert.rejects(tasks("lint,lint:css", ["cli"]));
  await task("fix", "cli");
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
  await assert.rejects(task("fmt", "cli"), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /Parsing error|SyntaxError/);
    return true;
  });
  await writeFile(path, "export const value: any = 'hello'\n");
  // Fix stages deliberately allow remaining findings. Read-only checks must
  // still reject them; the shared fix orchestration is exercised separately.
  await task("fix:lint", "cli");
  await assert.rejects(task("lint", "cli"), (error) => {
    assert.equal(error.code, 1);
    assert.match(error.stdout + error.stderr, /no-explicit-any/);
    return true;
  });
  await writeFile(path, 'export const value = "hello";\n');
  await writeFile(
    join(root, "client/cli/src/example.css"),
    "a { color: #zzzzzz; }\n",
  );
  await task("fix:css", "cli");
  await assert.rejects(task("lint:css", "cli"), (error) => {
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
// The normal and component checks observe the formatted files on disk without
// replacing a formatter/linter implementation.
async function fixture(t, { workspaceChecks = false } = {}) {
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
assert.match(await readFile("client/cli/src/example.ts", "utf8"), /value = "hello";/);
${
  workspaceChecks
    ? `assert.match(await readFile("server/example.go", "utf8"), /func Example\\(\\) int \\{ return 1 \\}/);
assert.match(await readFile("proto/example/v1/greeting.proto", "utf8"), /  string name = 1;/);`
    : ""
}
await appendFile(process.argv[2], "passed");
`,
  );
  const runNx = (args, env = {}) =>
    run(process.execPath, [nx, ...args], {
      cwd: root,
      env: {
        ...process.env,
        NX_DAEMON: "false",
        NX_ISOLATE_PLUGINS: "false",
        NX_NO_CLOUD: "true",
        NX_DEFAULT_OUTPUT_STYLE: "static",
        // An uncached outer test run must still exercise this fixture's cache.
        NX_SKIP_NX_CACHE: "false",
        NX_DISABLE_NX_CACHE: "false",
        ...env,
      },
      maxBuffer: 8 * 1024 * 1024,
    });
  return {
    root,
    runNx,
    task: (target, project = "ocm-fleet") =>
      runNx(["run", `${project}:${target}`, "--outputStyle=static"]),
    tasks: (targets, projects) =>
      runNx([
        "run-many",
        `--targets=${targets}`,
        `--projects=${projects.join(",")}`,
        "--nxBail=false",
        "--outputStyle=static",
      ]),
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
