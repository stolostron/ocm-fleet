import type { CheckResult, PreflightOptions } from "../contracts.js";
import { type CheckDependencies, resolveDependencies } from "./dependencies.js";
import { checkKindNetwork, checkKindSocket } from "./kind.js";
import { checkFleetctlNode } from "./node.js";
import { checkHostPlatform } from "./platform.js";
import { checkAioPorts } from "./ports.js";
import { checkDockerValidation, checkSelectedRuntime } from "./runtime.js";
import { discoverRuntime } from "./runtime-discovery.js";

export type {
  CheckDependencies,
  CommandResult,
  UnixSocketProbeResult,
} from "./dependencies.js";
export { checkKindNetwork, checkKindSocket } from "./kind.js";
export { checkFleetctlNode } from "./node.js";
export { checkHostPlatform } from "./platform.js";
export { checkAioPorts } from "./ports.js";
export { checkDockerValidation, checkSelectedRuntime } from "./runtime.js";
export { discoverRuntime, discoverRuntimes } from "./runtime-discovery.js";

export async function runPreflightChecks(
  options: PreflightOptions,
  overrides: Partial<CheckDependencies> = {},
): Promise<CheckResult[]> {
  const dependencies = resolveDependencies(overrides);
  const platformResult = checkHostPlatform(dependencies.platform);
  if (platformResult.status === "error") return [platformResult];

  const discovery =
    options.discovery ?? (await discoverRuntime(options.runtime, dependencies));
  if (discovery.runtime !== options.runtime)
    throw new TypeError("Preflight discovery must match selected runtime");
  const checks: CheckResult[] = [
    platformResult,
    checkSelectedRuntime(discovery),
  ];
  const dockerValidation = checkDockerValidation(discovery);
  if (dockerValidation) checks.push(dockerValidation);

  const portChecks = await checkAioPorts(dependencies);
  checks.push(...portChecks);
  checks.push(await checkFleetctlNode(dependencies));

  if (options.mode === "kind") {
    checks.push(await checkKindSocket(discovery, dependencies));
    checks.push(await checkKindNetwork(discovery, dependencies));
  }

  return checks;
}
