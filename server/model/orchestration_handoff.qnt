/// Compare a signal to the current execution with an instance-scoped request
/// to run reconciliation. Domain mutations and delivery are abstracted to
/// generation changes and observations. The publisher retries until accepted.
module orchestration_handoff {
  const RETAIN_PENDING_RUN: bool

  type Phase = Idle | Runnable | Loading | Reconciling | Exiting
  type Worker = { phase: Phase, snapshot: int, continued: bool }
  type State = {
    desired: int,
    observed: int,
    published: int,
    pending: bool,
    executionSignaled: bool,
    worker: Worker,
  }
  var state: State
  val worker = state.worker
  val observed = state.observed

  action init = state' = {
    desired: 1, observed: 0, published: 1, pending: true, executionSignaled: false,
    worker: { phase: Runnable, snapshot: 0, continued: false },
  }

  // Commit two further mutations at most. No fairness assumption requires
  // callers to make writes. Publication is a separate, durable caller step.
  action commitIntent = all {
    state.desired < 3,
    state' = state.with("desired", state.desired + 1),
  }

  action publishWork = all {
    state.published < state.desired,
    val idle = worker.phase == Idle
    state' = state.with("published", state.desired)
      .with("pending", state.pending or idle or RETAIN_PENDING_RUN)
      .with("executionSignaled", state.executionSignaled or (not(idle) and not(RETAIN_PENDING_RUN)))
      .with("worker", if (idle) worker.with("phase", Runnable) else worker),
  }

  // The workflow engine durably assigns pending work to a run before the
  // workflow reads the domain DB. A crash can resume at Loading. A publish
  // after the claim sets pending again, so it cannot be lost by that claim.
  action claimWork = all {
    worker.phase == Runnable,
    state' = state.with("pending", false).with("executionSignaled", false)
      .with("worker", worker.with("phase", Loading)),
  }

  action loadLatest = all {
    worker.phase == Loading,
    state' = state.with("worker", worker.with("phase", Reconciling).with("snapshot", state.desired)),
  }

  // PersistAndCompleteReconciliation compares with the current generation.
  // Even checking the execution's signal here leaves a window before exit.
  action completePass = all {
    worker.phase == Reconciling,
    state' = state.with("observed", worker.snapshot)
      .with("worker", worker.with("phase",
        if (worker.snapshot < state.desired or state.executionSignaled) Runnable else Exiting)),
  }

  // Backend completion serializes with publishWork for this logical instance.
  // If a request arrived after the last check, schedule the successor before
  // releasing the slot. Ordinary execution signals expire with the old run.
  action finishExecution = all {
    worker.phase == Exiting,
    state' = state.with("executionSignaled", false)
      .with("worker", worker.with("phase", if (state.pending) Runnable else Idle)),
  }

  // One continuation suffices to exercise survival of an unclaimed request
  // across replacement of the execution history. It preserves pending work.
  action continueExecution = all {
    worker.phase == Runnable, not(worker.continued),
    state' = state.with("executionSignaled", false)
      .with("worker", worker.with("continued", true)),
  }

  val publishedWorkHasOwner = worker.phase != Idle or observed >= state.published
  action workerStep = any { claimWork, loadLatest, completePass, finishExecution, continueExecution }
  action step = any { commitIntent, publishWork, workerStep, state' = state }

  temporal eventualProgress = (publishWork.weakFair(state) and workerStep.weakFair(state)) implies
    always(eventually(observed == state.desired))
}

module execution_signal_handoff {
  import orchestration_handoff(RETAIN_PENDING_RUN = false).*

  run accepted_signal_to_exiting_instance_loses_update = init
    .then(claimWork).then(loadLatest).then(completePass)
    .then(commitIntent).then(publishWork)
    .then(finishExecution)
    .expect(not(publishedWorkHasOwner))
}

module durable_pending_handoff {
  import orchestration_handoff(RETAIN_PENDING_RUN = true).*

  run wake_survives_exit_and_continue_as_new = init
    .then(claimWork).then(loadLatest).then(completePass)
    .then(commitIntent).then(publishWork)
    .then(finishExecution)
    .then(commitIntent).then(publishWork)
    .then(continueExecution)
    .expect(state.pending)
    .then(claimWork).then(loadLatest).then(completePass).then(finishExecution)
    .expect(observed == 3 and publishedWorkHasOwner)
}
