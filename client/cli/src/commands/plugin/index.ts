import type { CommandEntry } from "../types";
import { runPluginCommand } from "./run";

export const pluginCommand: CommandEntry = {
  path: "plugin",
  description: "Run commands provided by remote CLI plugins",
};

export const pluginCommands: CommandEntry[] = [runPluginCommand];
