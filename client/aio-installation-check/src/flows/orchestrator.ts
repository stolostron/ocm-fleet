import type {
  AioMode,
  CheckResult,
  DataPathValidation,
  LaunchOptions,
  LaunchPlan,
  LaunchResult,
  PreflightOptions,
  RuntimeDiscovery,
  RuntimeKind,
} from "../contracts";
import type { Terminal } from "../ui/terminal";

export interface InstallationServices {
  discoverRuntimes(): Promise<RuntimeDiscovery[]>;
  runPreflightChecks(options: PreflightOptions): Promise<CheckResult[]>;
  buildLaunchPlan(options: LaunchOptions): LaunchPlan;
  validateDataPath(path: string): Promise<DataPathValidation>;
  createDataDirectory(path: string): Promise<void>;
  executeLaunch(plan: LaunchPlan): Promise<LaunchResult>;
}

const POLL_INTERVAL_MS = 2_000;
const POLL_ATTEMPTS = 100;
const AIO_IMAGE = "quay.io/stolostron/fleetshift:latest";
const NODE_DOWNLOADS_URL = "https://nodejs.org/en/download/";
const NVM_URL = "https://github.com/nvm-sh/nvm";

function isBlocking(result: CheckResult): boolean {
  return result.blocking && result.status !== "pass";
}

function isKindOnly(result: CheckResult): boolean {
  return /kind|socket|network|alias|\/tmp/i.test(
    `${result.id} ${result.title}`,
  );
}

function isNodeCheck(result: CheckResult): boolean {
  return /node/i.test(`${result.id} ${result.title}`);
}

function printResult(terminal: Terminal, result: CheckResult): void {
  if (terminal.result) {
    terminal.result(result);
    return;
  }
  const label = result.status.toUpperCase();
  const requirement =
    result.blocking && result.status !== "pass" ? " — required fix" : "";
  terminal.write(`[${label}] ${result.title}${requirement}: ${result.reason}`);
  if (result.remediation) {
    terminal.write(`  Fix guidance: ${result.remediation.explanation}`);
    terminal.write(
      `  Documentation: ${result.remediation.documentationUrl ?? "Documentation link unavailable"}`,
    );
    for (const action of result.remediation.actions ?? []) {
      terminal.write(
        `  Available action: ${action.title} — ${action.description}`,
      );
      if (action.command) terminal.write(`  Command: ${action.command}`);
    }
  }
}

function printResults(terminal: Terminal, results: CheckResult[]): void {
  terminal.write("\nPreflight results", "heading");
  for (const result of results) printResult(terminal, result);

  const required = results.filter(isBlocking);
  const optional = results.filter(
    (result) => result.status !== "pass" && !result.blocking,
  );
  terminal.write(
    `\nReadiness: ${required.length} blocking check(s), ${optional.length} advisory item(s).`,
    required.length > 0 ? "error" : "success",
  );
}

async function offerCopy(terminal: Terminal, command: string): Promise<void> {
  terminal.write(`Command remains available:\n${command}`);
  const choice = await terminal.choose(
    "Copy this exact command to clipboard?",
    ["Copy", "No"],
  );
  if (choice !== 0) return;
  terminal.write(
    (await terminal.copy(command))
      ? "Command copied to clipboard."
      : "Clipboard unavailable; command remains visible above.",
  );
}

async function copyExactCommand(
  terminal: Terminal,
  command: string,
): Promise<void> {
  terminal.write(`Command remains available:\n${command}`);
  terminal.write(
    (await terminal.copy(command))
      ? "Command copied to clipboard."
      : "Clipboard unavailable; command remains visible above.",
  );
}

async function showNodeGuidance(
  terminal: Terminal,
  results: CheckResult[],
): Promise<void> {
  const nodeWarnings = results.filter(
    (result) => isNodeCheck(result) && result.status !== "pass",
  );
  if (nodeWarnings.length === 0) return;

  terminal.write(
    "\nNode.js warning applies to fleetctl only; AIO preflight and launch can continue.",
  );
  terminal.write(`Install or upgrade Node.js: ${NODE_DOWNLOADS_URL}`);
  terminal.write(`Manage Node.js versions with nvm: ${NVM_URL}`);
  const choice = await terminal.choose(
    "fleetctl Node.js guidance (AIO can continue):",
    ["Open Node.js downloads", "Open nvm", "Skip and continue with AIO"],
  );
  if (choice === 2) return;
  const url = choice === 0 ? NODE_DOWNLOADS_URL : NVM_URL;
  const opener = terminal.platform === "darwin" ? "open" : "xdg-open";
  const opened = await terminal.runCommand(`${opener} '${url}'`);
  if (!opened)
    terminal.write(`Could not open browser; use URL directly: ${url}`);
}

async function pollManualFix(
  terminal: Terminal,
  services: InstallationServices,
  options: PreflightOptions,
  checkId: string,
): Promise<CheckResult[]> {
  while (true) {
    let latest: CheckResult[] = [];
    for (let attempt = 0; attempt < POLL_ATTEMPTS; attempt += 1) {
      await terminal.wait(POLL_INTERVAL_MS);
      latest = await services.runPreflightChecks(options);
      const affected = latest.find((result) => result.id === checkId);
      if (affected) printResult(terminal, affected);
      if (affected?.status === "pass") return latest;
    }

    const retry = await terminal.choose("Polling limit reached.", [
      "Retry polling",
      "Stop",
    ]);
    if (retry !== 0) return latest;
  }
}

async function applyFixesSequentially(
  terminal: Terminal,
  services: InstallationServices,
  runtime: RuntimeKind,
  mode: AioMode,
  initialResults: CheckResult[],
): Promise<CheckResult[]> {
  let results = initialResults;
  const handled = new Set<string>();
  const options = { runtime, mode };

  while (true) {
    const candidate = results.find(
      (result) =>
        result.status !== "pass" &&
        !isNodeCheck(result) &&
        (result.remediation?.actions ?? []).some(
          (action) => !handled.has(`${result.id}:${action.id}`),
        ),
    );
    if (!candidate) return results;

    const action = (candidate.remediation?.actions ?? []).find(
      (available) => !handled.has(`${candidate.id}:${available.id}`),
    );
    if (!action) return results;
    handled.add(`${candidate.id}:${action.id}`);

    const command = action.command ?? action.description;
    terminal.write(
      `\n${candidate.blocking ? "Required" : "Optional"} fix: ${action.title}`,
    );
    terminal.write(action.description);
    terminal.write(
      `Action to approve: ${action.command ?? action.description}`,
    );
    if (action.requiresPrivilege || action.manual) {
      terminal.write(
        "This fix needs manual action; utility will not escalate privileges.",
      );
    }

    const choice = await terminal.choose("Run this fix?", [
      "Run",
      "No",
      "Copy command",
    ]);
    if (choice === 2) {
      await copyExactCommand(terminal, command);
      continue;
    }
    if (choice === 1) {
      await offerCopy(terminal, command);
      continue;
    }

    if (action.requiresPrivilege || action.manual) {
      terminal.write(
        `Run this command manually, then return here:\n${command}`,
      );
      await terminal.input(
        "Press Enter after completing the manual fix (Ctrl-C cancels): ",
      );
      results = await pollManualFix(terminal, services, options, candidate.id);
      continue;
    }

    if (action.command) {
      const success = await terminal.runCommand(action.command);
      if (!success)
        terminal.write(
          "Fix command failed or could not start; rechecking condition anyway.",
        );
    }

    results = await services.runPreflightChecks(options);
    const updated = results.find((result) => result.id === candidate.id);
    if (updated) printResult(terminal, updated);
  }
}

function runtimeLabel(runtime: RuntimeDiscovery): string {
  return `${runtime.runtime === "podman" ? "Podman" : "Docker"}: ${runtime.summary}`;
}

async function chooseRuntime(
  terminal: Terminal,
  runtimes: RuntimeDiscovery[],
): Promise<RuntimeKind> {
  const available = runtimes.filter((runtime) => runtime.detected);
  if (available.length === 1) {
    terminal.write(`Using ${available[0].runtime}: ${available[0].summary}`);
    return available[0].runtime;
  }

  const choices = available.map((runtime) =>
    runtime.preferred
      ? "Podman — recommended, project-tested"
      : runtime.runtime === "docker"
        ? "Docker — limited project validation"
        : "Podman",
  );
  const preferredIndex = Math.max(
    0,
    available.findIndex((runtime) => runtime.preferred),
  );
  const selected = await terminal.choose(
    "Choose container runtime to evaluate:",
    choices,
    preferredIndex,
  );
  return available[selected].runtime;
}

async function configurePersistence(
  terminal: Terminal,
  services: InstallationServices,
): Promise<string | undefined> {
  const persist = await terminal.choose(
    "Optional: persist AIO /data storage?",
    ["Persist data", "Skip persistence"],
  );
  if (persist !== 0) return undefined;

  while (true) {
    const path = (
      await terminal.input("Host directory path for AIO /data: ")
    ).trim();
    if (!path) {
      terminal.write("Enter a non-empty host directory path.");
      continue;
    }
    const validation = await services.validateDataPath(path);
    if (validation.status === "usable") return path;
    terminal.write(validation.reason);

    if (validation.status === "missing") {
      const create = await terminal.choose("Directory does not exist.", [
        "Create directory",
        "Change path",
      ]);
      if (create === 0) {
        const approved = await terminal.choose(
          `Create ${path}? This changes host filesystem state.`,
          ["Create", "No"],
        );
        if (approved === 0) {
          try {
            await services.createDataDirectory(path);
            const afterCreate = await services.validateDataPath(path);
            if (afterCreate.status === "usable") return path;
            terminal.write(afterCreate.reason);
          } catch (error) {
            terminal.write(
              `Could not create directory: ${String(error)}. Use a writable path or adjust permissions; utility will not silently escalate.`,
            );
          }
        }
      }
      continue;
    }

    if (validation.status === "permission-denied") {
      terminal.write(
        "Use a directory writable by your account; utility will not silently escalate privileges.",
      );
    }
  }
}

async function maybeLaunch(
  terminal: Terminal,
  services: InstallationServices,
  runtime: RuntimeKind,
  mode: AioMode,
  engineSocketPath?: string,
): Promise<void> {
  const setup = await terminal.choose(
    "Host is compatible. Set up AIO launch?",
    ["Set up launch", "Not now"],
  );
  if (setup !== 0) return;

  terminal.write(
    "\n[3/3] Configure launch · preview before approval",
    "heading",
  );
  const dataPath = await configurePersistence(terminal, services);
  const launchOptions: LaunchOptions = { runtime, mode };
  if (dataPath !== undefined) launchOptions.dataPath = dataPath;
  if (engineSocketPath !== undefined)
    launchOptions.engineSocketPath = engineSocketPath;
  const plan = services.buildLaunchPlan(launchOptions);
  terminal.write(`\nLaunch command preview (${AIO_IMAGE}):`);
  if (mode === "kind") {
    terminal.write(
      "Kind mode uses the documented local/development --privileged mode, which grants broad host access.",
    );
  }
  for (const [key, value] of Object.entries(plan.environment)) {
    terminal.write(`Environment: ${key}=${value}`);
  }
  terminal.write(plan.displayCommand);

  const choice = await terminal.choose("Run this AIO launch command?", [
    "Run",
    "No",
    "Copy command",
  ]);
  if (choice === 1 || choice === 2) {
    if (choice === 1) {
      await offerCopy(terminal, plan.displayCommand);
    } else {
      await copyExactCommand(terminal, plan.displayCommand);
    }
    terminal.write(
      "Launch declined. No image pulled or container started.",
      "muted",
    );
    return;
  }

  const result = await services.executeLaunch(plan);
  if (result.started) {
    terminal.write("AIO started.");
  } else {
    terminal.write(
      `AIO launch failed; host readiness remains unchanged. ${result.error ?? "No error details provided."}`,
    );
  }
}

export async function runInstallationCheck(
  args: string[],
  services: InstallationServices,
  terminal: Terminal,
): Promise<void> {
  terminal.write("OME | FleetShift AIO installation check", "banner");
  if (terminal.platform !== "darwin" && terminal.platform !== "linux") {
    terminal.write(
      `Unsupported host platform: ${terminal.platform}. Preflight cannot report success.`,
      "error",
    );
    return;
  }

  terminal.write("\n[1/3] Discovering local container runtimes…", "heading");
  const runtimes = await services.discoverRuntimes();
  let checkedRuntimes = runtimes;
  const originalServices = services;
  // Every recheck (including remediation polling and fallback) shares a fresh
  // discovery snapshot with launch generation, not the initial runtime menu.
  services = {
    ...originalServices,
    async runPreflightChecks(options) {
      checkedRuntimes = await originalServices.discoverRuntimes();
      const discovery = checkedRuntimes.find(
        (candidate) => candidate.runtime === options.runtime,
      );
      if (!discovery)
        throw new Error("Selected runtime missing from discovery");
      return originalServices.runPreflightChecks({ ...options, discovery });
    },
  };
  for (const runtime of runtimes) terminal.write(runtimeLabel(runtime));
  if (!runtimes.some((runtime) => runtime.detected)) {
    terminal.write(
      "No usable Podman or Docker client detected. Install a local container runtime, then retry.",
      "error",
    );
    return;
  }

  let runtime = await chooseRuntime(terminal, runtimes);
  let mode: AioMode = args.includes("--with-kind") ? "kind" : "basic";
  terminal.write(
    `Selected mode: ${mode === "kind" ? "Kind local-cluster AIO" : "basic AIO"}`,
  );
  terminal.write(`\n[2/3] Checking ${runtime} host prerequisites…`, "heading");
  let results = await services.runPreflightChecks({ runtime, mode });
  printResults(terminal, results);
  await showNodeGuidance(terminal, results);
  results = await applyFixesSequentially(
    terminal,
    services,
    runtime,
    mode,
    results,
  );

  let blocking = results.filter(isBlocking);
  if (mode === "kind" && blocking.length > 0 && blocking.every(isKindOnly)) {
    const basicResults = await services.runPreflightChecks({
      runtime,
      mode: "basic",
    });
    if (!basicResults.some(isBlocking)) {
      const fallback = await terminal.choose(
        "Kind-only prerequisites block Kind readiness; basic AIO checks pass. Disable Kind and continue?",
        ["Disable Kind and continue with basic AIO", "Keep Kind mode"],
      );
      if (fallback === 0) {
        mode = "basic";
        results = basicResults;
        printResults(terminal, results);
      }
    }
  }

  blocking = results.filter(isBlocking);
  if (blocking.length > 0) {
    const alternative = runtimes.find(
      (candidate) =>
        candidate.detected &&
        candidate.runtime !== runtime &&
        candidate.ready &&
        candidate.local,
    );
    if (alternative) {
      const switchRuntime = await terminal.choose(
        `${runtime} remains blocked. ${alternative.runtime} is available; switch and evaluate it?`,
        [`Switch to ${alternative.runtime}`, "Keep selected runtime"],
      );
      if (switchRuntime === 0) {
        runtime = alternative.runtime;
        results = await services.runPreflightChecks({ runtime, mode });
        printResults(terminal, results);
      }
    }
  }

  blocking = results.filter(isBlocking);
  if (blocking.length > 0) {
    terminal.write(
      `AIO is not ready: ${blocking.map((result) => result.title).join(", ")}.`,
      "error",
    );
    return;
  }

  const warnings = results.filter((result) => result.status === "warning");
  terminal.write(
    warnings.length > 0 ? "Host compatible with warnings." : "Host compatible.",
    warnings.length > 0 ? "warning" : "success",
  );
  for (const warning of warnings)
    terminal.write(`Warning: ${warning.title}: ${warning.reason}`);
  const selectedRuntime = checkedRuntimes.find(
    (candidate) => candidate.runtime === runtime,
  );
  await maybeLaunch(
    terminal,
    services,
    runtime,
    mode,
    selectedRuntime?.engineSocketPath,
  );
}
