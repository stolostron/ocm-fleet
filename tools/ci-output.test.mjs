import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

function runStep(t, source, prepare = () => {}) {
  const root = mkdtempSync(join(tmpdir(), "ci-output-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const script = join(root, "step.sh");
  const summary = join(root, "summary.md");
  const output = join(root, "output");
  prepare(root);
  writeFileSync(script, source);
  const result = spawnSync("bash", [resolve("tools/ci-output.sh"), script], {
    encoding: "utf8",
    maxBuffer: 8 * 1024 * 1024,
    env: {
      ...process.env,
      RUNNER_TEMP: root,
      GITHUB_STEP_SUMMARY: summary,
      GITHUB_OUTPUT: output,
    },
  });
  return { ...result, root, summary, output };
}

test("passing steps are silent and preserve workflow environment files", (t) => {
  const result = runStep(
    t,
    'echo normal; echo warning >&2; echo key=value >> "$GITHUB_OUTPUT"',
  );
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout + result.stderr, "");
  assert.equal(readFileSync(result.output, "utf8"), "key=value\n");
  assert.deepEqual(readdirSync(join(result.root, "ci-logs")), []);
});

test("failures preserve exit status and complete stdout/stderr with an annotation", (t) => {
  const result = runStep(t, "echo context; echo diagnosis >&2; exit 23");
  assert.equal(result.status, 23);
  assert.match(result.stdout, /::error::.*23/);
  assert.match(result.stdout, /context\ndiagnosis/);
  const logs = readdirSync(join(result.root, "ci-logs"));
  assert.equal(logs.length, 1);
  assert.equal(
    readFileSync(join(result.root, "ci-logs", logs[0]), "utf8"),
    "context\ndiagnosis\n",
  );
  assert.match(readFileSync(result.summary, "utf8"), /23.*ci-logs/);
});

test("shell failures stop immediately, including failed pipelines", (t) => {
  const result = runStep(t, "false | cat\necho should-not-run");
  assert.equal(result.status, 1);
  assert.doesNotMatch(result.stdout, /should-not-run/);
});

test("large failure output is complete and workflow commands are treated as log text", (t) => {
  const result = runStep(
    t,
    'printf "::error::untrusted output\\n"; head -c 2000000 /dev/zero | tr "\\0" x; echo END; exit 1',
  );
  assert.equal(result.status, 1);
  assert.match(result.stdout, /::stop-commands::/);
  assert.match(result.stdout, /x{2000000}END/);
});

// Read with Nx's YAML dependency so workflow configuration follows the same
// parser/toolchain already installed for workspace tasks.
test("workflows capture command output and retain failure logs in every checked-out job", () => {
  const { parse } = createRequire(import.meta.resolve("nx/package.json"))(
    "yaml",
  );
  for (const name of readdirSync(".github/workflows")) {
    const workflow = parse(
      readFileSync(join(".github/workflows", name), "utf8"),
    );
    assert.equal(
      workflow.defaults.run.shell,
      "bash tools/ci-output.sh {0}",
      name,
    );
    for (const job of Object.values(workflow.jobs)) {
      if (!job.steps.some((step) => step.uses?.startsWith("actions/checkout@")))
        continue;
      const upload = job.steps.find(
        (step) => step.name === "Upload failure logs",
      );
      assert.ok(upload, name);
      assert.equal(upload.if, "${{ failure() || cancelled() }}");
      assert.equal(upload.with.path, "${{ runner.temp }}/ci-logs/");
      assert.equal(upload.with["if-no-files-found"], "ignore");
      if (job.strategy?.matrix)
        assert.match(upload.with.name, /strategy.job-index/);
    }
  }
  const composite = parse(
    readFileSync(".github/actions/setup-e2e/action.yml", "utf8"),
  );
  for (const step of composite.runs.steps.filter((step) => step.run)) {
    assert.equal(step.shell, "bash tools/ci-output.sh {0}");
  }
});

test("nested Nx failures include diagnostics without successful dependency output", (t) => {
  const result = runStep(
    t,
    'cd "$RUNNER_TEMP"\nnode node_modules/nx/dist/bin/nx.js run-many --targets=outer --projects=fixture',
    (root) => {
      symlinkSync(resolve("node_modules"), join(root, "node_modules"), "dir");
      writeFileSync(join(root, "nx.json"), '{"plugins": []}');
      writeFileSync(
        join(root, "package.json"),
        '{"name": "ci-output-fixture", "private": true}',
      );
      writeFileSync(
        join(root, "project.json"),
        JSON.stringify({
          name: "fixture",
          targets: {
            pass: {
              executor: "nx:run-commands",
              options: { command: "echo noisy-passing-dependency" },
            },
            inner: {
              executor: "nx:run-commands",
              options: {
                command: "echo specific-nested-diagnosis >&2; exit 7",
              },
            },
            outer: {
              executor: "nx:run-commands",
              dependsOn: ["pass"],
              options: {
                command:
                  "node node_modules/nx/dist/bin/nx.js run fixture:inner",
              },
            },
          },
        }),
      );
    },
  );
  assert.equal(result.status, 1, result.stderr);
  assert.match(result.stdout, /specific-nested-diagnosis/);
  assert.doesNotMatch(result.stdout, /noisy-passing-dependency/);
  assert.doesNotMatch(result.stdout, /full log:/);
});
