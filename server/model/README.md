# Managed-resource workflow model

This is a handwritten Quint model of the resource manager's Create, Update,
Delete, orchestration, and deletion cleanup protocol on `fleet-controller-core`.
It preserves the current protocol alongside a proposed correction. **Only the
model is corrected; application behavior is unchanged.** No model traces drive
application tests yet.

This model needs review too. The existing
[delivery POC](../../poc/fulfillment-delivery/README.md) was not heavily reviewed
and supplies no guarantees or trusted assumptions to this model. Target-side
delivery is an explicit abstraction here.

## Reading and running it

Start with the `run` blocks in [counterexamples.qnt](counterexamples.qnt).
Each `.then(...)` is one interleaving step. In the main workflow model,
`db` is the committed fulfillment view and the other records track the
workflow bodies. A `*Reply` phase means the side effect happened and the
result still needs a workflow checkpoint. The `exit` phase means the
workflow decided to return and still owns its backend instance ID. The
[source correspondence table](#source-correspondence) and comments above the
action groups map these steps to Go methods.

- [managed_resources.qnt](managed_resources.qnt): workflow state, transitions,
  invariants, fairness, and current/corrected configurations.
- [counterexamples.qnt](counterexamples.qnt): executable reproductions of current
  gaps and regressions that the current Update path already handles.
- [managed_resources_test.qnt](managed_resources_test.qnt): corrected recovery
  scenarios, including failed notification and cleanup retries.
- [aggregate.qnt](aggregate.qnt): repository revisions, overlapping copies,
  creation decisions, incarnation identity, and reconciliation-result merging.
- [aggregate_test.qnt](aggregate_test.qnt): concrete repository-contract scenarios.
- [Workflow boundaries](workflow_boundaries.md) covers the focused models for
  delivery completion, Postgres instance admission, and a proposed durable
  wake operation. Each model keeps its executable scenarios next to the
  configuration it checks.

Prerequisites: Quint **0.33.1** on PATH and Java.
Quint's TLC backend first uses Apalache to translate the model to TLA+, then runs
the TLC bundled in that distribution. Run `server:model:bootstrap` once to let
Quint download the pinned Apalache **0.62.1** distribution into its cache. That
bootstrap checks only the initial state; the verification target below uses TLC.
The compiler starts a local service on port 8822;
restricted execution environments must permit that listener.

From the workspace root:

```sh
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:bootstrap
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:fmt
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:check
```

The combined check runs these independently available targets in sequence:

```sh
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:typecheck
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:test
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:reproduce
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:verify
```

`model:reproduce` succeeds only when the designated broken properties produce
actual TLC counterexamples; compiler errors, process failures, and unexpected
successes fail the target. It also checks the existing Update recovery as a
positive control. `model:verify` exhaustively checks the reachable states of the
finite configurations and their fair temporal properties. Neither target is
application conformance testing or a proof of the Go implementation.

For one check, pass the module, property, expected result (`holds` or `violated`),
and optionally its file:

```sh
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:verify \
  --args='current_delete mutationProgress violated'
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:model:verify \
  --args='current_aggregate safety holds aggregate.qnt'
```

Full TLC output, including state-by-state counterexamples, goes into the ignored
`_out/` directory. Apalache compiler logs go into `_apalache-out/`. The runner
prints each result and state count. There are no generated Go tests, trace
adapters, networked Fleet instances, or container dependencies.

## Current behavior and reproduced gaps

The workflow configurations named `current_*` reflect the current code. Checks
without maintenance isolate the guarantees of the existing durable workflows.
Fleet currently also runs maintenance: these counterexamples demonstrate why
it is still needed, rather than establishing a production outage with a healthy,
fairly scheduled maintenance pass.

| Configuration/property                               | Result         | What the counterexample establishes                                                                                                                  |
| ---------------------------------------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `current_create / eventualProgress`                  | Counterexample | The resource commits, but Create's dispatch activity exhausts its retries. The workflow returns an error, leaving no orchestration owner.            |
| `current_delete / mutationProgress`                  | Counterexample | After Delete commits, its workflow or cleanup can lose the progress obligation. This check excludes failures confined to the initial Create.         |
| `current_delete / noPausedEntry`                     | Counterexample | A Start delayed after a successful Create preflight can load a fulfillment that another workflow has since paused. Orchestration has no entry guard. |
| `current_delete / noRepeatedRemoval`                 | Counterexample | Removal and completion commit, notification fails, and orchestration enters the deletion pipeline again despite persisted readiness.                 |
| `current_update / mutationProgress`                  | Holds          | Once an Update commits, losing its mutation reply or racing an exiting orchestration does not strand that work in this model.                        |
| `current_delete_with_maintenance / eventualProgress` | Holds          | Fair level-based maintenance repairs the modeled lost obligations.                                                                                   |

The executable traces distinguish two important failure schedules:

1. **Lost Delete commit reply:** the conditional mutation commits generation 2;
   its reply is lost; the same activity retries with its original etag; that
   retry returns a conflict; `Run` exits before starting cleanup. Another
   orchestration can even finish delivery removal, but its notification has no
   cleanup listener. The resource still cannot finish deletion.
2. **Failed cleanup:** cleanup has already been started, delivery removal has
   completed, and its readiness is durable. Exhausting a cleanup read/delete
   activity's retries terminates that workflow. The mutation and orchestration
   workflows may already have returned; a new workflow must be dispatched to
   finish removing the rows.

A lost **Create** commit reply alone is not a counterexample: replaying its
accepted creation nonce resolves the same incarnation. A rejected nonce remains
rejected after name reuse. Likewise, the current shared `convergenceLoop`
already retries `AlreadyRunning`; it is not modeled as swallowing that result.
Current Update additionally preserves its pinned target/outcome through retries
and checks current work after an ambiguous mutation.

`noPausedEntry` concerns starting a pass from a snapshot already marked paused.
It does not claim that a pause can retroactively stop effects from an earlier
in-flight pass. `noRepeatedRemoval` detects another pass through the deletion
pipeline. Once successful cleanup empties the target and delivery lists, that
pass can amount to redundant reads and cleanup rather than another addon call.

## Corrected protocol

The `corrected_*` configurations change the modeled protocol in four ways:

1. **Keep the durable mutation owner.** Create replays its nonce after exhausted
   persistence/dispatch retries. Delete preserves its pinned attempt and settled
   outcome, checks the current incarnation's work even after an ambiguous
   mutation returns a conflict, and ensures cleanup was started. Transient
   exhaustion continues the workflow instead of ending its obligation.
2. **Enforce orchestration entry centrally.** A missing or paused fulfillment
   causes an idle exit. A completed deletion bypasses external delivery work.
   The caller's preflight remains an optimization, not enforcement.
3. **Retain notification responsibility.** A completed deletion still retries
   signaling its cleanup listener. Skipping both delivery and notification is
   incorrect: `guard_without_notification` reproduces a waiting cleanup workflow
   stranded after the earlier completion signal fails.
4. **Keep cleanup durable through exhausted retries.** Its readiness check and
   row deletion retry without requiring maintenance to create another workflow.

Current Update's repository fence is a prerequisite for generalizing its
recovery to Delete. The model's `settleMutation` is an abstraction of that
contract, refined in `aggregate.qnt`: before returning after a failed/ambiguous
attempt, retire its pinned revision or establish that another write already did.
The current Delete activity does **not** yet check/advance that root revision.
Implementing this proposal requires extending its mutation/settlement path;
simply ignoring its stale-etag error would be insufficient.

These changes add no cancellation API, per-mutation ledger, retention policy,
or background recovery task. Continue-as-new is abstracted as re-entering the
same control point with durable identity/outcome intact. The model does not
measure workflow history size or claim that continued histories are garbage
collected. A Go implementation should preserve the existing bounded-run pattern.

## Source correspondence

Database commits are separate from replies/checkpoints. A committed mutation
survives `lose*Reply`; retrying the activity is a new transaction. Orchestration's
last generation comparison and backend instance release are separate as well.
This is necessary to reproduce `AlreadyRunning` during workflow exit.

| Model transition/state                                                    | Implementation boundary                                                                                                                                                                                                    |
| ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `persistCreate`, `checkpointCreate`, `creator.accepted`                   | [Create persistence and nonce replay](../internal/domain/create_managed_resource.go); accepted/rejected decisions survive the aggregate.                                                                                   |
| `createPreflight`, `startCreateOrchestration`                             | Separate read transaction and `Orchestration.Start` in `CreateManagedResourceWorkflowSpec.StartOrchestration`.                                                                                                             |
| `requestMutation`, `mutate`, `settleMutation`, progress/start checkpoints | [Update workflow](../internal/domain/update_managed_resource.go) and its [durable continuation state](../internal/domain/update_managed_resource_state.go).                                                                |
| Conditional Delete, cleanup start, own-generation progress check          | [Delete workflow](../internal/domain/delete_managed_resource.go) and [shared convergence loop](../internal/domain/mutation_workflow.go).                                                                                   |
| `loadOrchestration`, `completeEffects`/`authFailure`                      | [Orchestration](../internal/domain/orchestration.go): `AcquireLockAndLoad`, placement/delete pipeline, and authentication pause result.                                                                                    |
| `persistReconciliation`, notification, checkpoint                         | `PersistAndCompleteReconciliation`: fresh aggregate merge, observation/lock commit, then signal, then activity result.                                                                                                     |
| `orchestration.pc == "exit"`, `exitOrchestration`                         | `Run` has decided to return, but the [workflow backend](../internal/infrastructure/goworkflows/registry.go) still reserves the fulfillment's instance ID until it records completion.                                      |
| Cleanup check, buffered signal, erasure, replay                           | [Cleanup workflow](../internal/domain/delete_managed_resource_cleanup.go); row deletion is scoped to the original fulfillment identity.                                                                                    |
| Optional `maintain`                                                       | [Maintenance](../internal/application/managed_resource_maintenance.go): level scan, start cleanup first, signal persisted readiness, start pending unpaused orchestration.                                                 |
| Root revision vs intent version vs generation                             | [ExtensionResource](../internal/domain/extension_resource.go) and [Fulfillment](../internal/domain/fulfillment.go). Label-only changes/fences advance only the root revision; Delete advances only fulfillment generation. |
| Atomic `saving` and `settle`                                              | Repository `Update`/`FenceUpdate` in [PostgreSQL](../internal/infrastructure/postgres/extension_resource_repo.go) and [SQLite](../internal/infrastructure/sqlite/extension_resource_repo.go).                              |
| Atomic `create` with retained decision                                    | `Create` plus its [decision projection](../internal/infrastructure/postgres/managed_resource_creation.go); unique constraints arbitrate competing transactions.                                                            |
| `mergeReconciliation`                                                     | `Fulfillment.MergeReconciliationResult` and `CompleteReconciliation`: old results cannot restore older lifecycle/auth or regress observation.                                                                              |

The aggregate model includes deliberately broken alternatives that omit failure
fencing, label revision advancement, result merging guards, observation
monotonicity, cleanup identity, or remembered creation rejections. Each must
violate its associated invariant.
Those variants are negative controls, not additional claims of current defects.

## Bounds and assumptions

- The workflow graph contains one name/incarnation, its Create, and zero or one
  later spec Update/Delete. It reaches generations 0–2. Etag equality covers
  the fields represented there; it is not a specification of the hash encoding.
- The repository graph adds two loaded attempts, two nonces, two caller scopes
  (anonymous and authenticated), and at most two accepted incarnations of that
  name. Generations are capped at 4. UID and fulfillment identity have a stable
  one-to-one correspondence within the model. Payloads/labels are binary values.
  One initial reconciliation result can replay independently after newer passes
  or resource replacement.
- Each workflow verification has one abstract fault; scenario tests also use
  two. A fault can stand for an entire exhausted finite activity retry budget,
  not just one RPC error. Crashes/checkpoint loss consume the same bounded
  budget; faults eventually stop.
- Weak fairness schedules each continuously enabled workflow/cleanup/maintenance
  actor. External requests need not arrive fairly. Writes are finitely bounded.
  The main workflow model assumes durable workflow storage, timers, unique
  active instance IDs, and durable buffered signals. The focused
  [Postgres admission model](workflow_instances.qnt) checks the uniqueness
  assumption separately. Progress assumes finite outages and continued
  execution of the workflows.
- Delivery/placement/removal is one abstract pass which eventually completes or
  reports an authorization pause. Removal is idempotent; successful removal
  includes target cleanup. This does not verify any provider's implementation,
  per-target journal, partial effect, rollout, or fencing behavior.
- `settled` permits an explicit authentication pause and distinguishes it from
  successful delivery. A deleting row is otherwise unsettled until hard deletion.
  `deletionProgress` separately requires eventual row removal when no pause
  occurs. Permanent delivery failures and their surfaced API status are not
  modeled as transient faults.
- Repository operations are transaction-linearized. Atomic intent/derived-strategy
  persistence is a required storage guarantee, not a proof of SQL statements or
  locking. Revision fencing is exercised in both commit-before-fence and
  fence-before-commit orders, including delayed copies after the owner returns.
- The optional maintenance action abstracts a fresh scan/dispatch; its read/start
  race is not separately modeled. The corrected checks disable it entirely.

This is not an unbounded proof or a refinement proof between the two models.
Larger caller counts, no-op updates, Resume/Touch, provider inventory, changing
target pools, terminal delivery failures, explicit workflow cancellation, and
actual workflow serialization/replay need further review or modeling. In
particular the shared loop's newer-generation shortcut is retained in the source
mapping, but generations above its own are not reached by this workflow bound.
The model also does not check the shared loop's direct `Start` side effect or its
unbounded polling history. Those are implementation concerns for later conformance
work, not guarantees inferred from these results.
