import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { globSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, normalize } from "node:path";
import test from "node:test";

const { parse, parseDocument, visit } = createRequire(
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
    const document = parseDocument(readFileSync(file, "utf8"));
    assert.deepEqual(document.errors, [], file);
    visit(document, {
      Pair(_, { key, value }) {
        if (key.value !== "uses") return;
        assertActionReference(value.value, value.comment, file);
      },
    });
  }
});

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
