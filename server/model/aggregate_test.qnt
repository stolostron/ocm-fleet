module aggregate_test {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).* from "./aggregate"

  run labels_advance_root_revision_without_new_intent_or_generation = init
    .then(create(1, 0)).then(prepare(1, false)).then(save(1))
    .expect(resource.revision == 2 and resource.intentVersion == 1 and resource.generation == 1)
    .then(delayedSave(1)).expect(safety)

  run spec_advances_intent_and_derived_generation_atomically = init
    .then(create(1, 0)).then(prepare(1, true)).then(save(1))
    .expect(resource.revision == 2 and resource.intentVersion == 2 and resource.generation == 2)
    .expect(intentAndStrategiesAgree)

  run concurrent_loaded_copies_cannot_lose_a_label_edit = init
    .then(create(1, 0)).then(prepare(1, false)).then(prepare(2, true))
    .then(save(1)).then(save(2))
    .expect(not(attempts.get(2).succeeded) and resource.labels == 1 and revisionsFenceWrites)

  run settlement_prevents_a_delayed_commit = init
    .then(create(1, 0)).then(prepare(1, true)).then(rejectActivity(1)).then(settle(1))
    .then(delayedSave(1)).expect(resource.generation == 1 and safety)

  run commit_before_fence_is_found_by_the_progress_read = init
    .then(create(1, 0)).then(prepare(1, true)).then(rejectActivity(1))
    .then(delayedSave(1)).then(settle(1))
    .expect(resource.generation == 2 and resource.revision == 2 and safety)

  run late_observation_preserves_new_delete_and_credentials = init
    .then(create(1, 0)).then(loadReconciliation).then(markDeleting).then(mergeReconciliation)
    .expect(resource.deleting and resource.auth == 3 and resource.observed == 1 and safety)

  run delete_changes_generation_without_new_root_revision_or_intent = init
    .then(create(1, 0)).then(markDeleting)
    .expect(resource.revision == 1 and resource.intentVersion == 1 and resource.generation == 2)

  run old_completion_replay_cannot_regress_observation = init
    .then(create(1, 0)).then(loadReconciliation).then(mergeReconciliation)
    .then(markDeleting).then(loadReconciliation).then(mergeReconciliation)
    .then(replayInitialReconciliation)
    .expect(resource.observed == 2 and observationIsMonotone and safety)

  run rejected_nonce_stays_rejected_after_name_becomes_free = init
    .then(create(1, 0)).then(create(2, 0))
    .expect(state.lastCreate.outcome == "alreadyExists")
    .then(markDeleting).then(loadReconciliation).then(mergeReconciliation).then(hardDelete)
    .then(create(2, 0)).expect(state.lastCreate.outcome == "alreadyExists" and resource.uid == 0)

  run accepted_nonce_does_not_bind_to_a_replacement = init
    .then(create(1, 0)).then(prepare(1, true))
    .then(markDeleting).then(loadReconciliation).then(mergeReconciliation).then(hardDelete)
    .then(create(2, 0)).then(create(1, 0)).then(replayCleanup(1)).then(delayedSave(1))
    .expect(state.lastCreate.outcome == "retired" and resource.uid == 2 and safety)

  run creation_scope_includes_the_caller = init
    .then(create(1, 0)).then(create(1, 1))
    .expect(state.lastCreate.outcome == "invalidScope" and resource.uid == 1 and safety)
}
