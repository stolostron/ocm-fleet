module managed_resources_test {
  import managed_resources(
    OPERATION = "delete", RECOVER_CREATE_DELETE = true,
    GUARD_ORCHESTRATION_ENTRY = true, NOTIFY_COMPLETED_DELETE = true,
    RECOVER_CLEANUP = true, MAINTENANCE = false, FAULT_BUDGET = 2
  ).* from "./managed_resources"

  run create_exhaustion_keeps_a_durable_owner = init
    .then(persistCreate).then(checkpointCreate).then(exhaustCreate)
    .expect(creator.pc == "persist" and creator.accepted and not(strandedCreate))
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .expect(settled and db.generation == 1)

  run ambiguous_delete_finishes_cleanup_even_with_a_conflict_response = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .then(requestMutation).then(mutate).then(loseMutationReply)
    .then(mutate).then(checkpointMutation).then(settleMutation)
    .then(exhaustMutation)
    .expect(mutation.outcome == "conflict" and mutation.pc == "check")
    .then(checkMutationProgress).then(startCleanup).then(checkpointCleanupStart)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(notifyCleanup).then(checkpointReconciliation).then(exitOrchestration)
    .then(checkCleanup).then(eraseResource).then(checkpointErasure)
    .expect(not(db.exists) and deletionIsSafe)

  run paused_entry_is_an_idle_exit = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(requestMutation).then(mutate).then(checkpointMutation).then(settleMutation)
    .then(checkMutationProgress).then(startCleanup).then(checkpointCleanupStart)
    .then(checkMutationProgress).then(startMutationOrchestration).then(checkpointMutationStart)
    .then(loadOrchestration).then(authFailure).then(persistReconciliation)
    .then(afterPersist).then(checkpointReconciliation).then(exitOrchestration)
    .then(startCreateOrchestration).then(checkpointCreateStart).then(loadOrchestration)
    .expect(db.paused and orchestration.pc == "exit" and noPausedEntry)

  run completed_delete_retries_notification_without_external_removal = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(requestMutation).then(mutate).then(checkpointMutation).then(settleMutation)
    .then(checkMutationProgress).then(startCleanup).then(checkpointCleanupStart)
    .then(checkCleanup)
    .expect(cleanup.pc == "wait")
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(exhaustReconciliation)
    .then(loadOrchestration)
    .expect(deletionReady and orchestration.pc == "notify" and noRepeatedRemoval)
    .then(notifyCleanup).then(checkpointReconciliation).then(exitOrchestration)
    .then(receiveCleanupSignal).then(exhaustCleanup)
    .expect(cleanup.pc == "erase")
    .then(eraseResource).then(checkpointErasure)
    .expect(not(db.exists) and deletionIsSafe)

  run signal_before_wait_is_buffered = init
    .then(persistCreate).then(checkpointCreate).then(createPreflight)
    .then(startCreateOrchestration).then(checkpointCreateStart)
    .then(requestMutation).then(mutate).then(checkpointMutation).then(settleMutation)
    .then(checkMutationProgress).then(startCleanup).then(checkpointCleanupStart)
    .then(checkCleanup)
    .then(loadOrchestration).then(completeEffects).then(persistReconciliation)
    .then(afterPersist).then(notifyCleanup)
    .then(receiveCleanupSignal).then(eraseResource).then(loseErasureReply)
    .then(eraseResource).then(checkpointErasure)
    .expect(not(db.exists) and deletionIsSafe)
}
