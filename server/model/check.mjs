import { spawnSync } from "node:child_process";
import { closeSync, mkdirSync, openSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Keep model checks explicit; they do not execute or drive application tests.
const cwd = fileURLToPath(new URL(".", import.meta.url));
const phase = process.argv[2] ?? "test";
const version = spawnSync("quint", ["--version"], { encoding: "utf8" });
if (version.status !== 0 || version.stdout.trim() !== "0.33.1") {
  throw new Error(
    "These checks require Quint 0.33.1 on PATH (and Java for TLC).",
  );
}

function quint(...args) {
  const result = spawnSync("quint", args, { cwd, stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function verify(
  selected,
  property,
  expected = "holds",
  file = "managed_resources.qnt",
) {
  if (!["holds", "violated"].includes(expected)) {
    throw new Error(`Unknown expected result: ${expected}`);
  }
  const kind = [
    "eventualProgress",
    "mutationProgress",
    "deletionProgress",
  ].includes(property)
    ? "temporal"
    : "invariant";
  const args = [
    "verify",
    file,
    `--main=${selected}`,
    "--backend=tlc",
    "--apalache-version=0.62.1",
    "--tlc-config=tlc.json",
    `--${kind}=${property}`,
    "--verbosity=3",
  ];
  mkdirSync(new URL("./_out/", import.meta.url), { recursive: true });
  const log = new URL(`./_out/${selected}-${property}.log`, import.meta.url);
  const descriptor = openSync(log, "w");
  let result;
  try {
    // Write incrementally so long searches can be inspected while running.
    // A timeout or tool failure must never count as an expected counterexample.
    result = spawnSync("quint", args, {
      cwd,
      stdio: ["ignore", descriptor, descriptor],
      timeout: 600_000,
    });
  } finally {
    closeSync(descriptor);
  }
  if (result.error) throw result.error;
  const output = readFileSync(log, "utf8");
  const violated =
    result.status === 1 &&
    /error: found a counterexample/.test(output) &&
    /Error: (?:Temporal property q_temporalProps was violated|Invariant q_inv is violated)/.test(
      output,
    );
  const holds =
    result.status === 0 &&
    /Model checking completed\. No error has been found\./.test(output);
  console.log(
    `${selected}/${property}: ${violated ? "counterexample" : holds ? "holds" : "CHECKER ERROR"}; ${fileURLToPath(log)}`,
  );
  console.log(
    output
      .split("\n")
      .filter((line) =>
        /states generated|distinct states|depth of|Model checking completed|Error:/.test(
          line,
        ),
      )
      .join("\n"),
  );
  if (
    (expected === "holds" && !holds) ||
    (expected === "violated" && !violated)
  ) {
    throw new Error(
      `Expected ${selected}/${property} to ${expected}; inspect ${fileURLToPath(log)}`,
    );
  }
}

if (phase === "bootstrap") {
  // This initial-state-only Apalache run lets Quint install the pinned tool
  // distribution. Subsequent verification uses exhaustive TLC, not this bound.
  quint(
    "verify",
    "managed_resources.qnt",
    "--main=current_create",
    "--backend=apalache",
    "--apalache-version=0.62.1",
    "--invariant=safety",
    "--max-steps=0",
  );
} else if (phase === "typecheck") {
  for (const file of [
    "managed_resources.qnt",
    "counterexamples.qnt",
    "managed_resources_test.qnt",
    "aggregate.qnt",
    "aggregate_test.qnt",
    "delivery_reports.qnt",
    "workflow_instances.qnt",
    "orchestration_handoff.qnt",
  ]) {
    quint("typecheck", file);
  }
} else if (phase === "test") {
  for (const main of [
    "create_counterexamples",
    "delete_counterexamples",
    "update_regressions",
  ]) {
    quint("test", "counterexamples.qnt", `--main=${main}`, "--seed=1");
  }
  quint("test", "managed_resources_test.qnt", "--seed=1");
  quint("test", "aggregate_test.qnt", "--seed=1");
  for (const [file, main] of [
    ["delivery_reports.qnt", "current_delivery_reports"],
    ["delivery_reports.qnt", "persisted_delivery_reports"],
    ["workflow_instances.qnt", "current_postgres_instances"],
    ["workflow_instances.qnt", "unique_active_instances"],
    ["orchestration_handoff.qnt", "execution_signal_handoff"],
    ["orchestration_handoff.qnt", "durable_pending_handoff"],
  ]) {
    quint("test", file, `--main=${main}`, "--seed=1");
  }
} else if (phase === "verify") {
  if (process.argv[3]) {
    verify(
      process.argv[3],
      process.argv[4] ?? "safety",
      process.argv[5] ?? "holds",
      process.argv[6] ?? "managed_resources.qnt",
    );
  } else {
    for (const main of ["current_create", "current_update", "current_delete"])
      verify(main, "safety");
    verify("current_update", "mutationProgress");
    verify("current_delete_with_maintenance", "eventualProgress");
    verify("current_aggregate", "safety", "holds", "aggregate.qnt");
    for (const main of [
      "corrected_create",
      "corrected_update",
      "corrected_delete",
    ]) {
      verify(main, "correctedSafety");
      verify(main, "eventualProgress");
    }
    verify("corrected_delete", "deletionProgress");
    verify(
      "persisted_delivery_reports",
      "eventualProgress",
      "holds",
      "delivery_reports.qnt",
    );
    verify(
      "unique_active_instances",
      "oneActiveExecution",
      "holds",
      "workflow_instances.qnt",
    );
    verify(
      "durable_pending_handoff",
      "publishedWorkHasOwner",
      "holds",
      "orchestration_handoff.qnt",
    );
    verify(
      "durable_pending_handoff",
      "eventualProgress",
      "holds",
      "orchestration_handoff.qnt",
    );
  }
} else if (phase === "reproduce") {
  for (const [main, property, expected] of [
    ["current_create", "eventualProgress", "violated"],
    ["current_delete", "mutationProgress", "violated"],
    ["current_delete", "noPausedEntry", "violated"],
    ["current_delete", "noRepeatedRemoval", "violated"],
    ["current_update", "mutationProgress", "holds"],
    ["guard_without_notification", "eventualProgress", "violated"],
  ])
    verify(main, property, expected);
  for (const [main, property] of [
    ["without_fencing", "revisionsFenceWrites"],
    ["without_label_revision", "revisionsFenceWrites"],
    ["without_result_merge_guard", "newerDecisionsSurvive"],
    ["without_cleanup_identity", "replacementIsSafe"],
    ["without_rejected_nonce", "creationDecisionsAreStable"],
    ["without_observation_monotonicity", "observationIsMonotone"],
  ])
    verify(main, property, "violated", "aggregate.qnt");
  verify(
    "current_delivery_reports",
    "eventualProgress",
    "violated",
    "delivery_reports.qnt",
  );
  verify(
    "current_postgres_instances",
    "oneActiveExecution",
    "violated",
    "workflow_instances.qnt",
  );
  verify(
    "execution_signal_handoff",
    "publishedWorkHasOwner",
    "violated",
    "orchestration_handoff.qnt",
  );
} else {
  throw new Error(`Unknown check phase: ${phase}`);
}
