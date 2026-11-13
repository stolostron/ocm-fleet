import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { globSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, normalize } from "node:path";
import test from "node:test";

const { parse, parseDocument } = createRequire(
  import.meta.resolve("nx/package.json"),
)("yaml");
const workflow = (name) =>
  parse(readFileSync(`.github/workflows/${name}.yml`, "utf8"));
const actionFiles = globSync([
  ".github/workflows/*.{yml,yaml}",
  ".github/actions/**/action.{yml,yaml}",
  "action.{yml,yaml}",
]);
const localReferencePaths = new Set(
  actionFiles.map((file) =>
    file.startsWith(".github/workflows/") ? file : dirname(file),
  ),
);

test("external actions use full commit SHAs with readable release versions", () => {
  assert.ok(actionFiles.length > 0);
  for (const file of actionFiles) {
    assertPinnedActions(readFileSync(file, "utf8"), file);
  }
});

const stepWithUsesInput = `
  - uses: ./.github/actions/setup-e2e
    with:
      uses: arbitrary-input
    env:
      uses: arbitrary-environment-value
`;
for (const [kind, source] of [
  [
    "reusable workflow",
    "jobs:\n  call:\n    uses: ./.github/workflows/codeql.yml\n    with:\n      uses: arbitrary-input",
  ],
  [
    "workflow",
    `jobs:\n  check:\n    steps:\n${stepWithUsesInput.replaceAll(/^/gm, "    ")}`,
  ],
  [
    "parallel step",
    `jobs:\n  check:\n    steps:\n      - parallel:\n${stepWithUsesInput.replaceAll(/^/gm, "        ")}`,
  ],
  [
    "composite action",
    `runs:\n  using: composite\n  steps:\n${stepWithUsesInput}`,
  ],
]) {
  test(`${kind} inputs named uses are not action references`, () => {
    assert.doesNotThrow(() => assertPinnedActions(source));
  });
}

for (const [kind, source] of [
  [
    "reusable workflow",
    "jobs:\n  call:\n    uses: owner/repo/.github/workflows/reusable.yml@v1",
  ],
  [
    "workflow step",
    "jobs:\n  check:\n    steps:\n      - uses: owner/action@v1",
  ],
  [
    "parallel step",
    "jobs:\n  check:\n    steps:\n      - parallel:\n          - uses: owner/action@v1",
  ],
  [
    "composite step",
    "runs:\n  using: composite\n  steps:\n    - uses: owner/action@v1",
  ],
]) {
  test(`${kind} references require commit pins`, () => {
    assert.throws(
      () => assertPinnedActions(source),
      /must pin an upstream commit/,
    );
  });
}

for (const prefix of ["./", "$/"]) {
  test(`${prefix} references require a manifest covered by the action policy`, () => {
    assert.doesNotThrow(() =>
      assertActionReference(`${prefix}.github/actions/setup-e2e`),
    );
    assert.doesNotThrow(() =>
      assertActionReference(`${prefix}.github/workflows/codeql.yml`),
    );
    assert.throws(
      () => assertActionReference(`${prefix}tools/uncovered-action`),
      /manifest covered by the action policy/,
    );
  });
}

test("Dependabot maintains workflow and local composite action pins", () => {
  const config = parse(readFileSync(".github/dependabot.yml", "utf8"));
  assert.equal(config.version, 2);
  const updates = config.updates.filter(
    (update) => update["package-ecosystem"] === "github-actions",
  );
  const directories = new Set(
    updates.flatMap((update) => {
      assert.ok(
        ["daily", "weekly", "monthly"].includes(update.schedule.interval),
      );
      assert.ok((update["open-pull-requests-limit"] ?? 5) > 0);
      return update.directories ?? [update.directory];
    }),
  );
  assert.ok(directories.has("/"), "workflows need automated updates");
  for (const file of actionFiles.filter(
    (file) => !file.startsWith(".github/workflows/"),
  )) {
    const directory = dirname(file);
    assert.ok(
      directories.has(directory === "." ? "/" : `/${directory}`),
      `${file} needs automated updates`,
    );
  }
});

test("every PR runs the full UI and CLI suites against its AIO image", () => {
  const e2e = workflow("e2e");
  assert.deepEqual(e2e.on.pull_request, { branches: ["main"] });
  assert.deepEqual(e2e.on.push, { branches: ["main"] });
  assert.ok(Object.hasOwn(e2e.on, "merge_group"));
  assert.equal(e2e.jobs["aio-image"].if, undefined);
  const suites = e2e.jobs["e2e-playwright"];
  assert.equal(suites.if, undefined);
  assert.deepEqual(suites.needs, ["aio-image"]);
  assert.equal(suites.strategy["fail-fast"], false);
  assert.deepEqual(
    suites.strategy.matrix.include.map(({ project, arguments: args }) => [
      project,
      args,
    ]),
    [
      ["e2e-web", "-- --project=chromium"],
      ["e2e-cli", ""],
    ],
  );
  const run = suites.steps.find((step) => step.run?.includes("nx test:e2e"));
  assert.equal(run.env.OCM_FLEET_E2E_AIO_PREBUILT, "1");
  assert.equal(
    run.run,
    "npx nx test:e2e ${{ matrix.project }} ${{ matrix.arguments }}",
  );
});

test("the E2E merge gate fails for failed, cancelled, or skipped dependencies", () => {
  const gate = Object.values(workflow("e2e").jobs).find(
    (job) => job.name === "E2E",
  );
  assert.ok(gate, "branch protection needs a stable E2E check");
  assert.equal(gate.if, "${{ always() }}");
  assert.deepEqual(gate.needs, ["aio-image", "e2e-playwright"]);
  const step = gate.steps.find((step) => step.run);
  assert.equal(step.shell, "bash", "the gate must work without a checkout");
  assert.deepEqual(step.env, {
    IMAGE_RESULT: "${{ needs.aio-image.result }}",
    TEST_RESULT: "${{ needs.e2e-playwright.result }}",
  });
  for (const image of ["success", "failure", "cancelled", "skipped"]) {
    for (const suite of ["success", "failure", "cancelled", "skipped"]) {
      const result = spawnSync("bash", ["-e", "-c", step.run], {
        encoding: "utf8",
        env: { ...process.env, IMAGE_RESULT: image, TEST_RESULT: suite },
      });
      assert.equal(
        result.status === 0,
        image === "success" && suite === "success",
        `${image}/${suite}: ${result.stderr}`,
      );
    }
  }
});

test("published-image sanity runs nightly or on demand, outside PR validation", () => {
  const published = workflow("e2e-published");
  assert.deepEqual(Object.keys(published.on).sort(), [
    "schedule",
    "workflow_dispatch",
  ]);
  assert.deepEqual(published.on.schedule, [{ cron: "0 3 * * *" }]);
  const run = published.jobs["e2e-published"].steps.find((step) =>
    step.run?.includes("nx test:e2e"),
  );
  assert.equal(run.env.OCM_FLEET_E2E_AIO_PULL, "1");
  assert.match(run.run, /--project=chromium-sanity$/);
});

test("native bootstrap cold-builds and smoke-tests the Linux executable", () => {
  const bootstrap = workflow("native-bootstrap");
  assert.deepEqual(Object.keys(bootstrap.on).sort(), [
    "pull_request",
    "push",
    "workflow_dispatch",
  ]);
  for (const trigger of ["pull_request", "push"]) {
    assert.deepEqual(bootstrap.on[trigger].branches, ["main"]);
    assert.deepEqual(bootstrap.on[trigger].paths, [
      ".github/workflows/native-bootstrap.yml",
      "client/aio-installation-check/**",
      "package.json",
      "package-lock.json",
      "tsconfig.json",
      "nx.json",
    ]);
  }
  assert.deepEqual(bootstrap.permissions, { contents: "read" });
  const job = bootstrap.jobs["native-bootstrap"];
  assert.equal(job["runs-on"], "ubuntu-latest");
  assert.equal(job["timeout-minutes"], 60);
  // The version-agnostic Nx entry point: whichever bootstrap implementation
  // the tree carries, CI drives it through the same workspace target.
  const build = job.steps.find((step) => step.run?.includes("build-native"));
  assert.equal(build.run, "npx nx run aio-installation-check:build-native");
  const smoke = job.steps.find((step) => step.run?.includes("smoke-native"));
  assert.equal(smoke.run, "npx nx run aio-installation-check:smoke-native");
  // Linux CI smoke only; the PTY acceptance suite is not part of this job.
  assert.ok(
    !job.steps.some((step) => step.run?.includes("native-verify")),
    "native-bootstrap must not run the PTY suite",
  );
  const prerequisites = job.steps.find((step) =>
    step.run?.includes("apt-get install"),
  );
  // Only the base C/clang SDK is preinstalled: each build path brings its
  // own LLVM and Rust pieces, so neither toolchain is set up here.
  assert.match(prerequisites.run, /build-essential/);
  assert.match(prerequisites.run, /clang/);
  assert.doesNotMatch(prerequisites.run, /rust|llvm/i);
});

function assertPinnedActions(source, file = "fixture") {
  const document = parseDocument(source);
  assert.deepEqual(document.errors, [], file);
  for (const { value: job } of document.get("jobs", true)?.items ?? []) {
    assertReference(job.get("uses", true));
    assertSteps(job.get("steps", true));
  }
  assertSteps(document.getIn(["runs", "steps"], true));

  function assertSteps(steps) {
    for (const step of steps?.items ?? []) {
      assertReference(step.get("uses", true));
      assertSteps(step.get("parallel", true));
    }
  }

  function assertReference(node) {
    if (node) assertActionReference(node.value, node.comment, file);
  }
}

function assertActionReference(reference, comment, file = "fixture") {
  if (reference.startsWith("./") || reference.startsWith("$/")) {
    assert.ok(
      localReferencePaths.has(normalize(reference.slice(2) || ".")),
      `${file}: ${reference} needs a manifest covered by the action policy; keep local actions in .github/actions/ or the repository root`,
    );
    return;
  }
  assert.match(
    reference,
    /^[\w.-]+\/[\w.-]+(?:\/[\w./-]+)?@[a-f\d]{40}$/,
    `${file}: ${reference} must pin an upstream commit`,
  );
  assert.match(
    comment ?? "",
    /^\s*v\d+\.\d+\.\d+(?:[-+][\w.-]+)?\s*$/,
    `${file}: ${reference} needs a release version comment`,
  );
}
