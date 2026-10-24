export type RuntimeKind = "podman" | "docker";

export type AioMode = "basic" | "kind";

export type CheckStatus = "pass" | "warning" | "error";

export interface FixAction {
  id: string;
  title: string;
  description: string;
  command?: string;
  requiresPrivilege: boolean;
  manual: boolean;
}

export interface CheckRemediation {
  explanation: string;
  documentationUrl?: string;
  actions?: FixAction[];
}

export interface CheckResult {
  id: string;
  title: string;
  status: CheckStatus;
  reason: string;
  blocking: boolean;
  remediation?: CheckRemediation;
}

export interface RuntimeDiscovery {
  runtime: RuntimeKind;
  detected: boolean;
  ready: boolean;
  local: boolean;
  preferred: boolean;
  clientVersion?: string;
  serverVersion?: string;
  /** Host-reachable Unix endpoint used only for the HTTP API probe. */
  hostSocketPath?: string;
  /** Absolute path in the engine's filesystem, used as the Kind bind source. */
  engineSocketPath?: string;
  /** Managed local Podman machine whose filesystem owns engineSocketPath. */
  podmanMachineName?: string;
  summary: string;
}

export interface PreflightOptions {
  runtime: RuntimeKind;
  mode: AioMode;
  /** Fresh discovery snapshot shared by checks and launch generation. */
  discovery?: RuntimeDiscovery;
}

export interface LaunchPlan {
  executable: string;
  args: string[];
  environment: Record<string, string>;
  displayCommand: string;
}

export interface LaunchOptions {
  runtime: RuntimeKind;
  mode: AioMode;
  dataPath?: string;
  engineSocketPath?: string;
}

export type DataPathStatus =
  "usable" | "missing" | "not-directory" | "permission-denied";

export interface DataPathValidation {
  status: DataPathStatus;
  path: string;
  reason: string;
}

export interface LaunchResult {
  started: boolean;
  error?: string;
}
