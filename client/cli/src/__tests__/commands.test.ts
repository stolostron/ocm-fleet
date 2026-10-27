/* eslint-disable playwright/no-standalone-expect */
import { load as loadYaml } from "js-yaml";
import { describe, expect, it } from "vitest";

import { executeCommand } from "../app";
import { parseArgs } from "../argv";
import { registerCommand } from "../command";
import { helpText } from "../commands/help";
import { findCommand } from "../commands/registry";
import type { CommandEntry, CommandSpec } from "../commands/types";

describe("command registry", () => {
  it("keeps resource aliases", () => {
    expect(findCommand(["res", "search"])?.path).toBe("resource query");
  });

  it("lists root commands", () => {
    expect(helpText()).toContain("deployment");
    expect(helpText()).toContain("resource");
    expect(helpText()).toContain("plugin");
  });

  it("registers the remote plugin command", () => {
    expect(findCommand(["plugin", "run"])?.path).toBe("plugin run");
  });

  it("executes differently typed specs after registry lookup", async () => {
    const namesCommand = {
      path: "example names",
      aliases: ["ls"],
      description: "Names",
      implemented: true,
      run: async () => ["one", "two"],
      present: (names) => names.join(", "),
    } satisfies CommandSpec<string[]>;
    const countCommand = {
      path: "example count",
      description: "Count",
      implemented: true,
      run: async ({ args }) => ({ count: args.positionals.length }),
      present: (data) => `Count: ${data.count}`,
    } satisfies CommandSpec<{ count: number }>;
    const commands: CommandEntry[] = [
      { path: "example", aliases: ["ex"], description: "Examples" },
      registerCommand(namesCommand),
      registerCommand(countCommand),
    ];

    for (const [tokens, text, json, yaml] of [
      [["ex", "ls"], "one, two", '[\n  "one",\n  "two"\n]', "- one\n- two"],
      [["example", "count"], "Count: 1", '{\n  "count": 1\n}', "count: 1"],
    ] as const) {
      const entry = findCommand([...tokens], commands);
      if (!entry?.implemented) throw new Error("Expected a runnable command");

      for (const [output, expected] of [
        ["component", text],
        ["json", json],
        ["yaml", yaml],
      ] as const) {
        const content = await entry.withSpec((spec) =>
          executeCommand(
            spec,
            { args: parseArgs([...tokens, "input"]) },
            output,
          ),
        );

        expect(content).toBe(expected);
        if (output === "yaml") {
          expect(loadYaml(content as string)).toEqual(JSON.parse(json));
        }
      }
    }
  });
});
