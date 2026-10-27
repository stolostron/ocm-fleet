import React from "react";

import { flagString, hasFlag } from "../../argv";
import { registerCommand } from "../../command";
import {
  initializePluginRuntime,
  loadPluginCommand,
} from "../../plugins/runtime";
import type { CommandSpec } from "../types";

const run: CommandSpec<React.ReactElement> = {
  path: "plugin run",
  description: "Run a remote plugin's command component",
  implemented: true,
  present: (element) => element,
  run: async ({ args }) => {
    const pluginName = args.positionals[0];
    if (!pluginName) {
      throw new Error("Usage: fleetctl plugin run <plugin-name>");
    }

    const baseURL = process.env.FLEETCTL_PLUGIN_BASE_URL;
    if (!baseURL) {
      throw new Error(
        "FLEETCTL_PLUGIN_BASE_URL must be set to run a remote CLI plugin.",
      );
    }

    const moduleName = flagString(args, "module", "command");
    const runtime = initializePluginRuntime(baseURL, pluginName);
    try {
      const Command = await loadPluginCommand(
        runtime,
        pluginName,
        moduleName,
        hasFlag(args, "retry"),
      );
      return React.createElement(Command as React.ComponentType);
    } finally {
      runtime.dispose();
    }
  },
};

export const runPluginCommand = registerCommand(run);
