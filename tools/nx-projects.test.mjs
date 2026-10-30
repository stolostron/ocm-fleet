import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import { readInventory, validateProjects } from "./nx-projects.mjs";

test("accepts registered projects with inherited lifecycle targets", () => {
  const { graph, inventory } = fixture();
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("catches new Go modules and npm workspaces without Nx projects", () => {
  const { graph, inventory } = fixture();
  inventory.files.push("extra/go.mod");
  inventory.npmProjects.push("new-web");
  const errors = validateProjects(graph, inventory).join("\n");
  assert.match(errors, /extra.*Go module.*Nx project/);
  assert.match(errors, /new-web.*npm workspace.*Nx project/);
});

test("missing tags cannot silently exempt source projects", () => {
  const { graph, inventory } = fixture();
  graph.nodes.api.data.tags = ["lang:go"];
  graph.nodes.ui.data.tags = [];
  const errors = validateProjects(graph, inventory).join("\n");
  assert.match(errors, /api.*type:go-module/);
  assert.match(errors, /ui.*type:js-project/);
});

test("TypeScript sources require a language tag and a typecheck target", () => {
  for (const extension of ["ts", "tsx", "mts", "cts"]) {
    const { graph, inventory } = fixture();
    graph.nodes.ui.data.tags = ["type:js-project"];
    delete graph.nodes.ui.data.targets.typecheck;
    inventory.files = ["api/go.mod", `ui/src/index.${extension}`];
    const errors = validateProjects(graph, inventory).join("\n");
    assert.match(errors, /ui.*lang:typescript/, extension);
    assert.match(errors, /ui:typecheck.*missing/, extension);
  }
});

test("TypeScript tags require executable type checks even without discovered source", () => {
  const { graph, inventory } = fixture();
  inventory.files = ["api/go.mod"];
  graph.nodes.ui.data.targets.typecheck = {};
  assert.match(
    validateProjects(graph, inventory).join("\n"),
    /ui:typecheck.*executor/,
  );
});

test("JavaScript projects share tooling without requiring TypeScript checks", () => {
  const { graph, inventory } = fixture();
  graph.nodes.ui.data.tags = ["type:js-project"];
  delete graph.nodes.ui.data.targets.typecheck;
  inventory.files = ["api/go.mod", "ui/src/index.mjs"];
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("catches missing targets and unresolved empty target entries", () => {
  const { graph, inventory } = fixture();
  delete graph.nodes.api.data.targets.check;
  graph.nodes.ui.data.targets.fmt = {};
  const errors = validateProjects(graph, inventory).join("\n");
  assert.match(errors, /api:check.*missing/);
  assert.match(errors, /ui:fmt.*executor/);
});

test("requires project participation and declared checks in the workspace graph", () => {
  for (const change of [
    ({ graph }) => {
      graph.nodes["ocm-fleet"].data.targets.check.dependsOn = ["ui:check"];
    },
    ({ graph }) => {
      graph.nodes.api.data.targets.check.dependsOn = ["lint"];
    },
  ]) {
    const state = fixture();
    change(state);
    assert.match(
      validateProjects(state.graph, state.inventory).join("\n"),
      /api:check.*workspace|api:test.*api:check/,
    );
  }
});

test("source capabilities require their checks even when no target was declared", () => {
  const { graph, inventory } = fixture();
  inventory.files.push(
    "ui/src/example.test.ts",
    "ui/src/example.ct.tsx",
    "ui/src/example.scss",
  );
  delete graph.nodes.ui.data.targets.test;
  const errors = validateProjects(graph, inventory).join("\n");
  for (const target of ["test", "test:ct", "lint:css", "fix:css"]) {
    assert.ok(errors.includes(`ui:${target}`), errors);
  }
});

test("end-to-end specs do not require a normal test target", () => {
  const { graph, inventory } = fixture();
  graph.nodes.ui.data.tags.push("type:e2e");
  delete graph.nodes.ui.data.targets.test;
  inventory.files.push("ui/tests/example.spec.ts");
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("requires formatting before verify/fix and disables caching for rewrites", () => {
  const { graph, inventory } = fixture();
  graph.nodes.api.data.targets.verify.dependsOn = [];
  graph.nodes.ui.data.targets.fix.dependsOn = [];
  graph.nodes.api.data.targets.fmt.cache = true;
  const errors = validateProjects(graph, inventory).join("\n");
  assert.match(errors, /api:verify.*api:fmt/);
  assert.match(errors, /ui:fix.*ui:fmt/);
  assert.match(errors, /api:fmt.*cach/);
});

test("read-only checks cannot reach formatting or integration suites", () => {
  for (const target of ["fmt", "test-integration", "test:e2e"]) {
    const { graph, inventory } = fixture();
    graph.nodes.api.data.targets[target] ??= task();
    graph.nodes.api.data.targets.check.dependsOn.push(target);
    assert.match(
      validateProjects(graph, inventory).join("\n"),
      new RegExp(`api:${target}.*read-only|api:${target}.*explicit`),
    );
  }
});

test("cached standard checks must retain their shared configuration inputs", () => {
  const { graph, inventory } = fixture();
  graph.nodes.api.data.targets.lint.inputs = ["default"];
  graph.nodes.ui.data.targets["lint:format"].inputs = ["default"];
  graph.nodes.ui.data.targets.typecheck.inputs = ["default"];
  const errors = validateProjects(graph, inventory).join("\n");
  assert.match(errors, /api:lint.*goLintConfig/);
  assert.match(errors, /ui:lint:format.*formatConfig/);
  assert.match(errors, /ui:typecheck.*typescriptConfig/);
});

test("exemptions need a reason and do not cover unregistered nested modules", () => {
  const { graph, inventory } = fixture();
  graph.nodes.placeholder = project("placeholder", {
    metadata: { quality: { exemption: "Backend implementation is deferred." } },
  });
  assert.deepEqual(validateProjects(graph, inventory), []);
  graph.nodes.placeholder.data.metadata.quality.exemption = " ";
  assert.match(
    validateProjects(graph, inventory).join("\n"),
    /placeholder.*exemption.*reason/,
  );
  graph.nodes.placeholder.data.metadata.quality.exemption = "Deferred.";
  inventory.files.push("placeholder/nested/go.mod");
  assert.match(
    validateProjects(graph, inventory).join("\n"),
    /placeholder\/nested.*Nx project/,
  );
});

test("fixtures and standalone experiments stay outside the inventory policy", () => {
  const { graph, inventory } = fixture();
  inventory.files.push("poc/example/go.mod", "api/testdata/module/go.mod");
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("embedded protobuf fixtures do not change a Go project's quality profile", () => {
  const { graph, inventory } = fixture();
  inventory.files.push("api/internal/schema.proto");
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("nested projects own their capabilities instead of contributing them to a parent", () => {
  const { graph, inventory } = fixture();
  graph.nodes.nested = project("nested", {
    root: "api/nested",
    metadata: { quality: { exemption: "Unimplemented UI experiment." } },
  });
  inventory.files.push("api/nested/example.scss", "api/nested/example.ts");
  assert.deepEqual(validateProjects(graph, inventory), []);
});

test("reads new module/package inventory and capabilities while excluding artifacts", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "ocm-fleet-nx-inventory-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  for (const [path, content] of [
    ["package.json", JSON.stringify({ workspaces: ["packages/*"] })],
    ["packages/new/package.json", "{}"],
    ["packages/new/src/example.scss", "a {}"],
    ["extra/go.mod", "module example.com/extra\n"],
    ["extra/testdata/fixture/go.mod", "module fixture\n"],
    ["poc/example/go.mod", "module experiment\n"],
    ["extra/dist/generated.ts", "export {};"],
  ]) {
    await mkdir(dirname(join(root, path)), { recursive: true });
    await writeFile(join(root, path), content);
  }
  const inventory = await readInventory(root);
  assert.deepEqual(inventory.npmProjects, ["packages/new"]);
  assert.deepEqual(inventory.files.sort(), [
    "extra/go.mod",
    "packages/new/src/example.scss",
  ]);
});

// These are resolved project data, not replacements for tools. The validator
// constructs real Nx task graphs from them, including Nx's missing-target rules.
function fixture() {
  return {
    graph: {
      nodes: {
        "ocm-fleet": project("ocm-fleet", {
          root: ".",
          targets: {
            check: task({
              dependsOn: [{ projects: ["*", "!ocm-fleet"], target: "check" }],
            }),
          },
        }),
        api: project("api", {
          tags: ["type:go-module"],
          targets: lifecycle({
            lint: task({ cache: true, inputs: ["goLintConfig"] }),
          }),
        }),
        ui: project("ui", {
          tags: ["type:js-project", "lang:typescript"],
          targets: lifecycle({
            lint: task({ cache: true, inputs: ["eslintConfig"] }),
            "lint:format": task({ cache: true, inputs: ["formatConfig"] }),
            typecheck: task({ cache: true, inputs: ["typescriptConfig"] }),
          }),
        }),
      },
      dependencies: { "ocm-fleet": [], api: [], ui: [] },
      externalNodes: {},
    },
    inventory: {
      files: ["api/go.mod", "api/main.go", "ui/src/index.ts"],
      npmProjects: ["ui"],
    },
  };
}

function project(name, data) {
  return {
    name,
    type: "lib",
    data: { name, root: name, targets: {}, ...data },
  };
}

function lifecycle(checks) {
  return {
    fmt: task(),
    verify: task({ dependsOn: ["fmt"] }),
    fix: task({ dependsOn: ["fmt"] }),
    "fix:lint": task(),
    "refresh:inputs": task(),
    check: task({
      dependsOn: [
        "lint",
        "lint:format",
        "lint:css",
        "test",
        "test:ct",
        "typecheck",
      ],
    }),
    test: task(),
    ...checks,
  };
}

function task(options = {}) {
  return { executor: "nx:noop", cache: false, ...options };
}
