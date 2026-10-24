import type {
  DataPathValidation,
  LaunchOptions,
  LaunchPlan,
  LaunchResult,
} from "../contracts";
import { createLaunchService } from "./launch-service";

const launchService = createLaunchService();

export const KIND_PRIVILEGE_EXPLANATION =
  "Kind mode uses the documented local/development --privileged setting, which gives the container broad host access. Use only with trusted local workloads.";

export function buildLaunchPlan(options: LaunchOptions): LaunchPlan {
  return launchService.buildLaunchPlan(options);
}

export function validateDataPath(path: string): Promise<DataPathValidation> {
  return launchService.validateDataPath(path);
}

export function createDataDirectory(path: string): Promise<void> {
  return launchService.createDataDirectory(path);
}

export function executeLaunch(plan: LaunchPlan): Promise<LaunchResult> {
  return launchService.executeLaunch(plan);
}
