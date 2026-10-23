import { type ComponentType, createElement } from "react";

import { parseArgs } from "../argv";
import { registerCommand } from "../command";
import { listCommand } from "../commands/deployment/list";
import type { DeploymentListItem } from "../commands/deployment/types";
import type { CommandEntry, CommandSpec } from "../commands/types";

declare const View: ComponentType<{ data: { name: string } }>;

registerCommand({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  present: (data) => createElement(View, { data }),
});

registerCommand<{ name: string }>({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  // @ts-expect-error The presenter must accept the runner's data.
  present: (data: { count: number }) => String(data.count),
});

registerCommand<{ name: string }>({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  // @ts-expect-error A presenter must return a React node.
  present: (data) => ({ name: data.name }),
});

registerCommand<{ name: string }>({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  present: (data) => [data.name, "ready"],
});

registerCommand<{ name: string }>({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  // @ts-expect-error The component must accept the data passed by its presenter.
  present: (data) => createElement(View, { data: { count: data.name.length } }),
});

registerCommand<{ name: string }>({
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  // @ts-expect-error Machine-readable formatters are owned by the renderer.
  formatters: { json: (data) => JSON.stringify(data) },
});

const spec = {
  path: "example",
  description: "Example",
  implemented: true,
  run: async () => ({ name: "ready" }),
  present: (data) => createElement(View, { data }),
} satisfies CommandSpec<{ name: string }>;

// Leaf commands expose their typed specs before the group index registers them.
const deploymentSpec: CommandSpec<DeploymentListItem[]> = listCommand;
void deploymentSpec;

const registered = registerCommand(spec);

// @ts-expect-error Registry consumers must access the typed spec through withSpec.
void registered.run;

registered.withSpec(async (spec) => {
  const data = await spec.run({ args: parseArgs([]) });
  // @ts-expect-error A registry consumer cannot assume a particular data type.
  void data.name;
});

const invalidSpec: CommandSpec<{ name: string }> = {
  path: "example",
  description: "Example",
  implemented: true,
  // @ts-expect-error The spec itself checks the data returned by run.
  run: async () => ({ count: 1 }),
  present: (data) => createElement(View, { data }),
};
void invalidSpec;

// @ts-expect-error An implemented registry entry must provide access to a typed spec.
const incompleteEntry: CommandEntry = {
  path: "example",
  description: "Example",
  implemented: true,
};
void incompleteEntry;
