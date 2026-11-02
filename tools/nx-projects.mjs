import { createProjectGraphAsync, workspaceRoot } from "@nx/devkit";
import { readFile } from "node:fs/promises";
import { join, posix, resolve } from "node:path";
import { pathToFileURL } from "node:url";
// Nx exposes no public task-graph builder. Keep these pinned-version adapters
// here; policy tests exercise the real builder without running any tasks.
import { createTaskGraph } from "nx/src/tasks-runner/create-task-graph";
import { globWithWorkspaceContext } from "nx/src/utils/workspace-context";

const profiles = ["type:go-module", "type:js-project", "lang:proto"];
const lifecycle = [
  "fmt",
  "verify",
  "fix",
  "fix:lint",
  "refresh:inputs",
  "check",
  "lint",
];
const checks = [
  "lint",
  "lint:format",
  "lint:css",
  "test",
  "test:ct",
  "typecheck",
];
const rewrites = [
  "fmt",
  "fix",
  "fix:lint",
  "fix:css",
  "lint:fix",
  "refresh:inputs",
];
const ignoredDirectories = [
  "node_modules",
  "vendor",
  "testdata",
  "generated",
  "dist",
  "bin",
  ".nx",
  ".git",
  "test-results",
  "playwright-report",
];

/** Validate resolved Nx configuration and source inventory without executing tasks. */
export function validateProjects(graph, inventory) {
  const errors = [];
  const projects = Object.values(graph.nodes).map(({ data }) => data);
  const byRoot = new Map(projects.map((project) => [project.root, project]));
  const files = inventory.files.filter(inScope);
  const participating = [];
  const requireTarget = (project, name) => {
    const target = project.targets?.[name];
    if (!target)
      errors.push(
        `${project.name}:${name}: missing target; see docs/nx-projects.md.`,
      );
    else if (!target.executor)
      errors.push(
        `${project.name}:${name}: no resolved executor; check its profile tag and shared defaults.`,
      );
  };

  for (const [root, kind, tag] of [
    ...files
      .filter((file) => file.endsWith("/go.mod"))
      .map((file) => [posix.dirname(file), "Go module", "type:go-module"]),
    ...inventory.npmProjects
      .filter(inScope)
      .map((root) => [root, "npm workspace", "type:js-project"]),
  ]) {
    const project = byRoot.get(root);
    if (!project)
      errors.push(`${root}: ${kind} needs an Nx project at its root.`);
    else if (
      !project.metadata?.quality?.exemption &&
      !project.tags?.includes(tag)
    )
      errors.push(`${project.name}: ${kind} requires the ${tag} tag.`);
  }

  // Assign each source to its closest project so nested modules and packages
  // do not accidentally contribute capabilities to their parent project.
  const roots = [...byRoot.keys()]
    .filter((root) => root !== ".")
    .sort((a, b) => b.length - a.length);
  const ownedFiles = new Map(projects.map((project) => [project.root, []]));
  for (const file of files) {
    const root = roots.find((root) => file.startsWith(`${root}/`));
    if (root) ownedFiles.get(root).push(file);
    else if (file.endsWith(".proto"))
      errors.push(
        `${file}: protobuf source needs an Nx project tagged lang:proto.`,
      );
  }

  for (const project of projects) {
    if (project.root === "." || !inScope(project.root)) continue;
    const exemption = project.metadata?.quality?.exemption;
    if (exemption !== undefined) {
      if (typeof exemption !== "string" || !exemption.trim())
        errors.push(
          `${project.name}: quality exemption requires a nonempty reason.`,
        );
      continue;
    }
    const tags = project.tags ?? [];
    if (profiles.filter((tag) => tags.includes(tag)).length !== 1) {
      errors.push(
        `${project.name}: select exactly one quality tag (${profiles.join(", ")}) or document metadata.quality.exemption.`,
      );
      continue;
    }
    participating.push(project);
    for (const target of lifecycle) requireTarget(project, target);
    const sources = ownedFiles.get(project.root);
    const hasTypeScript = sources.some((file) =>
      /\.(?:[cm]?ts|tsx)$/.test(file),
    );
    if (hasTypeScript && !tags.includes("lang:typescript"))
      errors.push(
        `${project.name}: TypeScript sources require the lang:typescript tag.`,
      );
    const required = new Set();
    if (hasTypeScript || tags.includes("lang:typescript"))
      required.add("typecheck");
    if (tags.includes("type:go-module") || tags.includes("test:vitest"))
      required.add("test");
    if (tags.includes("type:js-project")) required.add("lint:format");
    if (
      sources.some(
        (file) =>
          /\.(test|spec)\.[cm]?[jt]sx?$/.test(file) &&
          (!tags.includes("type:e2e") || file.includes(".test.")),
      )
    )
      required.add("test");
    if (sources.some((file) => file.endsWith(".ct.tsx")))
      required.add("test:ct");
    if (sources.some((file) => /\.(css|scss)$/.test(file))) {
      required.add("lint:css");
      required.add("fix:css");
    }
    for (const target of required) requireTarget(project, target);
    for (const name of [...rewrites, "verify"]) {
      if (project.targets?.[name]?.cache === true)
        errors.push(
          `${project.name}:${name}: rewriting/verification targets must not be cached.`,
        );
    }
    for (const [name, input] of [
      [
        "lint",
        tags.includes("type:go-module")
          ? "goLintConfig"
          : tags.includes("type:js-project")
            ? "eslintConfig"
            : undefined,
      ],
      ["lint:format", "formatConfig"],
      ["test", tags.includes("test:vitest") ? "jsRuntime" : undefined],
      [
        "typecheck",
        tags.includes("lang:typescript") ? "typescriptConfig" : undefined,
      ],
    ]) {
      const target = project.targets?.[name];
      if (input && target?.cache && !target.inputs?.includes(input))
        errors.push(
          `${project.name}:${name}: cached standard check must retain the ${input} input.`,
        );
    }
  }

  // Building graphs in memory uses Nx's real dependency matching, including
  // implicit/inferred targets. It does not invoke a nested Nx CLI per project.
  try {
    const taskGraph = createTaskGraph(
      graph,
      {},
      ["ocm-fleet"],
      ["check"],
      undefined,
      {},
    );
    const workspaceTasks = reachable(taskGraph, "ocm-fleet:check");
    for (const project of participating) {
      const id = `${project.name}:check`;
      if (!workspaceTasks.has(id))
        errors.push(`${id}: not reached by workspace check.`);
      const projectTasks = reachable(taskGraph, id);
      for (const name of checks) {
        if (
          project.targets?.[name] &&
          !projectTasks.has(`${project.name}:${name}`)
        )
          errors.push(`${project.name}:${name}: not reached by ${id}.`);
      }
    }
    for (const id of workspaceTasks) {
      const name = taskGraph.tasks[id]?.target.target;
      if (rewrites.includes(name))
        errors.push(
          `${id}: source rewrites cannot participate in read-only check.`,
        );
      if (["test-integration", "test:integration", "test:e2e"].includes(name))
        errors.push(`${id}: integration/end-to-end suites must stay explicit.`);
    }
    const names = participating
      .filter((project) =>
        ["verify", "fix"].every((name) => project.targets?.[name]?.executor),
      )
      .map(({ name }) => name);
    const localGraph = createTaskGraph(
      graph,
      {},
      names,
      ["verify", "fix"],
      undefined,
      {},
    );
    for (const project of participating) {
      for (const name of ["verify", "fix"]) {
        if (
          !reachable(localGraph, `${project.name}:${name}`).has(
            `${project.name}:fmt`,
          )
        )
          errors.push(
            `${project.name}:${name}: must depend on ${project.name}:fmt.`,
          );
      }
    }
  } catch (error) {
    errors.push(`Nx quality task graph: ${error.message}`);
  }
  return errors;
}

/** Reuse Nx's file index, respecting ignored artifacts and deliberate fixtures. */
export async function readInventory(root = workspaceRoot) {
  const manifest = JSON.parse(
    await readFile(join(root, "package.json"), "utf8"),
  );
  const workspaces = Array.isArray(manifest.workspaces)
    ? manifest.workspaces
    : (manifest.workspaces?.packages ?? []);
  const exclude = [
    "poc/**",
    ...ignoredDirectories.map((directory) => `**/${directory}/**`),
    "**/playwright/.cache/**",
  ];
  const [files, manifests] = await Promise.all([
    globWithWorkspaceContext(
      root,
      ["**/go.mod", "**/*.{ts,tsx,mts,cts,js,jsx,mjs,cjs,go,proto,css,scss}"],
      exclude,
    ),
    workspaces.length
      ? globWithWorkspaceContext(
          root,
          workspaces.map((path) => `${path}/package.json`),
          exclude,
        )
      : [],
  ]);
  return { files, npmProjects: manifests.map((path) => posix.dirname(path)) };
}

function inScope(path) {
  return (
    path !== "poc" &&
    !path.startsWith("poc/") &&
    !path.split("/").some((part) => ignoredDirectories.includes(part)) &&
    !path.includes("/playwright/.cache/")
  );
}

function reachable(graph, start) {
  const visited = new Set();
  const pending = graph.tasks[start] ? [start] : [];
  while (pending.length) {
    const id = pending.pop();
    if (visited.has(id)) continue;
    visited.add(id);
    pending.push(...(graph.dependencies[id] ?? []));
  }
  return visited;
}

async function main() {
  const [graph, inventory] = await Promise.all([
    createProjectGraphAsync(),
    readInventory(),
  ]);
  const errors = validateProjects(graph, inventory);
  if (errors.length) {
    console.error(errors.map((error) => `- ${error}`).join("\n"));
    process.exitCode = 1;
  } else {
    console.log("Nx project quality configuration is complete.");
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
)
  await main();
