/// Resource-manager workflow handoffs, independently of the delivery POC.
/// One name has one initial Create and at most one later spec Update or Delete.
/// Database commits, activity checkpoints, and backend instance release are
/// distinct transitions. See README.md for source correspondence and boundaries.
module managed_resources {
  const OPERATION: str
  const RECOVER_CREATE_DELETE: bool
  const GUARD_ORCHESTRATION_ENTRY: bool
  const NOTIFY_COMPLETED_DELETE: bool
  const RECOVER_CLEANUP: bool
  const MAINTENANCE: bool
  const FAULT_BUDGET: int

  type Database = {
    exists: bool,
    generation: int,
    observed: int,
    deleting: bool,
    paused: bool,
    removed: bool,
  }

  // Equality of the visible fields represented here, NOT just generation.
  // Root revisions, labels, and incarnation fencing are checked separately in
  // aggregate.qnt. Real etags also cover those and other visible status fields.
  type Token = { generation: int, observed: int, deleting: bool, paused: bool }
  type CreateCall = { pc: str, accepted: bool }
  type MutationCall = {
    pc: str,
    target: Token,
    outcome: str,
    myGeneration: int,
    freshStart: bool,
    cleanupStarted: bool,
  }
  type Orchestration = {
    pc: str,
    generation: int,
    deleting: bool,
    pausedAtLoad: bool,
    pauseResult: bool,
    restart: bool,
  }
  type CleanupCall = { pc: str, signaled: bool }
  type State = {
    db: Database,
    create: CreateCall,
    mutation: MutationCall,
    orchestration: Orchestration,
    cleanup: CleanupCall,
    faults: int,
    loadedWhilePaused: bool,
    repeatedRemoval: bool,
    unsafeDeletion: bool,
  }

  var state: State
  // The model keeps separate control positions for each Go workflow and the
  // fulfillment record they share. A *Reply phase follows an activity side
  // effect but precedes the workflow engine recording the activity result.
  val db = state.db
  val creator = state.create
  val mutation = state.mutation
  val orchestration = state.orchestration
  val cleanup = state.cleanup
  val token: Token = {
    generation: db.generation, observed: db.observed,
    deleting: db.deleting, paused: db.paused,
  }
  val deletionReady = db.exists and db.deleting and not(db.paused) and db.observed >= db.generation
  val updateRecovery = OPERATION == "update" or RECOVER_CREATE_DELETE

  pure val emptyToken: Token = { generation: 0, observed: 0, deleting: false, paused: false }
  pure def maximum(a: int, b: int): int = if (a > b) a else b

  action init = state' = {
    db: { exists: false, generation: 0, observed: 0, deleting: false, paused: false, removed: false },
    create: { pc: "persist", accepted: false },
    mutation: {
      pc: "idle", target: emptyToken, outcome: "unknown", myGeneration: 0,
      freshStart: false, cleanupStarted: false,
    },
    orchestration: {
      pc: "idle", generation: 0, deleting: false, pausedAtLoad: false,
      pauseResult: false, restart: false,
    },
    cleanup: { pc: "off", signaled: false },
    faults: FAULT_BUDGET,
    loadedWhilePaused: false, repeatedRemoval: false, unsafeDeletion: false,
  }

  // CreateManagedResourceWorkflowSpec.Run

  /// PersistManagedResource commits acceptance, intent, and fulfillment together.
  /// A replay uses the accepted nonce; it does not create a second incarnation.
  action persistCreate = all {
    creator.pc == "persist",
    state' = if (creator.accepted and not(db.exists))
      state.with("create", creator.with("pc", "done"))
    else
      state
        .with("db", if (creator.accepted) db else db.with("exists", true).with("generation", 1))
        .with("create", { pc: "persistReply", accepted: true }),
  }

  action checkpointCreate = all {
    creator.pc == "persistReply",
    state' = state.with("create", creator.with("pc", "preflight")),
  }

  /// Create's StartOrchestration activity first reads the fulfillment. Its
  /// read transaction ends before the call to the workflow backend.
  action createPreflight = all {
    creator.pc == "preflight",
    state' = state.with("create", creator.with("pc",
      if (not(db.exists) or db.paused or deletionReady) "done" else "start")),
  }

  action startCreateOrchestration = all {
    creator.pc == "start",
    state' = state
      .with("orchestration", if (orchestration.pc == "idle") orchestration.with("pc", "load") else orchestration)
      .with("create", creator.with("pc", "startReply")),
  }

  // Current Create treats both a fresh Start and AlreadyRunning as success.
  action checkpointCreateStart = all {
    creator.pc == "startReply",
    state' = state.with("create", creator.with("pc", "done")),
  }

  action loseCreateReply = all {
    creator.pc == "persistReply" or creator.pc == "startReply",
    state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("create", creator.with("pc", if (creator.pc == "persistReply") "persist" else "preflight")),
  }

  /// One transition represents exhaustion of the finite activity retry budget,
  /// not a single failed attempt. No database commit is undone by this error.
  action exhaustCreate = all {
    Set("persist", "preflight", "start").contains(creator.pc),
    state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("create", creator.with("pc", if (RECOVER_CREATE_DELETE) "persist" else "done")),
  }

  // UpdateManagedResourceWorkflowSpec.Run / DeleteManagedResourceWorkflowSpec.Run

  /// External requests need not arrive fairly. There is at most one, so writes
  /// eventually quiesce. Preparing an Update and supplying a Delete etag both
  /// pin the visible state before the first mutation activity runs.
  action requestMutation = all {
    OPERATION != "none", mutation.pc == "idle", db.exists, not(db.deleting),
    state' = state.with("mutation", mutation.with("pc", "mutate").with("target", token)),
  }

  // MutateSpec / MutateToDeleting. This transition commits the transaction.
  // Only checkpointMutation gives the reply to the workflow body.
  action mutate = all {
    mutation.pc == "mutate",
    val matches = db.exists and token == mutation.target
    state' = state
      .with("db", if (matches)
        db.with("generation", db.generation + 1).with("paused", false).with("deleting", OPERATION == "delete")
        else db)
      .with("mutation", mutation.with("pc", "mutationReply")
        .with("outcome", if (matches) "success" else "conflict")
        .with("myGeneration", if (matches) db.generation + 1 else 0)),
  }

  action loseMutationReply = all {
    mutation.pc == "mutationReply", state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("mutation", mutation.with("pc", "mutate").with("outcome", "unknown")),
  }

  action checkpointMutation = all {
    mutation.pc == "mutationReply",
    state' = state.with("mutation", mutation.with("pc",
      if (updateRecovery) "settle"
      else if (mutation.outcome == "conflict") "done"
      else "cleanupStart")),
  }

  /// Current Update settles ambiguous attempts using the pinned repository
  /// revision before checking progress. aggregate.qnt refines this atomic step.
  action settleMutation = all {
    mutation.pc == "settle",
    state' = state.with("mutation", mutation.with("pc", "check")),
  }

  action startCleanup = all {
    mutation.pc == "cleanupStart",
    state' = state
      .with("cleanup", if (cleanup.pc == "off") cleanup.with("pc", "check") else cleanup)
      .with("mutation", mutation.with("pc", "cleanupReply")),
  }

  action checkpointCleanupStart = all {
    mutation.pc == "cleanupReply",
    state' = state.with("mutation", mutation.with("pc", "check").with("cleanupStarted", true)),
  }

  // Update calls LoadProgress; current Delete uses convergenceLoop with its
  // own generation. The two branches preserve that distinction.
  action checkMutationProgress = all {
    mutation.pc == "check",
    val needsCleanup = OPERATION == "delete" and db.exists and db.deleting and not(mutation.cleanupStarted)
    val done = if (OPERATION == "update" or RECOVER_CREATE_DELETE)
      not(db.exists) or db.paused or (OPERATION == "update" and db.deleting) or db.observed >= db.generation
      else not(db.exists) or db.observed >= mutation.myGeneration or db.generation > mutation.myGeneration
    state' = state.with("mutation", mutation.with("pc",
      if (needsCleanup) "cleanupStart" else if (done) "done" else "start")),
  }

  action startMutationOrchestration = all {
    mutation.pc == "start",
    val fresh = orchestration.pc == "idle"
    state' = state
      .with("orchestration", if (fresh) orchestration.with("pc", "load") else orchestration)
      .with("mutation", mutation.with("pc", "startReply").with("freshStart", fresh)),
  }

  /// Both current Update and the shared convergenceLoop retry AlreadyRunning.
  /// The latter is NOT modeled as simply swallowing this race.
  action checkpointMutationStart = all {
    mutation.pc == "startReply",
    state' = state.with("mutation", mutation.with("pc", if (mutation.freshStart) "done" else "check")),
  }

  action loseMutationStartReply = all {
    Set("startReply", "cleanupReply").contains(mutation.pc), state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("mutation", mutation.with("pc", if (mutation.pc == "startReply") "start" else "cleanupStart")),
  }

  action exhaustMutation = all {
    Set("mutate", "check", "start", "cleanupStart", "settle").contains(mutation.pc), state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("mutation", mutation.with("pc", if (updateRecovery) mutation.pc else "done")),
  }

  // OrchestrationWorkflowSpec.Run

  /// AcquireLockAndLoad captures a snapshot for one reconciliation pass.
  /// Instance-ID uniqueness is assumed here and checked separately in
  /// workflow_instances.qnt. The fulfillment's active-workflow marker can
  /// clear before the backend releases the instance ID.
  action loadOrchestration = all {
    orchestration.pc == "load",
    val stop = not(db.exists) or (GUARD_ORCHESTRATION_ENTRY and db.paused)
    val completedDelete = GUARD_ORCHESTRATION_ENTRY and deletionReady
    state' = state
      .with("loadedWhilePaused", state.loadedWhilePaused or (db.exists and db.paused and not(GUARD_ORCHESTRATION_ENTRY)))
      .with("orchestration", if (stop) orchestration.with("pc", "exit")
        else if (completedDelete) orchestration.with("pc", if (NOTIFY_COMPLETED_DELETE) "notify" else "exit").with("restart", false)
        else orchestration.with("pc", "effects").with("generation", db.generation)
          .with("deleting", db.deleting).with("pausedAtLoad", db.paused)),
  }

  /// A successful placement/delivery pass, or removal plus CleanupDeliveryData.
  /// Target-side mechanics are assumptions here, not inherited from the POC.
  action completeEffects = all {
    orchestration.pc == "effects",
    state' = state
      .with("db", if (orchestration.deleting) db.with("removed", true) else db)
      .with("repeatedRemoval", state.repeatedRemoval or (orchestration.deleting and deletionReady))
      .with("orchestration", orchestration.with("pc", "persist").with("pauseResult", false)),
  }

  action authFailure = all {
    orchestration.pc == "effects",
    state' = state.with("orchestration", orchestration.with("pc", "persist").with("pauseResult", true)),
  }

  // PersistAndCompleteReconciliation: commit the merge and generation check,
  // then send any cleanup signal, then checkpoint the result.
  action persistReconciliation = all {
    orchestration.pc == "persist", db.exists,
    val current = orchestration.generation == db.generation
    state' = state
      .with("db", db.with("observed", maximum(db.observed, orchestration.generation))
        .with("paused", if (current) orchestration.pauseResult else db.paused))
      .with("orchestration", orchestration.with("pc", "persistReply")
        .with("restart", db.generation > orchestration.generation)),
  }

  // A cleanup may have removed the fulfillment after this pass loaded it.
  // Persist retries fail, then the continued run observes NotFound and exits.
  action persistAfterErasure = all {
    orchestration.pc == "persist", not(db.exists),
    state' = state.with("orchestration", orchestration.with("pc", "load")),
  }

  action afterPersist = all {
    orchestration.pc == "persistReply",
    val notify = orchestration.deleting and not(orchestration.pauseResult) and not(orchestration.restart)
    state' = state.with("orchestration", orchestration.with("pc", if (notify) "notify" else "completeReply")),
  }

  /// Signaling happens AFTER the DB commit and BEFORE the activity checkpoint.
  /// A listener buffers a successful signal even before it starts waiting.
  action notifyCleanup = all {
    orchestration.pc == "notify", cleanup.pc != "off",
    state' = state.with("cleanup", cleanup.with("signaled", true))
      .with("orchestration", orchestration.with("pc", "completeReply")),
  }

  action missingCleanupListener = all {
    orchestration.pc == "notify", cleanup.pc == "off",
    state' = state.with("orchestration", orchestration.with("pc", "load")),
  }

  action checkpointReconciliation = all {
    orchestration.pc == "completeReply",
    state' = state.with("orchestration", orchestration.with("pc", if (orchestration.restart) "load" else "exit")),
  }

  /// Loss before the checkpoint can replay the activity. Exhausting its retries
  /// instead takes releaseLockAndContinue and reloads in the next execution.
  action loseReconciliationReply = all {
    Set("persistReply", "completeReply").contains(orchestration.pc), state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("orchestration", orchestration.with("pc", "persist")),
  }

  action exhaustReconciliation = all {
    Set("load", "effects", "persist", "persistReply", "notify", "completeReply").contains(orchestration.pc),
    state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("orchestration", orchestration.with("pc", "load")),
  }

  action exitOrchestration = all {
    orchestration.pc == "exit",
    state' = state.with("orchestration", orchestration.with("pc", "idle")),
  }

  // DeleteManagedResourceCleanupWorkflowSpec.Run

  // CleanupRequired reads readiness. A waiting run can advance on its signal.
  action checkCleanup = all {
    cleanup.pc == "check",
    state' = state.with("cleanup", cleanup.with("pc",
      if (not(db.exists)) "off" else if (deletionReady) "erase" else "wait")),
  }

  action receiveCleanupSignal = all {
    cleanup.pc == "wait", cleanup.signaled,
    state' = state.with("cleanup", cleanup.with("pc", "erase")),
  }

  action eraseResource = all {
    cleanup.pc == "erase",
    state' = state.with("db", db.with("exists", false))
      .with("unsafeDeletion", state.unsafeDeletion or (db.exists and not(db.removed)))
      .with("cleanup", cleanup.with("pc", "eraseReply")),
  }

  action checkpointErasure = all {
    cleanup.pc == "eraseReply",
    state' = state.with("cleanup", cleanup.with("pc", "off")),
  }

  action loseErasureReply = all {
    cleanup.pc == "eraseReply", state.faults > 0,
    state' = state.with("faults", state.faults - 1).with("cleanup", cleanup.with("pc", "erase")),
  }

  action exhaustCleanup = all {
    Set("check", "erase").contains(cleanup.pc), state.faults > 0,
    state' = state.with("faults", state.faults - 1)
      .with("cleanup", cleanup.with("pc", if (RECOVER_CLEANUP) cleanup.pc else "off")),
  }

  /// Optional level-based repair, abstracting RunOnce's paginated scan and
  /// idempotent dispatch. Not part of the no-maintenance protocol checks.
  action maintain = all {
    MAINTENANCE, db.exists,
    db.deleting or (not(db.paused) and db.observed < db.generation),
    state' = if (db.deleting and cleanup.pc == "off")
      state.with("cleanup", cleanup.with("pc", "check"))
    else if (deletionReady)
      state.with("cleanup", cleanup.with("signaled", true))
    else if (not(db.paused) and orchestration.pc == "idle")
      state.with("orchestration", orchestration.with("pc", "load"))
    else state,
  }

  action createStep = any {
    persistCreate, checkpointCreate, createPreflight, startCreateOrchestration,
    checkpointCreateStart, loseCreateReply, exhaustCreate,
  }
  action mutationStep = any {
    mutate, loseMutationReply, checkpointMutation, settleMutation, startCleanup,
    checkpointCleanupStart, checkMutationProgress, startMutationOrchestration,
    checkpointMutationStart, loseMutationStartReply, exhaustMutation,
  }
  action orchestrationStep = any {
    loadOrchestration, completeEffects, authFailure, persistReconciliation, persistAfterErasure,
    afterPersist, notifyCleanup, missingCleanupListener, checkpointReconciliation,
    loseReconciliationReply, exhaustReconciliation, exitOrchestration,
  }
  action cleanupStep = any {
    checkCleanup, receiveCleanupSignal, eraseResource, checkpointErasure,
    loseErasureReply, exhaustCleanup,
  }
  action stutter = state' = state
  action step = any { requestMutation, createStep, mutationStep, orchestrationStep, cleanupStep, maintain, stutter }

  val noPausedEntry = not(state.loadedWhilePaused)
  val noRepeatedRemoval = not(state.repeatedRemoval)
  val deletionIsSafe = not(state.unsafeDeletion)
  val safety = and {
    state.faults >= 0, state.faults <= FAULT_BUDGET,
    db.observed >= 0, db.observed <= db.generation, db.generation <= 2,
    deletionIsSafe,
  }
  val correctedSafety = safety and noPausedEntry and noRepeatedRemoval
  // A pause is an explicit suspended outcome, not successful delivery. A
  // deleting row still requires cleanup even after its generation is observed.
  val settled = not(db.exists) or db.paused or (not(db.deleting) and db.observed >= db.generation)
  val strandedCreate = db.exists and not(settled) and creator.pc == "done" and
    mutation.pc == "idle" and orchestration.pc == "idle" and cleanup.pc == "off"
  val strandedDelete = db.exists and db.deleting and not(db.paused) and
    creator.pc == "done" and mutation.pc == "done" and orchestration.pc == "idle" and cleanup.pc == "off"

  temporal fairProcesses = and {
    createStep.weakFair(state), mutationStep.weakFair(state),
    orchestrationStep.weakFair(state), cleanupStep.weakFair(state), maintain.weakFair(state),
  }
  temporal eventualProgress = fairProcesses implies always(eventually(settled))
  // Isolate Update's responsibility from Create's independently failing handoff.
  // A lost mutation reply must not erase this premise: the DB generation, not
  // the caller's remembered result, establishes that the write committed.
  temporal mutationProgress = fairProcesses implies
    always((db.generation == 2) implies eventually(settled))
  temporal deletionProgress = (fairProcesses and always(not(db.paused))) implies
    always(db.deleting implies eventually(not(db.exists)))
}

module current_create {
  import managed_resources(
    OPERATION = "none", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

module current_update {
  import managed_resources(
    OPERATION = "update", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

module current_delete {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

/// Proposed protocol, NOT the current Go implementation. Recovery extends the
/// Update pattern to Create/Delete, and the cleanup workflow retries durably.
/// Failed/ambiguous Delete attempts need the pinned revision settlement modeled
/// by aggregate.qnt too; merely deleting an error return is insufficient.
module corrected_create {
  import managed_resources(
    OPERATION = "none", RECOVER_CREATE_DELETE = true,
    GUARD_ORCHESTRATION_ENTRY = true, NOTIFY_COMPLETED_DELETE = true,
    RECOVER_CLEANUP = true, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

module corrected_update {
  import managed_resources(
    OPERATION = "update", RECOVER_CREATE_DELETE = true,
    GUARD_ORCHESTRATION_ENTRY = true, NOTIFY_COMPLETED_DELETE = true,
    RECOVER_CLEANUP = true, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

module corrected_delete {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = true,
    GUARD_ORCHESTRATION_ENTRY = true, NOTIFY_COMPLETED_DELETE = true,
    RECOVER_CLEANUP = true, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

/// Negative control: an attractive entry guard that forgets to retry the signal
/// loses progress for an already-waiting cleanup workflow.
module guard_without_notification {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = true,
    GUARD_ORCHESTRATION_ENTRY = true, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = true, MAINTENANCE = false, FAULT_BUDGET = 1
  ).*
}

module current_delete_with_maintenance {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = true, FAULT_BUDGET = 1
  ).*
}
