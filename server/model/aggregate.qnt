/// Transaction-level refinement of the repository assumptions used by the
/// workflow model. Two loaded copies can overlap, fail, finish, and arrive late.
/// SQL unique constraints/serialized transactions are atomic transitions here;
/// this model does not verify PostgreSQL or SQLite isolation implementations.
module aggregate {
  const FENCE_FAILURES: bool
  const REVISION_ON_LABELS: bool
  const PROTECT_NEWER_RESULT: bool
  const MONOTONE_COMPLETION: bool
  const CHECK_CLEANUP_ID: bool
  const RETAIN_REJECTIONS: bool

  pure val IDS = Set(1, 2)
  pure val CALLERS = Set(0, 1)
  pure val MAX_GENERATION = 4

  type Resource = {
    uid: int, revision: int, intentVersion: int,
    spec: int, derivedSpec: int, labels: int,
    generation: int, observed: int, deleting: bool, auth: int,
  }
  type Attempt = {
    pc: str, uid: int, revision: int, intentVersion: int,
    spec: int, labels: int, changeSpec: bool,
    succeeded: bool, lateCopy: bool, loadedSequence: int,
  }
  type Observation = { uid: int, generation: int, deleting: bool, auth: int }
  type CreateResult = { nonce: int, outcome: str, uid: int }
  type State = {
    resource: Resource,
    // -1 is undecided; 0 is rejected; a positive value pins an accepted UID.
    decisions: int -> int,
    callers: int -> int,
    decidedOnce: Set[int],
    nextUID: int,
    lastCreate: CreateResult,
    attempts: int -> Attempt,
    observation: Observation,
    initialPassRecorded: bool,
    highestObserved: int,
    retiring: Set[int],
    retired: Set[int],
    expectedAuth: int,
    // Ghost sequence advances on every user write, independently of the
    // implementation's revision. It detects writes from obsolete copies.
    writeSequence: int,
    staleWrite: bool,
    lateWrite: bool,
    nonceReused: bool,
    wrongCleanup: bool,
  }

  var state: State
  val resource = state.resource
  val attempts = state.attempts
  pure val emptyResource: Resource = {
    uid: 0, revision: 0, intentVersion: 0, spec: 0, derivedSpec: 0,
    labels: 0, generation: 0, observed: 0, deleting: false, auth: 0,
  }
  pure val emptyAttempt: Attempt = {
    pc: "idle", uid: 0, revision: 0, intentVersion: 0, spec: 0,
    labels: 0, changeSpec: false, succeeded: false, lateCopy: false, loadedSequence: 0,
  }
  pure def maximum(a: int, b: int): int = if (a > b) a else b

  action init = state' = {
    resource: emptyResource,
    decisions: IDS.mapBy(_ => -1), callers: IDS.mapBy(_ => -1),
    decidedOnce: Set(), nextUID: 1,
    lastCreate: { nonce: 0, outcome: "none", uid: 0 },
    attempts: IDS.mapBy(_ => emptyAttempt),
    observation: { uid: 0, generation: 0, deleting: false, auth: 0 },
    initialPassRecorded: false, highestObserved: 0,
    retiring: Set(), retired: Set(), expectedAuth: 0, writeSequence: 0,
    staleWrite: false, lateWrite: false, nonceReused: false, wrongCleanup: false,
  }

  /// Constructor captures the nonce/scope. Repository Create arbitrates the
  /// name and nonce and commits its decision with the accepted aggregate and
  /// fulfillment. A rejected attempt commits no provisional fulfillment.
  action create(nonce: int, caller: int): bool = all {
    IDS.contains(nonce), CALLERS.contains(caller),
    state.decisions.get(nonce) != -1 or resource.uid != 0 or state.nextUID <= 2,
    val decision = state.decisions.get(nonce)
    val result = if (decision != -1) {
      nonce: nonce,
      outcome: if (state.callers.get(nonce) != caller) "invalidScope"
        else if (decision == 0) "alreadyExists"
        else if (decision != resource.uid) "retired" else "accepted",
      uid: if (decision > 0 and decision == resource.uid and state.callers.get(nonce) == caller) decision else 0,
    } else {
      nonce: nonce, outcome: if (resource.uid == 0) "accepted" else "alreadyExists",
      uid: if (resource.uid == 0) state.nextUID else 0,
    }
    state' = if (decision != -1) state.with("lastCreate", result)
    else if (resource.uid != 0)
      state.with("lastCreate", result).with("decidedOnce", state.decidedOnce.union(Set(nonce)))
        .with("decisions", if (RETAIN_REJECTIONS) state.decisions.set(nonce, 0) else state.decisions)
        .with("callers", if (RETAIN_REJECTIONS) state.callers.set(nonce, caller) else state.callers)
    else
      state.with("lastCreate", result)
        .with("resource", emptyResource.with("uid", state.nextUID).with("revision", 1)
          .with("intentVersion", 1).with("generation", 1))
        .with("decisions", state.decisions.set(nonce, state.nextUID))
        .with("callers", state.callers.set(nonce, caller))
        .with("nonceReused", state.nonceReused or state.decidedOnce.contains(nonce))
        .with("decidedOnce", state.decidedOnce.union(Set(nonce)))
        .with("nextUID", state.nextUID + 1).with("expectedAuth", 0).with("writeSequence", 0).with("highestObserved", 0),
  }

  /// Loading two copies before either saves is intentional. Each copy changes
  /// either spec or labels; the repository always saves the loaded labels too.
  /// A combined spec+labels mutation coalesces its root revision the same way.
  action prepare(worker: int, specChange: bool): bool = all {
    IDS.contains(worker), attempts.get(worker).pc == "idle", resource.uid != 0, not(resource.deleting),
    state' = state.with("attempts", attempts.set(worker, {
      pc: "live", uid: resource.uid, revision: resource.revision,
      intentVersion: resource.intentVersion,
      spec: if (specChange) 1 - resource.spec else resource.spec,
      labels: if (specChange) resource.labels else 1 - resource.labels,
      changeSpec: specChange, succeeded: false, lateCopy: true, loadedSequence: state.writeSequence,
    })),
  }

  def canWrite(a: Attempt): bool = and {
    a.uid == resource.uid, resource.uid != 0,
    a.revision == resource.revision, not(resource.deleting),
    not(a.changeSpec) or resource.generation < MAX_GENERATION,
  }

  /// The revision compare-and-swap and all intent/strategy writes commit in one
  /// transaction. Live activities additionally check their pinned etag; omitting
  /// that stricter check here overapproximates saves at the repository boundary.
  def saving(worker: int, late: bool): State = {
    val a = attempts.get(worker)
    val allowed = canWrite(a)
    val updatedAttempt = if (late) a.with("lateCopy", false)
      else a.with("pc", "resolved").with("succeeded", allowed)
    val nextResource = if (not(allowed)) resource else resource
      .with("revision", a.revision + (if (a.changeSpec or REVISION_ON_LABELS) 1 else 0))
      .with("labels", a.labels)
      .with("spec", if (a.changeSpec) a.spec else resource.spec)
      .with("derivedSpec", if (a.changeSpec) a.spec else resource.derivedSpec)
      .with("intentVersion", if (a.changeSpec) a.intentVersion + 1 else resource.intentVersion)
      .with("generation", resource.generation + (if (a.changeSpec) 1 else 0))
      .with("auth", if (a.changeSpec) worker else resource.auth)
    state.with("attempts", attempts.set(worker, updatedAttempt)).with("resource", nextResource)
      .with("expectedAuth", if (allowed and a.changeSpec) worker else state.expectedAuth)
      .with("writeSequence", state.writeSequence + (if (allowed) 1 else 0))
      .with("staleWrite", state.staleWrite or (allowed and a.loadedSequence != state.writeSequence))
      .with("lateWrite", state.lateWrite or (allowed and a.pc == "finished"))
  }

  action save(worker: int): bool = all {
    IDS.contains(worker), attempts.get(worker).pc == "live",
    state' = saving(worker, false),
  }

  /// An activity error does not prove an older executing copy cannot commit.
  /// Settlement below must serialize with that copy at the repository fence.
  action rejectActivity(worker: int): bool = all {
    IDS.contains(worker), attempts.get(worker).pc == "live",
    state' = state.with("attempts", attempts.set(worker, attempts.get(worker).with("pc", "resolved"))),
  }

  action settle(worker: int): bool = all {
    IDS.contains(worker), attempts.get(worker).pc == "resolved",
    val a = attempts.get(worker)
    val fence = FENCE_FAILURES and not(a.succeeded) and a.uid == resource.uid and a.revision == resource.revision
    state' = state
      .with("resource", if (fence) resource.with("revision", resource.revision + 1) else resource)
      .with("attempts", attempts.set(worker, a.with("pc", "finished"))),
  }

  action delayedSave(worker: int): bool = all {
    IDS.contains(worker), attempts.get(worker).lateCopy,
    state' = saving(worker, true),
  }

  /// Delete changes fulfillment generation/lifecycle/auth, not ER intent or
  /// revision. This is why none of these version counters can be conflated.
  action markDeleting = all {
    resource.uid != 0, not(resource.deleting), resource.generation < MAX_GENERATION,
    state' = state.with("resource", resource.with("deleting", true)
      .with("generation", resource.generation + 1).with("auth", 3))
      .with("retiring", state.retiring.union(Set(resource.uid))).with("expectedAuth", 3),
  }

  action loadReconciliation = all {
    resource.uid != 0,
    state' = state.with("initialPassRecorded", state.initialPassRecorded or (resource.uid == 1 and resource.generation == 1))
      .with("observation", {
      uid: resource.uid, generation: resource.generation, deleting: resource.deleting, auth: resource.auth,
    }),
  }

  def merging(o: Observation): State = {
    val sameIncarnation = o.uid == resource.uid
    val applies = o.generation >= resource.observed and
      (o.generation == resource.generation or not(PROTECT_NEWER_RESULT))
    // CompleteReconciliation runs even when MergeReconciliationResult ignores
    // an obsolete observation; its own high-water mark must remain monotonic.
    val observed = if (MONOTONE_COMPLETION) maximum(resource.observed, o.generation) else o.generation
    state.with("resource", if (not(sameIncarnation)) resource else resource
      .with("observed", observed)
      .with("deleting", if (applies) o.deleting else resource.deleting)
      .with("auth", if (applies) o.auth else resource.auth))
      .with("highestObserved", if (sameIncarnation) maximum(state.highestObserved, o.generation) else state.highestObserved)
  }

  action mergeReconciliation = all {
    state.observation.uid != 0,
    state' = merging(state.observation),
  }

  /// Retain one generation-1 activity result independently of the newer pass.
  /// Its replay can arrive after generation 2 was observed or after name reuse.
  action replayInitialReconciliation = all {
    state.initialPassRecorded,
    state' = merging({ uid: 1, generation: 1, deleting: false, auth: 0 }),
  }

  /// Readiness and notification are refined by managed_resources.qnt. Once
  /// proved, the cleanup transaction removes the original UID/FID atomically.
  action hardDelete = all {
    resource.uid != 0, resource.deleting, resource.observed >= resource.generation,
    state' = state.with("resource", emptyResource).with("retired", state.retired.union(Set(resource.uid))),
  }

  action replayCleanup(uid: int): bool = all {
    state.retired.contains(uid),
    val erase = resource.uid != 0 and (not(CHECK_CLEANUP_ID) or resource.uid == uid)
    state' = state.with("resource", if (erase) emptyResource else resource)
      .with("wrongCleanup", state.wrongCleanup or (erase and resource.uid != uid)),
  }

  action stutter = state' = state
  action step = any {
    nondet nonce = IDS.oneOf()
    nondet caller = CALLERS.oneOf()
    create(nonce, caller),
    nondet worker = IDS.oneOf()
    nondet specChange = Set(false, true).oneOf()
    prepare(worker, specChange),
    nondet worker = IDS.oneOf()
    save(worker),
    nondet worker = IDS.oneOf()
    rejectActivity(worker),
    nondet worker = IDS.oneOf()
    settle(worker),
    nondet worker = IDS.oneOf()
    delayedSave(worker),
    markDeleting, loadReconciliation, mergeReconciliation, replayInitialReconciliation, hardDelete,
    nondet uid = IDS.oneOf()
    replayCleanup(uid),
    stutter,
  }

  val revisionsFenceWrites = not(state.staleWrite) and not(state.lateWrite)
  val creationDecisionsAreStable = not(state.nonceReused)
  val replacementIsSafe = not(state.wrongCleanup) and not(state.retired.contains(resource.uid))
  val newerDecisionsSurvive = resource.uid == 0 or
    (resource.auth == state.expectedAuth and (not(state.retiring.contains(resource.uid)) or resource.deleting))
  val intentAndStrategiesAgree = resource.spec == resource.derivedSpec
  val observationIsMonotone = resource.uid == 0 or resource.observed == state.highestObserved
  val safety = and {
    revisionsFenceWrites, creationDecisionsAreStable, replacementIsSafe,
    newerDecisionsSurvive, intentAndStrategiesAgree, observationIsMonotone,
    resource.observed <= resource.generation, resource.generation <= MAX_GENERATION,
  }
}

module current_aggregate {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).*
}

module without_fencing {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = false, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).*
}

module without_label_revision {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = false, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).*
}

module without_result_merge_guard {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = false,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).*
}

module without_cleanup_identity {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = false, RETAIN_REJECTIONS = true
  ).*
}

module without_rejected_nonce {
  import aggregate(
    MONOTONE_COMPLETION = true,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = false
  ).*
}

module without_observation_monotonicity {
  import aggregate(
    MONOTONE_COMPLETION = false,
    FENCE_FAILURES = true, REVISION_ON_LABELS = true, PROTECT_NEWER_RESULT = true,
    CHECK_CLEANUP_ID = true, RETAIN_REJECTIONS = true
  ).*
}
