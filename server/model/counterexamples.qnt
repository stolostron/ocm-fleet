module create_counterexamples {
  import managed_resources(
    OPERATION = "none", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).* from "./managed_resources"

  run exhausted_create_handoff_strands_committed_intent = init
    .then(persistCreate).then(checkpointCreate)
    .then(exhaustCreate)
    .expect(strandedCreate)
    .then(stutter).expect(not(settled))

  /// This is NOT a counterexample: the creation decision repairs a lost reply.
  run lost_create_commit_reply_replays_acceptance = init
    .then(persistCreate).then(loseCreateReply)
    .then(persistCreate).then(checkpointCreate)
    .expect(db.generation == 1 and creator.pc == "preflight")
}

module delete_counterexamples {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).* from "./managed_resources"

  // Hold the Create handoff until after deletion, so it cannot accidentally
  // repair the missing owner in this particular execution.
  run lost_delete_commit_reply_exits_on_stale_etag = init
    .then(persistCreate).then(checkpointCreate)
    .then(requestMutation).then(mutate).then(loseMutationReply)
    .then(mutate).then(checkpointMutation)
    .then(createPreflight).then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist)
    .expect(deletionReady and mutation.outcome == "conflict" and cleanup.pc == "off")
    .then(missingCleanupListener)
    .expect(not(settled))

  run failed_cleanup_activity_has_no_durable_retry_owner = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .then(requestMutation).then(mutate).then(checkpointMutation)
    .then(startCleanup).then(checkpointCleanupStart)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(notifyCleanup).then(checkpointReconciliation).then(exitOrchestration)
    .then(checkCleanup).then(exhaustCleanup)
    .expect(strandedDelete and deletionReady)

  /// The Create preflight passed before another workflow paused the resource.
  /// Its delayed Start runs after that workflow has released the backend ID.
  run delayed_create_start_enters_a_paused_fulfillment = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(requestMutation).then(mutate).then(checkpointMutation)
    .then(startCleanup).then(checkpointCleanupStart)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(authFailure).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .expect(db.paused)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration)
    .expect(not(noPausedEntry))

  run failed_completion_signal_reexecutes_completed_removal = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(requestMutation).then(mutate).then(checkpointMutation)
    .then(startCleanup).then(checkpointCleanupStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(exhaustReconciliation)
    .expect(deletionReady)
    .then(loadOrchestration).then(completeEffects)
    .expect(not(noRepeatedRemoval))
}

module update_regressions {
  import managed_resources(
    OPERATION = "update", RECOVER_CREATE_DELETE = false,
    GUARD_ORCHESTRATION_ENTRY = false, NOTIFY_COMPLETED_DELETE = false,
    RECOVER_CLEANUP = false, MAINTENANCE = false, FAULT_BUDGET = 1
  ).* from "./managed_resources"

  run lost_update_commit_reply_still_hands_off_committed_work = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .then(requestMutation).then(mutate).then(loseMutationReply)
    .then(mutate).then(checkpointMutation).then(settleMutation)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .expect(mutation.outcome == "conflict" and db.observed == 2 and settled)

  run already_running_during_exit_keeps_the_update_responsible = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation)
    .expect(orchestration.pc == "exit")
    .then(requestMutation).then(mutate).then(checkpointMutation).then(settleMutation)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .expect(mutation.pc == "check" and db.observed == 1)
    .then(exitOrchestration)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .expect(settled and db.observed == 2)
}
