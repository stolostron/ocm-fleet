import { discoverRuntimes, runPreflightChecks } from "../checks";
import type { CheckDependencies } from "../checks/dependencies";
import {
  buildLaunchPlan,
  createDataDirectory,
  executeLaunch,
  validateDataPath,
} from "../launch";
import { TerminalCancelled } from "../ui/keyboard-selection";
import { createTerminal } from "../ui/terminal";
import { runInstallationCheck } from "./orchestrator";

export async function runStandalone(
  args: string[],
  checkDependencies: Partial<CheckDependencies> = {},
): Promise<void> {
  try {
    await runInstallationCheck(
      args,
      {
        discoverRuntimes: () => discoverRuntimes(checkDependencies),
        runPreflightChecks: (options) =>
          runPreflightChecks(options, checkDependencies),
        buildLaunchPlan: (options) => buildLaunchPlan(options),
        validateDataPath: (path) => validateDataPath(path),
        createDataDirectory: (path) => createDataDirectory(path),
        executeLaunch: (plan) => executeLaunch(plan),
      },
      createTerminal(),
    );
  } catch (error) {
    if (!(error instanceof TerminalCancelled)) throw error;
    process.stdout.write("Cancelled.\n");
    process.exitCode = 130;
  }
}
