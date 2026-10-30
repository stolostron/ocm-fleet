# OCM-io × OCM Fleet Shim — Analysis

Status: **background and rationale**. This document is not normative. It explains the two systems, why Fleet is being introduced, and what the OCM-io APIs actually provide. The requirements are in [requirements.md](requirements.md); declared gaps and coverage are in [coverage.md](coverage.md); candidate mechanisms are in [baseline-hypotheses.md](baseline-hypotheses.md). Where this document and the requirements disagree, the requirements win.

---

## 1. The two systems at a glance

| Dimension | OCM-io | OCM Fleet | Consequence for the shim |
|---|---|---|---|
| Unit of desired state | `ManifestWork`, one bundle per cluster, optionally fanned out by `ManifestWorkReplicaSet` | Fulfillment / Deployment intent, fanned out to many targets | One Fleet intent is N OCM-io objects; the unit of intent differs |
| Delivery | Spoke `work-agent` applies manifests; hub does not apply | A Fleet addon delivery agent (through a fleetlet, or platform-side), with generation-tagged deliveries and stale-result fencing | Two possible delivery paths to one cluster; the fleetlet is transport and shared services, not the delivery agent, and it is part of the shim where installed; the platform-side path is a declared variant |
| Provenance | No authorization evidence for a delivery. The work agent records applied resources, not a signer or content digest; `Applied` is an apply result. | Signed intent, exact-content binding, and verification at the platform side or, by design, at the target through a fleetlet; the signed intent is persisted, but there is no evidence log or durable per-delivery result. The resource manager carries evidence but is not its author. | Fleet provenance ends at a verifier; the OCM-io work agent is not one, so an OCM-io delivery path is downgraded (PA-3, GAP-13) |
| Source of truth | Kubernetes API, frequently written by GitOps or operators | Resource manager persistence. A GitOps controller writes OCM-io objects, not Fleet intent; a commit signature is independent unless a profile binds it. | GitOps may be authoritative for what it applies; the shim observes and never becomes a second writer (GO-1, GO-2, GAP-16) |
| Identity | Group/version/kind + namespace/name + Kubernetes UID | Full resource name `//{service}/{collection}/{id}`, deliberately no UID | UID cannot be the correlation key |
| Status | `ManifestWork.Status` conditions and per-manifest status; `Available` means the object exists, health only where condition rules are supplied; addon conditions | Deployment lifecycle, delivery conditions, inventory observations | Both a translation and a merge problem, and the words do not mean what they appear to mean |
| Cleanup memory | `AppliedManifestWork` on the spoke: work name, hub hash, agent ID, applied resource identities, eviction start; no per-apply timestamp and no component version | Journal, retained deliveries, tombstones on the platform | Neither side can be the other's cleanup record; a spoke-side shim component reads the local ledger and supplies the missing version and time itself |
| Membership | `ManagedCluster` + `Placement`; the hub cluster itself may be registered as a `ManagedCluster` | Target pool + placement strategy; targets are created by a Fleet addon at connect or by a delivery output, and there is no target-create, relabel, or deregister API; the label selector matches exact values only | Two placement authorities; claims must be projected into labels to be selectable; taints and tolerations have no Fleet equivalent; a self-registered hub is a target, not a special case |
| Tenancy | Hub RBAC; no tenant or workspace concept | Provider tenant, consumer tenants, workspace tree (design; not yet implemented — GAP-24) | A partition on one side that does not exist on the other |
| Extensibility | CRDs + addon-framework + ManifestWork | Dynamic proto extension services, addon capabilities | Different extension mechanisms; the shim does not map Fleet's addon or extension framework to OCM-io (scope row 16, G-15) |
| Observation | Feedback rules (JSONPath values, CEL condition rules), 4-minute resync | Inventory reports: properties, observations, conditions | Hub-side fidelity is bounded by OCM-io's feedback surface; a spoke-side shim component reached through the fleetlet can observe more and must report what it adds |
| Transport | Hub Kubernetes API, or CloudEvents drivers | Fleetlet channels, pluggable platform transport | The OCM-io path may be the only one available to some targets |
| Hosting | Klusterlet and addon agents, hub API | Containers, Nx, Podman/OpenShift, optionally addon-packaged | The shim may itself be an OCM-io addon, including on a self-registered hub |

**Placement surfaces are not equivalent.** OCM-io `Placement` filters on labels, claims, cluster sets, CEL predicates, and cluster taints, and can score candidates by allocatable CPU or memory when configured. Taints work through tolerations rather than labels: `NoSelect` removes a matching cluster, including one already selected; `NoSelectIfNew` excludes a cluster only while it is not already in the decision; `PreferNoSelect` is defined to deprioritize, but the v1.4.x placement plugin treats it as tolerated and no scoring plugin implements the preference, so it does not change selection today. A `NoSelect` taint empties the decision, a matching `Placement.spec.tolerations` entry restores it, and a placement created while the cluster carries `NoSelectIfNew` stays empty. Fleet placement accepts exact label equalities only — `TargetSelector.match_labels` is a conjunction of `key=value` pairs, with no match expressions — so none of this survives translation: a projected label can record that a cluster is tainted, but cannot express "select it only if the placement tolerates the taint" (M-6, GAP-11). Even an OCM-io label selector using `matchExpressions` has no Fleet equivalent: Fleet cannot test existence, inequality, or set membership.

---

## 2. Why introduce Fleet to an existing OCM-io installation

The users already run OCM-io. Their clusters are registered, their addons are enabled, their placements are configured, and their workloads are delivered through `ManifestWork`. Fleet does not replace any of that. It is introduced because it adds capabilities OCM-io does not provide, while OCM-io keeps doing what it already does.

The benefits divide into two classes by dependency on OCM-io facts:

- **Fleet-native** — available by running Fleet, with no OCM-io integration. New work can use them without touching the incumbent installation.
- **OCM-io-dependent** — require facts only OCM-io holds: registration, membership, addon enablement, placement decisions, or workload state. These are what the shim exists for, and they are bounded by the approved scope in requirements §4.

The table's **Shim role** column says what the shim must supply for each benefit: `None`, `Facts`, or `Delivery`.

Maturity labels: **Implemented** (present in the OCM Fleet prototype), **Partial** (part of the path exists), **Design** (specified in OCM Fleet's `docs/design`, not built), **POC** (validated in OCM Fleet's `poc/`, not integrated).

| # | Benefit | What it means for an existing OCM-io user | Shim role | Maturity |
|---|---|---|---|---|
| B-1 | **OCM-io stays compatible** | No fork, no required CRD or controller change, no manifest rewrite, no forced upgrade. General upstream enhancements are optional accelerators, never prerequisites. | None — this is the constraint | Constraint |
| B-2 | **A separate control plane with its own persistence** | Fleet intent and history live in the resource manager's SQLite/Postgres, not in hub etcd or as CRDs. Hub API and etcd load do not grow with Fleet's data model. | None | Implemented |
| B-3 | **Intent, placement, and rollout decoupled** | Keep a manifest set stable while changing placement; reuse one intent across pools; stage delivery instead of maintaining one bundle per cluster. | None for work Fleet delivers | Partial |
| B-4 | **Durable, replayable orchestration** | Create, delete, and resume survive process restarts and partitions; changing intent requires delete and recreate because Fleet has no update surface, and effects are idempotent and replayed, not dependent on periodic requeue. | None for work Fleet delivers | Implemented (Fleet operations) |
| B-5 | **Generation-fenced delivery** | A stale apply cannot overwrite newer intent; accepted versus delivered is explicit; removal is idempotent and journaled. | Delivery | Design |
| B-6 | **Targets beyond Kubernetes clusters** | Platform, local, and Fleet addon-registered targets use the same model; provisioning can itself be a deployment. | None — Fleet-native; OCM-io inventory adds Kubernetes-cluster targets only | Partial |
| B-7 | **Fleet-wide inventory and query** | One CEL query across all targets instead of per-cluster round trips; observations, properties, and conditions are uniformly queryable. | Facts | Partial |
| B-8 | **A uniform health model** | Platform-defined and Fleet addon-defined condition types, with transition history, across heterogeneous resources. | Facts when sourced from OCM-io | Partial |
| B-9 | **Native multi-tenancy** | Consumer tenants and workspaces: give teams access to their targets without giving them the hub or its kubeconfig. | Facts — to scope existing clusters into workspaces | Design |
| B-10 | **Per-delivery authorization and provenance** | Authorize each delivery by credential presentation and provenance instead of distributing hub credentials; produce attestations that outlive the hub session. | Delivery | Partial |
| B-11 | **Fleet addon-defined typed APIs without new CRDs** | A Fleet addon ships protos and gets a typed API compiled and served at connect time; no CRD per integration, atomic schema swap. | None | Implemented |
| B-12 | **Managed resources as consumer nouns** | Typed collections (`/clusters`, `/monitoring-stacks`) with versioned intent, lifecycle state, conditions, and inventory, instead of users reasoning about fulfillments. | None | Design |
| B-13 | **Recursive platforms and federation** | Platforms of platforms; on-demand fan-out with merge hints instead of a persistent global aggregate; provisioning and bootstrap/pivot as defined transitions. | None | Design / POC |
| B-14 | **CLI and console surface** | `fleetctl` and a console shell with a build-generated dynamic-plugin registry; an OpenShift dynamic-plugin-compatible extension model, though Fleet addon-defined UI and CLI plugins are design-only (scope row 16). | None | Implemented (prototype); plugin capability design-only |

Four consequences follow directly from this table:

- Fleet-native benefits apply to **work Fleet delivers**. They do not change how OCM-io delivers existing workloads. A user who adopts Fleet only for new work gets them without the shim at all.
- The shim's contribution is narrow: it unlocks the OCM-io-dependent benefits for aspects admitted by requirements §4. It does not make Fleet-native benefits apply to OCM-io delivery.
- An inventory-only shim still delivers B-1 and registers clusters introduced through Fleet into OCM-io so the cluster set is visible from both sides (F-1). It makes adopted cluster labels available to Fleet placement across one target pool — the cross-target selection part of B-7, not a CEL query, because Fleet's query surface covers extension resources, not targets. Fleet's CLI and console remain unchanged as Fleet-native surfaces over Fleet's own objects (B-14), with Fleet addon-defined UI and CLI plugins design-only (scope row 16). It adds no non-Kubernetes targets: OCM-io inventory yields Kubernetes clusters only, and Fleet-native platform and local targets are unchanged (B-6). B-9 is design-dependent: it becomes claimable only when Fleet's workspace partition is implemented (GAP-24). It does not deliver B-8 (cluster health is a separate aspect), B-3, B-4, B-5, or B-10 for OCM-io-delivered workloads, because those require workload intent or delivery to be in scope.
- Fleet's own addon and extension framework (B-11) and its CLI and console (B-14) are Fleet-native. B-12, the managed-resource consumer surface built on B-11, and B-13, recursive platforms, are design-only and are not claimed. The shim does not map any of this to OCM-io's addon-framework or to any OCM-io object (scope row 16, G-15).

No benefit justifies violating C-1. A benefit that requires an upstream change is out of scope unless the change is a general enhancement under C-15 through C-20, and the benefit MUST also exist on the baseline with its degradation declared (C-16, C-17).

### 2.1 Which benefits a deployment mode can provide

This table is the honest per-mode view required by C-16. "Inventory-only" is the confirmed first increment; "Delivery" requires workload intent in scope and a delivery path whose verification locus is declared. "Fleet-native" means the benefit does not depend on OCM-io facts at all.

| Benefit | Inventory-only | Delivery | Notes |
|---|---|---|---|
| B-1 | Yes | Yes | The constraint itself |
| B-2 | Yes | Yes | Fleet-native |
| B-3 | — | Yes (Fleet-delivered work) | Not for OCM-io-delivered work |
| B-4 | — | Yes (Fleet-delivered work) | Not for OCM-io-delivered work |
| B-5 | — | Yes | Requires a Fleet delivery path; cleanup is journaled Fleet-side |
| B-6 | Yes — Fleet-native; OCM-io adds Kubernetes-cluster targets only | Yes — same | Non-Kubernetes targets come from Fleet itself, not from OCM-io inventory |
| B-7 | Partial — adopted labels drive placement across the target pool; no CEL query over targets | Partial — no target query; delivery adds Fleet-native objects, not queryable cluster facts | Fleet's query surface covers extension resources, not targets, in every mode |
| B-8 | — | — unless cluster health is admitted | Needs cluster conditions (scope row 2); delivery scope alone does not supply them |
| B-9 | Design only | Design only | Fleet's workspace partition is a design goal, not an implemented baseline; until it exists, cluster visibility is provider-wide and the shim must not claim tenant isolation (GAP-24) |
| B-10 | — | Yes, with a signed deployment and a verifier | Requires a caller signature and a target- or platform-side verifier; an unsigned deployment carries no evidence, and the OCM-io work-agent path is downgraded (GAP-13). Per-delivery state is reported as partial where Fleet exposes no durable delivery result (GAP-21). |
| B-11 | Yes | Yes | Fleet-native |
| B-12 | Design only | Design only | Fleet's managed-resource consumer surface is design-only; not claimable in the baseline |
| B-13 | Design only | Design only | Recursive platforms and federation are design/POC; not claimable in the baseline |
| B-14 | Yes | Yes | Fleet-native |

A capability that appears only in the Delivery column MUST NOT be used as the reason to adopt the inventory-only increment (AD-6). The per-direction stories that show what each mode does for a user are in [use-cases.md](use-cases.md).

GitOps compatibility is not a benefit column, because it is a property every mode preserves rather than a capability one mode adds: the shim never writes a field an applier owns, and a delivery that would collide fails with a named conflict instead of forcing the field (GO-1, GO-2). The applier's reconciliation remains the operative desired state whether the shim is in inventory-only or delivery scope.

---

## 3. The ManifestWork family in context

ManifestWork is the only desired-state primitive the klusterlet work-agent acts on. Addons — including the OCM-io addon that carries fleetlet — reach clusters as ManifestWorks. Its status is frequently the only hub-visible evidence that anything converged on a spoke. Its finalizer plus `AppliedManifestWork` define the cleanup contract that any deleter must respect.

`ManifestWorkReplicaSet` is structurally the closest OCM-io analogue to a Fulfillment: one intent bundle, one placement reference, fan-out to many clusters, aggregated status. The resemblance is real but shallow. Treating the two as equivalent is the most likely integration error, so this section separates what is similar from what is not.

Nothing here changes the scope decision. Workload delivery remains excluded by default. The section records the contract so that decision can be made with the real semantics in view, and so the requirements are already stated if the scope is later expanded.

### 3.1 What ManifestWork guarantees

As of OCM-io v1.4.x, the behavior below follows the source paths named. Field spellings vary by version; the semantics do not.

- **Placement in the API.** A `ManifestWork` is namespaced on the hub, and the namespace names the target cluster. The work-agent watches its own namespace on the hub; it does not apply a spoke-local copy of the object. One work object carries a list of raw manifests.
- **Per-manifest configuration.** `spec.manifestConfigs` selects manifests by resource identity and attaches an update strategy (`Update`, `CreateOnly`, `ReadOnly`, `ServerSideApply`) and feedback rules. Feedback rules choose which fields are reported back. They do not decide whether `Available` is reported.
- **`Applied` means no apply error.** The condition is computed from the apply attempt. A server-side-apply conflict sets `Applied=False` with the conflicting field manager in the message; retrying does not turn it into success while the other manager owns the field, and it is not a transport failure. Conditions are recorded per manifest and aggregated to the work: one failed manifest turns the work-level `Applied` false, while the work-level `Available` can remain true from earlier successful applies.
- **`Available` means the object exists.** The work-agent sets `Available=True` when a read of the resource succeeds and `False` when the resource is absent (`pkg/work/spoke/objectreader`). A Deployment with zero ready replicas is `Available=True`. A missing object is `Available=False`, which is not the same as unhealthy.
- **Health and completion are opt-in.** `Progressing`, `Degraded`, and `Complete` appear only where the work supplies condition rules. `WellKnownConditions` recognizes only Job and Pod `Complete`; every other kind needs a CEL expression whose meaning the author chooses. Both include the observed generation, not a health verdict. A work-level `Complete=True` stops further reconciliation of that work; a per-manifest `Complete=True` stops updates to that manifest. A work that has completed will not pick up a spec change until the condition no longer holds.
- **Status feedback is separate.** `status.resourceStatus.manifests[].statusFeedback.values` carries JSONPath-derived values when `feedbackRules` are configured on the manifest config (the status field `statusFeedback` is singular). `Watch`-fed resources report promptly; polled resources report on agent reconcile. The agent registers at most 50 informers, keyed by group/version/resource and namespace and shared across all works, and falls back to polling when that budget is exhausted.
- **Convergence cadence.** The work-agent requeues each work every 4 minutes with ±50% jitter, so a drifted manifest is repaired at the next requeue: anywhere from immediately after the change to about 6 minutes later. `Update` rewrites the manifest and overwrites the drifted field. `ServerSideApply` does not: a field owned by a different field manager produces a conflict, the other manager's value stays, and `Applied` goes `False` (W-28). SSA drift repair therefore depends on ownership, not only on cadence; forcing it would take the field from the other manager.
- **Cleanup.** A finalizer keeps the work object alive until spoke cleanup completes. On the spoke, `AppliedManifestWork` records the work name, the hub hash, the agent ID, and the identities of applied resources (group, resource, namespace, name, UID, version). Its creation time marks when the ledger entry was created, not when the current spec was applied, and each applied-resource UID is refreshed from the live object when the ledger is reconciled, so it identifies the incarnation the agent last observed rather than the one the apply created. Status carries `evictionStartTime` once the work is missing from the hub or the hub hash changes — an eviction signal, not an apply timestamp. It carries no component version. Its labels are copied from the work at creation and are a snapshot; annotations are not copied. It is not part of OCM-io's hub-side status contract; on a self-registered hub it remains the cluster-local ledger, not a hub record.
- **Deletion.** `spec.deleteOption` chooses propagation behavior: `Foreground` removes the resources, `Orphan` leaves all of them, and `SelectivelyOrphan` orphans only the resources named by its orphaning rules and deletes the rest. While deletion is in progress the work carries a `Deleting` condition with reason `WorkDeleting` and an observed generation. Combined with finalizers, deletion is long-running and can block indefinitely on a resource whose finalizer never clears; cleanup that normally takes seconds can stall for as long as the foreign finalizer exists.
- **Update strategy meaning.** `CreateOnly` and `ReadOnly` deliberately stop forcing convergence. They are not variants of `Update`. `ReadOnly` never creates an object that is absent.

### 3.2 What ManifestWork does not provide

- Ordering or atomicity across manifests, or across the per-cluster works of one intent.
- A hub-visible record of apply attempts per generation. The spoke ledger has no time and no generation applied.
- A way to observe spoke-local cleanup state from the hub.
- Health semantics for arbitrary kinds without author-supplied condition rules. Existence is the only default.
- Any notion of a rollout, a group, a wave, or a gate on the primitive itself. `ManifestWorkReplicaSet` adds strategies, with the limits in §3.3.
- A tenant partition.
- A guaranteed upper bound on drift-repair latency.
- Any authenticated record of who authorized a work or of the exact content applied. `Applied` reports an apply result; `AppliedManifestWork` lists resource identities; neither carries a signer, a content digest, or a trust decision.

### 3.3 ManifestWorkReplicaSet versus Fulfillment

| Fulfillment concept | ManifestWorkReplicaSet | Gap the shim must not paper over |
|---|---|---|
| Manifest strategy, versioned independently | `manifestWorkTemplate.spec`, a single editable template | No version stream; no OCM-io `AddOnTemplate` or template generation; no per-axis history |
| Placement strategy (`selector`, `static`, `all`) | `placementRefs` resolving through OCM-io `Placement` | Only OCM-io placement semantics; no Fleet pool; Fleet cannot select independently |
| Rollout strategy (steps, groups, before/after tasks, preemption) | `rolloutStrategy` on each placement ref: `All`, `Progressive`, `ProgressivePerGroup`, with `minSuccessTime`, `progressDeadline`, `maxFailures`, `maxConcurrency`, and mandatory decision groups | No before/after tasks, no preemption, no arbitrary gates. Advancement depends on `Progressing` conditions the template author must supply. `Progressive` defaults to every selected cluster in one wave unless `maxConcurrency` is set, and `maxFailures` defaults to zero. The whole API is behind an Alpha feature gate that is off by default in v1.4.0. |
| Generation bumping reconciliation | one `metadata.generation` on the MWRS, plus per-child generations | Three Fleet streams collapse into one; lineage must be synthesized |
| Targets (`kubernetes`, `platform`, `local`, Fleet addon-registered) | ManagedClusters selected by Placement | Non-Kubernetes, local, and Fleet addon-registered targets are unreachable |
| Delivery agent contract (Accepted vs Delivered, journal, fencing) | klusterlet apply plus `AppliedManifestWork` | No Accepted/Delivered split, no journal, no fencing visible on the hub |
| Per-target delivery records keyed by fulfillment and target | child ManifestWorks, one per selected cluster | Correlation is by child object, not by a durable delivery record |
| Ownership of fanned-out children | labels (`.../placementname`, `.../ownerkey-hash`; the deprecated `.../manifestworkreplicaset` label only when its value fits the 63-character limit) and an `...-owner` annotation; no `ownerReferences` | Ownership must be read from labels, and it is not the Kubernetes ownership relation. Children look like ordinary objects and accept writes; the controller reconciles edits away. |
| Status aggregated from delivery conditions via CEL | `summary` (`total`, `applied`, `available`, `progressing`, `degraded`, `desiredTotal`, `updated`) plus a per-placement summary | Aggregate only; per-cluster truth lives on children. `desiredTotal` and `updated` are declared in the API but are not populated by the v1.4.0 controller, so their zero value is not evidence of zero. |
| Inert primitive owned by a user-facing concept | directly editable | Anyone with RBAC can edit it; authority is weaker |
| Tenant/workspace scoping | cluster-scoped object over cluster-namespaced children | No partition to map onto |

The name echoes Kubernetes `Deployment` → `ReplicaSet`, but the mechanism differs: an MWRS does not reconcile a running workload on the spoke, it creates hub-side `ManifestWork` objects that the klusterlet later applies. There is no revision history and no surge or unavailable accounting. Its progressive rollout is real but conditional: a placement whose clusters never report `Progressing` never advances, and the default strategy is all-at-once.

### 3.4 The status words, in one place

| Word | Source | What it means | What it does not mean |
|---|---|---|---|
| `Applied` | work-agent apply loop | The last apply attempt produced no error | The object is healthy or current |
| `Available` | object read | The object exists on the spoke | The object is healthy, ready, or fully reconciled |
| `Progressing` | condition rules | Whatever the rule's CEL expression says | That OCM-io derives progress for you |
| `Degraded` | condition rules | Whatever the rule's CEL expression says | A default failure verdict |
| `Complete` (work) | well-known Job/Pod rule or CEL | Stop reconciling this work | Delivered, healthy, or up to date |
| `Complete` (manifest) | per-manifest condition rule | Stop updating this manifest | The manifest is healthy |
| `Deleting` | finalizer controller | Deletion of the work has begun | Cleanup has finished, or will finish |
| `statusFeedback` | JSONPath rules | The declared fields, as last observed | The full object status |

Reading any of these as "fine" is the most common integration error. The requirements therefore forbid presenting `Available` as health (W-14) and require a declared meaning for every condition that crosses the boundary (S-10).

---

## 4. Status freshness in OCM-io today

The shim's freshness requirements (S-3, S-5) and its ability to tell applied from healthy (W-14, W-15) depend on what OCM-io status actually reports. As of v1.4.x:

**Present**

- `Klusterlet` and `ClusterManager` carry a top-level `status.observedGeneration`.
- `ManifestWork` and `ManifestWorkReplicaSet` set `observedGeneration` on their conditions, not on the top-level status.
- Addon config references record `lastObservedGeneration` for the configuration object.

**Missing or incomplete**

- `ManagedCluster` (including version, capacity, and claims), `Placement`, `PlacementDecision`, `ManagedClusterAddOn`, `ClusterManagementAddOn`, and the top-level status of `ManifestWork` and `ManifestWorkReplicaSet`.
- `AppliedManifestWork`, `AddOnDeploymentConfig`, `AddOnTemplate`, `ClusterClaim`, and `AddOnPlacementScore` have no freshness marker at all.

**What a `ManagedCluster` actually carries.** Identity is the object name. Labels are the placement surface. `status` holds `capacity` and `allocatable`, `version.kubernetes`, `conditions` (`HubAcceptedManagedCluster`, `ManagedClusterJoined`, `ManagedClusterConditionAvailable`, `ManagedClusterConditionClockSynced`), and `clusterClaims`. It has no `observedGeneration` and no per-field timestamp. In v1.4.0 there are no default claims: `status.clusterClaims` is empty unless the spoke has `ClusterClaim` objects (or an addon writes claims), and a claim created on the spoke is carried into `status.clusterClaims` by the claim controller.

**Delivery cadences.** These are properties of OCM-io, not budgets the shim can improve:

| Operation | Cadence | Mechanism |
|---|---|---|
| `ManifestWork` create → apply attempt | within seconds | work-agent watch |
| Drift repair for `Update` | at the next requeue: immediately to about 6 minutes after the change | 4-minute requeue with ±50% jitter |
| `ServerSideApply` on a field owned by another manager | not corrected while that manager owns the field; `Applied=False` with the conflict | apply refuses to take ownership; only `force` or the other manager releasing the field changes that |
| Polled status feedback | at the agent's next reconcile, up to one requeue interval | same resync |
| `Watch`-fed status feedback | promptly after the change | watch on the federated resource |
| Spoke `ClusterClaim` → hub `status.clusterClaims` | at the claim controller's next reconcile | claim controller watch |
| MWRS child creation | seconds after the placement decision | hub controller |
| MWRS cascade deletion with `Foreground` | tens of seconds to unbounded if a child is blocked | children removed first; the MWRS finalizer remains until they are gone |

The `metav1.Condition` type already has the slot wherever conditions exist; the gap is in the writers, not the schema. The shim cannot require this gap to close (C-16); it records source generation and timestamps on its own side and treats a missing marker as unknown, never current (BF-2). The general upstream option is E-1 in [enhancements.md](enhancements.md#e-1--standard-freshness-and-condition-vocabulary).

---

## 5. How Fleet learns about targets

Fleet has no REST endpoint that creates, relabels, or removes an arbitrary target. A target and its `target:<id>` inventory item enter through one of three mechanisms, two of which exist in the resource manager today:

- **Fleet addon connect.** A Fleet addon declares static targets in its connect input; the platform registers each one with `RegisterOrVerify` and rolls the registration back if the connect fails. The labels are fixed at registration: a later connect with different labels fails verification rather than updating them. Disconnect and disable do not remove the target, and no public API does.
- **Delivery outputs.** A delivery agent returns `ProvisionedTargets`; the platform upserts each target and its inventory item, so a later delivery replaces their content in place. Deleting the deployment that owns a fulfillment deletes the targets whose inventory items still trace to that fulfillment's deliveries, keyed by the delivery-owned inventory item.
- **Fleetlet control event.** `RegisterTarget` and `TargetAccepted` are defined in the control protocol, but the resource manager has no handler for them. The path is declared, not implemented, and cannot be used.

The difference decides how adoption works. Cluster inventory changes labels and leaves inventory, so an adopted target needs both relabeling and removal.

- **Fleet addon-connect registration cannot provide either.** The first relabel would fail verification, and an un-adopted cluster would remain a selectable target after its `ManagedCluster` is gone. Static registration is therefore not an adoption path.
- **A delivery output provides both, but only per fulfillment.** The upsert replaces the whole target on each delivery, and deletion removes the targets that still trace to that fulfillment's deliveries, keyed by the delivery-owned inventory item. Adoption therefore uses one dedicated registration fulfillment per cluster: its manifest carries the cluster's identity, labels, and projected claims; the provider returns a target carrying those labels, with its inventory item; and deleting the deployment that owns the current fulfillment un-adopts exactly one cluster. Fleet has no update, apply, or invalidate surface, so a label or claim change is not written into the existing fulfillment: the shim creates a successor carrying the changed record, lets its delivery upsert the same target ID and take ownership of the `target:<id>` item, and then deletes the deployment that owns the predecessor, whose cleanup no longer owns anything. Deleting that deployment first would remove the target and take it out of placement until the successor re-created it. A shared registration fulfillment would make one removal delete every target it produced.
- **The two paths must not meet.** If the same target ID is registered statically and later produced by a delivery, cleanup keys on the delivery-owned inventory item and deletes the target with the fulfillment. A target has one registration path.

The registration provider is a shim-owned Fleet delivery provider, registered through a delivery-only Fleet addon, using the same provider shape Fleet uses to create a target from a delivery output. It is shim infrastructure, not a mapped Fleet addon: neither its Fleet addon record and descriptor, nor its registration target and registration fulfillments, are mapped or shown as tenant inventory (M-8, scope row 16).

Registration is not health. A target registered with an empty state is treated as eligible; the label selector matches exact label values only, and properties are informational. A target being present therefore says nothing about the cluster being reachable, and a claim-based OCM-io selection has no direct Fleet equivalent until claims are projected into target labels (GAP-8). A label or claim change reaches Fleet as a successor registration delivery, so its latency is the delivery budget, not a direct API write (GAP-12).

## 6. Provenance, GitOps, and the boundary

This section explains why provenance is a delivery property and why GitOps is an authority question, not another delivery path. It does not add requirements; PA and GO in [requirements.md §18](requirements.md#18-provenance-and-source-authority-pa-go) do.

### 6.1 What Fleet provenance proves

Fleet's provenance design answers three questions about a delivery: which principal authenticated the assertion, which exact typed content and purpose it covered, and why the target's trust state accepts that principal and evidence. When a user signs a deployment intent, the resource manager stores the signature alongside the deployment fields it covers, then assembles the signed input and an attestation with the typed delivery action at delivery time and passes it to the delivery agent. The assembled attestation carries the stored signature and the minimal signer assertion — an enrollment identity token, a key registry, and a registry subject — so the verifier resolves the signer's keys itself and re-checks the subject mapping rather than trusting a key supplied for that delivery. The agent verifies the signer against its trust configuration, the exact content against the signed input, the generation against replay when the signer pins one, and the validity window before applying. The resource manager is a courier, not an author: compromising it lets an attacker bypass authorization, omit or delay evidence, or present a fork history, but it does not let the attacker forge a signature. Whether it can substitute a trust root depends on the verifier: a target-side verifier holds its own trust configuration, while a platform-side verifier trusts configuration the resource manager can change.

That guarantee is only as strong as the verifier's position. A target-side verifier — the fleetlet — holds its own trust configuration and accepts or rejects the delivery locally; a compromised resource manager can withhold or replay but cannot make it accept different content. This placement is proposed, not yet implemented. The implemented Kubernetes addon verifies on the platform side when the deployment carries an attestation, using a trust bundle from the target's properties; that protects against external forgery, but a compromised resource manager can skip its own verification and apply with the platform credentials it already holds. A deployment created without a signature carries no attestation, so the same addon applies it through token passthrough without invoking a verifier; that path's locus is none, not platform-side, and the shim reports it as provenance-absent. A path with no verifier — the OCM-io work agent — has neither guarantee: the work agent applies what the hub sends and records the applied resource identities, not who authorized them. The shim must name the locus for every path instead of saying "delivery" as if all paths were equivalent (PA-2, PA-3).

The implemented model is narrower than the design. The verifier handles deployment content, not managed-resource content; the shim maps no managed-resource aspect, so that boundary is outside its declared gaps. For the deliveries it does map, four limits are declared rather than papered over: there is no evidence log, delete produces no verifiable signed removal evidence, no durable API exposes a per-delivery result, and output binding compares the delivered manifests to the full signed manifest set, so a target that filters manifests by accepted type fails verification rather than receiving a verified subset. Removal, historical verification, per-delivery state, and filtered delivery are therefore declared gaps, not properties the shim can assume (PA-4, PA-5, PA-6, PA-7; GAP-18, GAP-19, GAP-20, GAP-21). The platform-side path has one further operational limit: deleting a signed deployment advances the fulfillment generation while the stored attestation keeps the generation the signer pinned, so the removal is rejected and the delete pauses until the signer re-signs at the next generation (GAP-18).

### 6.2 Why inventory is not attested

Cluster inventory is observation. A `ManagedCluster` label, a `ClusterClaim`, a registration condition, and a `ManifestWork` status are facts the shim read at a time; they carry a source object, a generation or resourceVersion where the API provides one, and a read time. They do not carry a signature, a digest of the object's exact bytes, or an authorization decision. Even when a registration fulfillment carries the projection into Fleet, the fulfillment's evidence, if any, covers the manifest it delivered at that generation — the projected values it carried — not the OCM-io object they were read from or their continued truth. Treating the copied fact as attested because it traveled in a delivery is the central provenance error the requirements forbid (PA-1).

The same distinction applies on the OCM-io side. `Applied` means the last apply attempt produced no error; `Available` means the object exists; `AppliedManifestWork` is a ledger of resource identities. None of them is a verification result, and none becomes one by being read from the hub. An operator who needs to know who authorized a change on an OCM-io cluster needs OCM-io's own audit trail or an applier that verifies evidence; Fleet can report what it observed and what it cannot prove (PA-7, GAP-14).

### 6.3 GitOps as authority

GitOps is already how many OCM-io users express desired state. It is not a separate delivery path the shim can co-own. The applier owns every field in its desired state, so any other value of those fields is drift it is entitled to restore; because it reasserts that state continuously, it is an authority rather than a one-time input, and a manifest cannot declare which fields its applier owns. The shim therefore cannot write to a GitOps-managed object and then negotiate ownership through annotations or labels: the applier's desired state remains authoritative, and injected tracking metadata is not a contract it honors (C-2). It also cannot treat a conflict as a transient error and force its value; that takes the field from the applier and creates a fight the shim cannot win (U-7, GO-1, GO-2).

The collision is concrete on the OCM-io side. A manifest in a work object with no explicit update strategy defaults to `Update`, which builds the desired object from the work, keeps the existing object's labels, annotations, owner references, and finalizers, and then replaces the object; a field another writer set that the work does not declare is dropped. `ServerSideApply` with `force=false` returns a conflict and leaves the other writer's value; `CreateOnly` and `ReadOnly` stop OCM-io from correcting the object. The per-manifest strategy is the only ownership lever, and it lives in the work object rather than in the manifest the applier owns (GAP-16). A delivery that overlaps a GitOps-managed workload therefore has to choose its strategy deliberately or it will oscillate.

The same collision exists on the Fleet delivery path. The implemented platform-side delivery agent applies each manifest with server-side apply under a fixed field manager and without force, so a field owned by another applier — a GitOps controller on the target, an operator, an autoscaler — that the apply would change returns a conflict and the delivery fails with a message naming the other manager instead of overwriting it. That is the required behavior, not a limitation to work around: forcing the apply would take the field from the applier and start the fight GO-1 and GO-2 forbid. A batch stops at the first conflicting manifest, so earlier manifests may already be applied. Re-delivery is idempotent, but Fleet treats the failure as retryable and repeats the whole delivery on its default five-second interval with no attempt cap, so the same non-force attempt recurs until the intent or the ownership changes. That class of failure does not move the deployment to a failed state and produces no pause reason: a deployment that never completed stays `CREATING` with `reconciling` true, and one that had completed stays `ACTIVE` with no visible failure. The shim reports the conflict from a result it receives first-hand, adds no retry, and declares the substrate's loop rather than presenting it as convergence (GAP-22). Where a delivery agent applies locally through a fleetlet, the same non-force rule is a requirement on the shim rather than an observed property of an implementation.

A Git commit is also not automatically provenance. Commit signing authenticates the commit; it does not say which platform content the signer authorized or whether the delivery agent should trust that signer. Git is not a transport for Fleet intent in this design: the applier writes OCM-io objects, and the shim observes the result. A Git identity becomes delivery provenance only when a configured profile defines how that identity authenticates the exact platform content. Until then, the shim records the OCM-io source object and read time it observed, reports a revision only where a profile binds it, and never injects source metadata into OCM-io (GO-4, GO-5, GAP-15).

What GitOps does provide is a truth the shim can rely on: the applier's desired state is the authority for the fields it contains. The shim's job is to observe the result, record the OCM-io source object and read time it observed, and never become a second reconciler (GO-1, GO-2). A rollback in Git is an ordinary change; a repeated correction is drift to surface, with the competing manager named where the API exposes it (GO-6, GO-7).

### 6.4 What this means for the shim

Four consequences follow. First, the shim reports an evidence class next to every mapped fact, so "observed" is not read as "verified" (PA-1, O-7). Second, it names the verification locus for every delivery path and marks the OCM-io work-agent path as downgraded, rather than presenting apply status as authenticated delivery (PA-2, PA-3). Third, it reports per-delivery verification only to the extent the API and the live event stream expose it: deployment state, pause text, live delivery events while connected, and what the shim recorded at submission, with per-target outcomes marked partial where Fleet exposes no durable delivery read (PA-7; GAP-21). Fourth, it keeps its own control surface declarative and idempotent — applying a record twice is safe and deleting it removes exactly its unit — and records the OCM-io source object it observed rather than a Git revision it cannot see, so no metadata is injected into OCM-io; where the baseline exposes only an imperative surface, that limit is declared (GO-3, GO-4; GAP-17). The confirmed inventory increment has no delivery provenance for the cluster facts it maps; saying so is part of the design, not a gap in the implementation (T-6, GAP-13).

## 7. What this analysis does not decide

Nothing here changes scope, authority, or a requirement. Scope is in requirements §4; authority in §6; the baseline version and API set are recorded in [decisions.md](decisions.md); the per-mode benefit claims are in §2.1 and are repeated in [coverage.md §7](coverage.md#7-benefits-and-the-first-increment).
