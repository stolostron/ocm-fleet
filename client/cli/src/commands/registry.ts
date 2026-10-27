import { authCommand, authCommands } from "./auth";
import { deploymentCommand, deploymentCommands } from "./deployment";
import { pluginCommand, pluginCommands } from "./plugin";
import { resourceCommand, resourceCommands } from "./resource";
import type { CommandEntry } from "./types";

export const commandEntries: CommandEntry[] = [
  authCommand,
  ...authCommands,
  deploymentCommand,
  ...deploymentCommands,
  pluginCommand,
  ...pluginCommands,
  resourceCommand,
  ...resourceCommands,
];

export function findCommand(
  tokens: string[],
  commands: readonly CommandEntry[] = commandEntries,
): CommandEntry | undefined {
  const input = tokens.join(" ");
  const direct = commands.find((command) => command.path === input);
  if (direct) return direct;
  const group = tokens[0];
  const canonicalGroup = commands.find(
    (command) =>
      !command.path.includes(" ") &&
      (command.path === group || command.aliases?.includes(group)),
  )?.path;
  if (!canonicalGroup) return undefined;
  if (tokens.length === 1) {
    return commands.find((command) => command.path === canonicalGroup);
  }
  const canonicalLeaf = commands.find(
    (command) =>
      command.path.startsWith(`${canonicalGroup} `) &&
      command.aliases?.includes(tokens[1]),
  );
  return (
    canonicalLeaf ??
    commands.find(
      (command) =>
        command.path === `${canonicalGroup} ${tokens.slice(1).join(" ")}`,
    )
  );
}
