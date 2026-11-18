/// Two concurrent starts with one instance ID and distinct execution IDs.
/// Only the active-instance admission transaction is modeled.
module workflow_instances {
  const UNIQUE_ACTIVE_INSTANCE: bool
  pure val CALLERS = Set(1, 2)
  type Start = NotStarted | NoActiveRow | Active | AlreadyRunning
  var calls: int -> Start

  action init = calls' = CALLERS.mapBy(_ => NotStarted)
  val activeExecutions = CALLERS.filter(caller => calls.get(caller) == Active)
  val oneActiveExecution = activeExecutions.size() <= 1

  // go-workflows v1.4.1, postgres.createInstance: SELECT for an active
  // instance under READ COMMITTED. This does not reserve an absent row.
  action checkActiveInstance(caller: int): bool = all {
    calls.get(caller) == NotStarted,
    calls' = calls.set(caller, if (activeExecutions.size() == 0) NoActiveRow else AlreadyRunning),
  }

  // The current unique index includes execution_id. The proposed variant
  // also checks uniqueness over active rows for the logical instance ID.
  action insertExecution(caller: int): bool = all {
    calls.get(caller) == NoActiveRow,
    calls' = calls.set(caller,
      if (UNIQUE_ACTIVE_INSTANCE and activeExecutions.size() > 0) AlreadyRunning else Active),
  }

  action step = any {
    nondet caller = CALLERS.oneOf()
    checkActiveInstance(caller),
    nondet caller = CALLERS.oneOf()
    insertExecution(caller),
    calls' = calls,
  }
}

module current_postgres_instances {
  import workflow_instances(UNIQUE_ACTIVE_INSTANCE = false).*

  run two_concurrent_starts_violate_the_instance_slot = init
    .then(checkActiveInstance(1)).then(checkActiveInstance(2))
    .then(insertExecution(1)).then(insertExecution(2))
    .expect(not(oneActiveExecution))
}

module unique_active_instances {
  import workflow_instances(UNIQUE_ACTIVE_INSTANCE = true).*

  run active_uniqueness_arbitrates_at_insert = init
    .then(checkActiveInstance(1)).then(checkActiveInstance(2))
    .then(insertExecution(1)).then(insertExecution(2))
    .expect(oneActiveExecution and calls.get(1) == Active and calls.get(2) == AlreadyRunning)
}
