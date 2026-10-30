# OCM-io × OCM Fleet Shim — Use Cases and User Stories

This document turns the requirements into concrete situations. It is a reading aid, not a second requirements document: every story cites the IDs it exercises, and no story adds, relaxes, or reinterprets a MUST. Where a story cannot be supported, it says so and points at the gap instead of describing a workaround.

The use cases are deliberately confined to **cluster inventory**: adopting OCM-io clusters into Fleet, using that inventory for Fleet placement, keeping it fresh, and registering clusters introduced through Fleet back into OCM-io. Workload delivery, placement bridging, delivery provenance, and Fleet's own addon/extension/plugin framework are separate aspects. Workload, placement, and Fleet-addon stories are not written until those aspects are admitted. The provenance and GitOps obligations stay normative in requirements §18. The inventory stories apply them to observed facts and applier-owned fields (UC-O15, UC-O16, UC-O19, UC-O20), and the negative cases record the refusals; delivery-specific provenance is exercised only when delivery is admitted.

Requirements are in [requirements.md](requirements.md). Candidate mechanisms are in [baseline-hypotheses.md](baseline-hypotheses.md). Declared losses are in [coverage.md](coverage.md). Recorded answers are in [decisions.md](decisions.md). Benefit claims B are in [analysis.md §2](analysis.md#2-why-introduce-fleet-to-an-existing-ocm-io-installation); upstream options E are in [enhancements.md](enhancements.md).

---

## 1. How to read a story

Each story has a **status**, a **flow**, an **expected result**, the **design basis** that carries it, and the **requirements** it exercises. The status says how far the requirement is supported; where part of the meaning is lost, the story names the gap instead of describing a workaround.

Status is about scope, not availability. No implementation is included in this directory: every status refers to the requirements and to the candidate mechanism named in the design basis, not to running code.

Status vocabulary:

| Status | Meaning |
|---|---|
| **Confirmed** | Inside the confirmed inventory scope; the design basis names a candidate baseline mechanism. |
| **Confirmed, with prerequisite** | Inside the confirmed scope, but the outcome requires a stated precondition that the shim cannot supply alone. |
| **Confirmed, declared gap** | Inside the confirmed scope, but a named part of the meaning is lost or approximated. The gap ID is the honest limit. |
| **Confirmed, with exclusion** | Inside the confirmed scope, with named objects that are never delivered to or cleaned up. |
| **Confirmed in part** | One part of the story is confirmed and another is excluded; the status line states the split. |
| **Blocked in the baseline** | Inside the confirmed scope, but the outcome depends on a platform capability the deployed baseline does not implement. The shim must report it unavailable, not approximate it. |

A gap citation records a limitation the story must respect; the status reflects whether the story's own outcome is affected. Adoption stories therefore stay **Confirmed** although every adoption pays GAP-12's per-cluster registration cost. **Design basis** names the candidate mechanism that would carry the story (BF-10, BF-16); it is not an approved or implemented design. Story numbers are stable and not contiguous; identifiers are preserved when a story is retired.

Actors:

| Actor | Cares about |
|---|---|
| **OCM platform admin** | Existing registration, placement, addon, and delivery behavior; no regressions. |
| **Fleet platform admin** | Target adoption, registration of Fleet-origin clusters, coverage, workspace grants, un-adoption. |
| **Fleet tenant** | Which clusters its placement selects; never OCM credentials or another tenant's clusters. |
| **Cluster owner / SRE** | Cluster identity, labels, reachability, maintenance. |
| **Operator** | Coverage, freshness, conflicts, and the blast radius of un-adoption. |
| **Auditor / security reviewer** | Which evidence backs each fact, where it came from, and what is recorded rather than attested. |
| **GitOps controller / repo owner** | That the applier's desired state is the only writer for the fields it owns; no shim metadata, no correction fight. |

The one rule that governs every story: **each cluster identity has one registration authority.** OCM-io owns the `ManagedCluster` and the cluster's membership; Fleet owns the target and its inventory projection. A story that needs Fleet to rewrite membership OCM-io already owns is out of scope by construction (F-1, X-1, Z-2).

---

## 2. The running example

The worked example uses two Kubernetes clusters registered in one OCM-io installation — `cluster1` and `local-cluster`, the hub registered as a managed cluster — plus one cluster introduced through Fleet, `fleet-cluster1`. A Fleet resource manager runs alongside. Its `shim-registration` target is the shim's own registration target, not an adoption; each OCM-io cluster that has no Fleet-origin target is adopted by a dedicated registration fulfillment that produces one Fleet target. The names are illustrative; the object shapes are what the stories rely on.

| Cluster | State | Fleet target |
|---|---|---|
| `cluster1` | OCM-io labels `cluster.open-cluster-management.io/clusterset=default`, `env=prod`; claim `platform` | `ocm-cluster1` |
| `local-cluster` (the hub itself) | OCM-io label `cluster.open-cluster-management.io/clusterset=default`; no claims | `ocm-local-cluster` |
| `fleet-cluster1` | known to Fleet; no OCM-io membership until it registers | `fleet-cluster1` |

Labels are the inventory facts Fleet placement can use directly. Claims are carried as inventory facts; a claim used for Fleet selection is projected into an exact-match label and the loss is declared (M-6, GAP-8). The example does not claim that a Fleet placement reproduces an OCM-io `Placement`; the two are separate selection authorities (M-2).

The `ocm-*` targets exist only as the output of their registration fulfillments: a provisioned target is owned by the fulfillment whose delivery last produced it and is removed when the deployment that owns that fulfillment is deleted. That ownership is what makes adoption and un-adoption per-cluster operations (BF-10). A label or claim change is a successor fulfillment whose delivery upserts the same target before the predecessor is deleted, so the target ID survives a refresh. A Fleet-origin target is not owned by a registration fulfillment; it enters Fleet through Fleet's own target registration — a static target or a delivery output — and is correlated to OCM-io after registration, not adopted (BF-16).

The objects the stories refer to:

| System | Object | State in the example |
|---|---|---|
| OCM-io | `ManagedCluster/cluster1` | joined and available; labels and claims as above |
| OCM-io | `ManagedCluster/local-cluster` | joined and available; no claims |
| OCM-io | `ManagedCluster/fleet-cluster1` | absent until the Fleet-origin registration completes |
| Fleet | targets | `shim-registration` (registration target, shim infrastructure), `ocm-cluster1`, `ocm-local-cluster`, `fleet-cluster1` |
| Fleet | inventory items | `target:shim-registration` (shim infrastructure), `target:ocm-cluster1`, `target:ocm-local-cluster`, `target:fleet-cluster1` |
| Fleet | registration fulfillments | one per adopted OCM-io cluster, each delivering to `shim-registration` and producing exactly its `ocm-*` target |
| Git | repository `platform-config`, path `clusters/cluster1` | desired OCM-io labels and claims for `cluster1` |

The GitOps controller in the example is an ordinary applier such as Argo CD or Flux. It applies OCM-io objects — labels and claims on `ManagedCluster`, for example — and is a writer outside the shim. It does not submit Fleet intent, and the shim neither writes its source nor injects metadata into its objects.

---

## 3. What flows which way

| Direction | What crosses | Confirmed? | Notes |
|---|---|---|---|
| **OCM-io → Fleet** | Cluster identity, membership presence, labels, claims | **Yes** | The core inventory scope. Fleet holds a projection; OCM-io remains authoritative. |
| **Fleet → OCM-io** | Registration of a Fleet-origin cluster | **Yes, through OCM-io's registration contract** | Creates real membership, not a placeholder. The cluster runs klusterlet; unregistration is ownership-aware (GAP-23). |
| **Fleet → OCM-io** | Membership, registration, placement, or status writes for OCM-io-origin clusters | **Never** | Fleet does not rename, re-register, or delete membership it did not create (F-1, X-1, Z-2). |
| **OCM-io → Fleet** | Work status, OCM-io addon state, placement decisions | No | Separate aspects; not mapped in the inventory increment. |
| **Fleet → OCM-io** | Fleet addon, extension-resource, or plugin state | No | Fleet-internal framework; an OCM-io `ManagedClusterAddOn` is a different object with a different lifecycle (G-15, scope row 16). |
| **GitOps → OCM-io** | Desired OCM-io objects | **Yes, outside the shim** | The applier owns what it applies; the shim observes and never writes or reverts (GO-1, GO-2). |

The asymmetry is deliberate. An adopted cluster can leave Fleet without leaving OCM-io; a cluster registered through Fleet joins OCM-io for real, and removing that membership is an ownership-aware operation that touches nothing the shim did not drive (AD-7, M-9; UC-F2). Neither direction makes the shim the authority for the other system's registration. Evidence does not flow back either: an OCM-io apply status is an observation, not an authentication of the content (PA-3).

---

## 4. OCM-io → Fleet use cases

These stories cover adoption, identity, coverage, tenancy, freshness, and the GitOps authority over OCM-io objects. They do not reproduce OCM-io placement selection in Fleet: Fleet placement is a separate authority over projected labels, and equivalence with an arbitrary OCM-io `Placement` is not claimed (M-2, M-6).

### UC-O1 Adopt the existing fleet in one pass

**Story.** As an OCM platform admin, I want every cluster already registered in OCM-io to appear in Fleet with its identity, labels, and claims, so that Fleet placement can use the fleet I already run without re-registering anything.

**Status.** Confirmed.

**Flow.**
1. The shim lists the `ManagedCluster` objects in the declared scope.
2. For each cluster with no correlated Fleet-origin target, no current registration fulfillment, and no recorded un-adoption, it creates one dedicated registration fulfillment whose manifest carries the cluster identity, its labels, and its claims projected into target labels.
3. The registration provider returns a `ProvisionedTarget` carrying the target ID and projected labels; Fleet records the delivery-owned `target:<id>` inventory item.
4. Fleet placement can select the target by its labels.

**Expected result.** One target per OCM-io cluster — the correlated Fleet-origin target where one exists, otherwise the adoption target. For an adopted cluster, deleting the deployment that owns its current registration fulfillment removes exactly that target. Adoption is a reconciliation, not a one-time pass: a cluster that registers into OCM-io later is adopted by the same rule, while an explicitly un-adopted cluster is not silently re-adopted (M-8; UC-O6). OCM-io is not modified.

**Design basis.** A target created from a delivery's provisioned-target output carries the inventory item owned by that fulfillment, and deleting the deployment that owns the current fulfillment removes exactly that target (BF-10, GAP-9). Adoption adds the OCM-io labels and projected claims to the same target shape and writes nothing back to OCM-io. A label change is a successor fulfillment delivered before the deployment that owns the predecessor is deleted; if the successor fails, the predecessor stays, the target is not removed, the old labels remain selectable, and the refresh is reported failed (GAP-12). A cluster already correlated to a Fleet-origin target is not adopted: its target is not a delivery output, so a second target would duplicate it (BF-16, F-1).

**Requirements.** F-1, M-6, M-7, M-8, M-9; BF-10, BF-16; GAP-9, GAP-12.

### UC-O6 A cluster leaves OCM-io and exactly one Fleet target goes away

**Story.** As a Fleet platform admin, I want un-adopting a cluster to remove its target and nothing else, so that a decommissioned cluster cannot remain selectable and a live cluster cannot be removed by mistake.

**Status.** Confirmed.

**Flow.**
1. The `ManagedCluster` is deleted.
2. The shim finds the deployment that owns the cluster's current registration fulfillment and deletes it.
3. Fleet cleanup removes the target and its `target:<id>` inventory item produced by that fulfillment.
4. Other clusters' targets and fulfillments are untouched.

**Expected result.** Exactly one target removed. Narrowing the declared scope is not un-adoption: it stops new writes, leaves existing targets and records in place, and marks the target out of scope in shim status (Z-9). The target remains visible to Fleet placement — the shim cannot make Fleet selection stop, and does not claim it has. A Fleet-origin cluster has no registration fulfillment and is not removed here: losing its OCM-io membership returns its target to the unregistered state (UC-F2, BF-16). OCM-io's own deletion lifecycle continues independently (M-5). The operator can see the removal unit and its blast radius before acting (M-9).

**Design basis.** Cleanup keys on the delivery-owned `target:<id>` inventory item, so deleting the deployment that owns the current fulfillment removes exactly one target (GAP-9). OCM-io's own deletion lifecycle is separate (M-5), and scope narrowing retains data rather than deleting it (Z-9). Deleting that deployment while the `ManagedCluster` still exists is a deliberate un-adoption, not a transient state: the shim MUST NOT silently re-adopt the cluster, and re-adoption is an explicit operation (M-8).

**Requirements.** F-1, M-1, M-5, M-8, M-9, Z-9; BF-10; GAP-9, GAP-12.

### UC-O7 A cluster re-registers under the same name

**Story.** As an OCM platform admin, I want a re-registered cluster to be treated as a new incarnation, so that a rebuilt cluster does not inherit the old cluster's Fleet identity, labels, or history.

**Status.** Confirmed, declared gap — a composite key detects the change and refuses inheritance; without a stable cluster identity the shim cannot recognize the same physical cluster across re-registration (GAP-5).

**Flow.**
1. The old `ManagedCluster` is deleted, and the shim removes the deployment that owns its registration fulfillment.
2. A new cluster registers with the same name but a new UID and creation timestamp.
3. The shim's composite key differs; it creates a new correlation and a new target.
4. Any stale record from the old incarnation is refused rather than reused.

**Expected result.** No inheritance. The old target is gone before or with the old incarnation; the new target is a new identity even if the name matches.

**Design basis.** `ManagedCluster` exposes UID and `creationTimestamp`, so a composite key can distinguish incarnations even when the name repeats (BF-1, BF-6). The cluster's own registration record carries no stable foreign key, so the identity must be composed, not looked up. A cluster-supplied stable identity (E-5) would let the shim keep one target across a re-registration; the baseline has none, so a new incarnation gets a new target (M-7).

**Requirements.** I-1, I-2, I-3, I-7, F-9, M-7; BF-1, BF-6; GAP-5.

### UC-O8 An unreachable cluster stays a target but is not reported healthy

**Story.** As a Fleet tenant, I want an unreachable cluster to remain in Fleet so its state is visible, but I do not want Fleet to present target presence as cluster health.

**Status.** Confirmed.

**Flow.**
1. The cluster stops heartbeating; OCM-io marks its conditions accordingly.
2. The Fleet target remains; placement does not silently shrink because reachability changed (M-4).
3. The shim reports "health: not reported"; health conditions are candidate scope (row 2), so the confirmed increment has no condition rule to report from.
4. `Applied=True` or target presence is never shown as health (S-10).

**Expected result.** Target present, health unknown or not reported, with the reason named. Fleet does not remove workloads because a cluster became unreachable.

**Design basis.** Target state and cluster conditions are separate facts. In the confirmed increment the shim has no condition rule, so it reports health as not reported; target presence and target state describe Fleet's target record, not the cluster (BF-4, GAP-3).

**Requirements.** M-4, S-10, O-1, O-6; BF-4; GAP-3.

### UC-O9 The hub is in inventory, but its control plane is not a workload target

**Story.** As a Fleet platform admin, I want the hub registered as a `ManagedCluster` to appear as a Fleet target with its role visible, while OCM-io's control-plane components and the shim's own components are excluded from delivery and cleanup.

**Status.** Confirmed, with exclusion.

**Flow.**
1. `local-cluster` is a normal `ManagedCluster` in the hub's own inventory.
2. The shim adopts it like any other cluster, but marks its role in shim status.
3. OCM-io's control-plane namespaces and the shim's own components are never treated as removable workload targets.
4. Being self-registered is not implicit opt-in for any unmapped aspect.

**Expected result.** The hub appears with a role marker; its OCM-io control plane is excluded; no shim component is a delivery target.

**Design basis.** `local-cluster` is an ordinary `ManagedCluster` with a clusterset label and no claims, so adoption treats it like any other cluster. Role marking and the exclusions are shim-side declarations (H-15, X-11).

**Requirements.** H-15, X-11, F-10, I-10, Z-17.

### UC-O10 One tenant sees only the clusters granted to its workspace

**Story.** As a Fleet tenant, I want to see and select only the OCM-io clusters granted to my workspace, so that adopting OCM-io clusters does not leak the whole fleet to every tenant.

**Status.** Blocked in the baseline — the outcome depends on Fleet's workspace partition, which the deployed baseline does not implement; until it does, tenant isolation cannot be claimed and the shim must report tenant-scoped visibility unavailable (GAP-24).

**Flow.**
1. A Fleet platform admin grants clusters to a workspace (or withholds them).
2. The tenant's placement sees only granted targets, where Fleet enforces workspace grants.
3. The tenant never receives OCM-io credentials, impersonation, or RBAC.
4. Removing the grant removes the tenant's visibility, not the cluster or the target.

**Expected result.** Per-workspace visibility where Fleet enforces workspace grants; until then, inventory is provider-scoped and no tenant-scoped claim is made (T-9, GAP-24).

**Design basis.** Targets and inventory items are internal orchestration state; Fleet exposes no tenant-facing target collection. Visibility is granted per workspace, so a tenant sees a target only when it is granted (T-9, Q-6). The shim must not present a tenant with inventory until Fleet enforces the grant (GAP-24).

**Requirements.** T-1, T-2, T-7, T-9, X-10; Q-6; GAP-24.

### UC-O11 An outage leaves last-known facts visible and marked stale

**Story.** As an operator, I want last-known inventory to stay visible and be marked stale during an outage, so that I do not mistake a stale reading for a fresh one.

**Status.** Confirmed, declared gap — a target label carries no freshness marker, so staleness is visible only in shim status (GAP-12).

**Flow.**
1. OCM-io or Fleet becomes unavailable.
2. The shim retains the last projected labels and claims; it does not delete or empty them (S-12).
3. It marks the inventory stale with the time it was last confirmed.
4. Fleet placement continues to use the last projected labels; the target itself cannot show that they are stale.
5. After recovery, the shim converges within the declared outage budget.

**Expected result.** No silent replacement by empty values; staleness visible in shim status; the residual limit declared (GAP-12). Full convergence within 10 minutes after an outage at the declared profile.

**Design basis.** A target label is a plain field with no timestamp, so the target itself cannot carry freshness. The shim's inventory record is the only place staleness and last confirmation can be stated (S-12, GAP-12).

**Requirements.** S-12, N-4; Q-17; GAP-12.

### UC-O13 An operator can tell "mapped" from "healthy"

**Story.** As an operator, I want coverage, freshness, and health reported separately, so that "cluster inventory is mapped" is never read as "the cluster is healthy" or "everything is synced".

**Status.** Confirmed.

**Flow.**
1. The shim exposes coverage per cluster and per aspect, next to health.
2. A cluster that is mapped but stale shows stale, not healthy.
3. An aspect that is not mapped shows "not mapped", not empty and not healthy.
4. Health is "not reported" unless a condition rule names it.

**Expected result.** Three independent signals: shim health, convergence, coverage. Partial scope is a first-class state.

**Design basis.** Target presence, `ManagedCluster` conditions, and workload health are independent signals. Reporting them separately is the only way to keep "mapped" from being read as "healthy" (BF-9, GAP-3).

**Requirements.** O-1, O-6, O-7, S-10, Z-6, Z-7; BF-9; GAP-3.

### UC-O14 Adopt a subset now and expand later without re-adopting

**Story.** As a Fleet platform admin, I want to adopt one `ManagedClusterSet` first and add another later, so that scope growth does not require re-creating identities or taking downtime.

**Status.** Confirmed.

**Flow.**
1. The shim adopts the clusters in the declared set.
2. A second set is added to the declared scope.
3. New clusters are adopted; existing targets and correlations are unchanged.
4. No OCM-io object is rewritten.

**Expected result.** Additive scope growth (Z-8); no re-adoption of existing clusters. The same rule applies when an aspect is enabled later.

**Design basis.** Scope is a declared set of clusters and aspects; adding a set adopts only the new clusters. Existing targets are untouched because their registration fulfillments are unchanged (Z-8, BF-10).

**Requirements.** Z-1, Z-8, F-11, M-2; BF-10.

### UC-O15 GitOps owns the cluster facts; Fleet follows

**Story.** As an OCM platform admin who manages cluster labels and claims in Git, I want Fleet's view to follow the applier's desired state without the shim writing to the hub, so Git remains the single source of truth.

**Status.** Confirmed.

**Flow.**
1. A GitOps controller applies a change to `ManagedCluster/cluster1` labels or its `ClusterClaim` objects from a Git commit.
2. OCM-io accepts the change; the applier owns every field in its desired state.
3. The shim observes the new values and replaces the cluster's registration fulfillment with a successor carrying them; the successor's delivery upserts the Fleet target under the same target ID, then the shim deletes the deployment that owns the predecessor.
4. A later Git revert changes the state back; the projection follows without an error.

**Expected result.** Fleet reflects the latest Git-applied state. The shim adds no label, annotation, finalizer, or owner reference to the OCM-io objects, and it does not correct the applier. The shim's record shows the OCM-io source object and the time it observed the change.

**Design basis.** The applier's desired state is authoritative for the fields it contains; the shim reads and re-delivers its registration record but never writes the object (GO-1, GO-2, GO-7; C-2). Label and claim refresh is the successor registration delivery in BF-10 and BF-11, so its latency is the delivery budget (GAP-12).

**Requirements.** C-2, C-6, M-6, M-9, S-12, X-2, U-7, GO-1, GO-2, GO-7; BF-10, BF-11; GAP-12.

### UC-O16 An inventory fact is an observation, not an attestation

**Story.** As an OCM platform auditor, when Fleet reports a cluster's labels and claims, I want to know whether those facts are attested or merely observed, so I do not mistake a current reading for proof of who authorized it.

**Status.** Confirmed, declared gap — inventory facts are observed, and the inventory record carries no signer or digest (GAP-14).

**Flow.**
1. The auditor opens the shim's inventory record for `ocm-cluster1`.
2. The shim reports the OCM-io source object, its generation or resourceVersion, the time it was read, and the evidence class: observed.
3. The record does not carry a signer, a content digest, or a verification result. The registration fulfillment's evidence, if any, covers the target record its manifest delivered — the projected values — not the OCM-io object they were read from, and not any later value.
4. If the auditor asks who authorized an OCM-io change, the shim reports that the OCM-io object carries no such evidence and that Fleet holds none for it.

**Expected result.** An explicit evidence class and source next to every inventory fact. No attestation is synthesized, and no observed fact inherits the provenance of a delivery that carried it. The answer states what is unavailable as clearly as what is available.

**Design basis.** Inventory is observation; only a delivery has provenance, and only a verifier can establish it (PA-1, PA-3, PA-7; Q-25). `ManagedCluster` records no signer or content digest for its current state (GAP-14).

**Requirements.** PA-1, PA-3, PA-7, S-3, O-1, T-6; Q-25; GAP-14.

### UC-O19 An out-of-band edit is reverted; the shim reports the transient, not a new truth

**Story.** As an OCM platform admin, when someone edits a GitOps-managed object directly, I want the applier to restore the declared state and the shim to show the edit as a transient observation, so the repository stays the source of truth and the shim does not adopt the edit as a new baseline.

**Status.** Confirmed in part — observation is confirmed; drift attribution against the applier's desired state is not available to the shim (GAP-15).

**Flow.**
1. An operator changes, outside the applier, a label that the applier's desired state declares on `ManagedCluster/cluster1`.
2. The shim reads the new value and records it with its read time as observed. It does not see the applier's desired state or revision, so it names the source as unavailable rather than inventing one (GO-4). The projection follows the observed value, because OCM-io is the source for the object's current state and the shim has no basis to prefer the applier's earlier value.
3. The applier's next reconcile restores the Git-declared value; the shim reads the restored value and reports the prior reading as transient.
4. The shim writes nothing to the object and does not treat either reading as a new authority.

**Expected result.** The repository remains the truth. The operator can see both readings and their times, and the shim names the source as unavailable instead of claiming it knows what Git declares. The shim does not claim it prevented the edit; the applier did that. If the applier is not running, the edited value remains the current observation, projected to Fleet with its read time and no source claim — visible as an observation, never presented as Git-authorized state.

**Design basis.** The applier owns every field in its desired state, so an out-of-band value for one of those fields is drift it restores (GO-1, GO-2, GO-7). Observation carries source and read time, never provenance (PA-1, S-3). The shim cannot see the applier's desired state, and it does not attribute a Git revision it cannot observe (GO-4, GAP-15).

**Requirements.** GO-1, GO-2, GO-4, GO-7, PA-1, S-3; GAP-15.

### UC-O20 The GitOps pipeline keeps working while the shim is down

**Story.** As an OCM platform admin, when the shim is down, degraded, or uninstalled, I want my GitOps controller to continue applying OCM-io objects, so the shim never becomes a dependency of my delivery pipeline.

**Status.** Confirmed.

**Flow.**
1. The applier reconciles a `ManagedCluster` label or `ClusterClaim` change from Git while the shim is stopped.
2. OCM-io accepts the change as it would from any authorized writer; no shim webhook, finalizer, required field, or admission check is in the path.
3. The shim, when it returns, reads the new state and delivers a successor registration fulfillment. It does not replay, revert, or infer the change from a missed event.
4. Uninstalling the shim leaves its records and registration fulfillments in place by default; deleting them is a separate, explicit un-adoption choice (U-4). The applier's objects and pipeline are unchanged.

**Expected result.** GitOps delivery does not depend on the shim being alive. A shim outage delays Fleet's view, not the authoritative state. The shim introduces no blocking dependency and leaves no cleanup for the applier.

**Design basis.** GitOps applies must succeed with the shim down or uninstalled; the shim adds no webhook, finalizer, required field, or admission dependency (GO-8, GO-1, C-4). The shim reads the result rather than replaying events (BF-2).

**Requirements.** C-4, U-4, GO-1, GO-8; BF-2.

---

## 5. Fleet → OCM-io use cases

These stories cover the other confirmed aspect: a cluster introduced through Fleet must be visible in OCM-io, and a target that cannot be registered must stay honestly visible as unregistered.

### UC-F1 A cluster added through Fleet appears in OCM-io inventory

**Story.** As an OCM platform admin, I want a cluster introduced through Fleet to appear as a managed cluster in OCM-io, so that OCM-io inventory remains the single place where operators see every registered cluster.

**Status.** Confirmed, with prerequisite — registration uses OCM-io's own contract; the cluster must run klusterlet, and a target that cannot register stays visibly unregistered (GAP-23).

**Flow.**
1. `fleet-cluster1` is known to Fleet as a target; OCM-io has no `ManagedCluster` for it.
2. The cluster owner installs klusterlet with a bootstrap credential obtained through OCM-io's registration contract; the shim approves the bootstrap CSR only where the installation does not auto-approve, using the approval permission OCM-io's contract already defines (C-12).
3. OCM-io's registration flow creates the `ManagedCluster`; the shim accepts it where the installation does not auto-accept, then correlates the registered identity to the Fleet target (BF-1, BF-6).
4. The cluster is now selectable by OCM-io placement like any other managed cluster; its Fleet target is unchanged.

**Expected result.** One real `ManagedCluster` and one correlated target; no duplicate and no placeholder. The shim never creates a `ManagedCluster` for a cluster that has not registered. The target keeps the labels Fleet registered it with: OCM-io labels and claims acquired after registration stay on the `ManagedCluster` and are not projected onto the target, because row 4 registers the cluster without adopting it back as inventory (GAP-12). If the cluster cannot run klusterlet, the target remains visible as unregistered and OCM-io membership is not created.

**Design basis.** OCM-io's registration contract creates real membership: the CSR is approved, the spoke creates the `ManagedCluster`, and the hub accepts it. The shim completes the steps the installation leaves manual and correlates the result; it does not fabricate the object (BF-16, M-3). Registration latency and cluster cooperation are inherent, not a shim failure (GAP-23).

**Requirements.** F-1, F-6, M-3; BF-1, BF-6, BF-16; GAP-23.

### UC-F2 A Fleet-only target is visible as unregistered, not fabricated into OCM-io

**Story.** As an operator, I want a Fleet target with no OCM-io counterpart to be visible as unregistered rather than silently missing or turned into a fake `ManagedCluster`, so inventory never lies in either direction.

**Status.** Confirmed, declared gap — the target is representable in Fleet and visible in shim status, but OCM-io has no `ManagedCluster` to show it, and the shim creates none (GAP-23).

**Flow.**
1. A Fleet target exists with no `ManagedCluster` and no registration in progress.
2. The shim lists it as `unregistered`, with the target identity and the reason: not requested, registration failed, or the cluster cannot run klusterlet.
3. It does not create a `ManagedCluster`, copy the target into OCM-io, or hide the target from Fleet.
4. When registration succeeds, the record becomes correlated; if the target is deleted before registration, nothing is left in OCM-io. Removing membership after registration is ownership-aware: where the target has a Fleet removal path (a delivery-produced target), removing it makes the shim remove the `ManagedCluster` membership it drove and nothing else; a static target has no Fleet-side delete, so unregistration follows the OCM-io side or the target's owning lifecycle, and the shim reports which path applies. Other clusters and other membership are untouched, and the cluster's klusterlet remains its owner's to remove (AD-7, M-9; GAP-12, GAP-23).

**Expected result.** Both sides remain representable: OCM-io shows the clusters it manages, Fleet shows the targets it knows, and the unregistered state is explicit. No placeholder membership and no silent omission.

**Design basis.** M-3 requires both sides to be representable and forbids fabricated membership. OCM-io has no record of a cluster that has not registered, so the shim's inventory is the only honest surface for the difference (BF-16; GAP-23).

**Requirements.** M-3, F-1, O-6; BF-16; GAP-23.

---

## 6. A day in the life

This sequence uses the values in §2 and shows the intended state on both sides at each step. Only the Fleet-side steps involve the shim; OCM-io objects change only through OCM-io operations, registration, or a GitOps applier. The shim writes none of them, except through OCM-io's Fleet-origin registration contract in step 7 (UC-F1), where it drives registration, approves the bootstrap CSR, and accepts the `ManagedCluster` where the installation does not automate those steps.

| Step | Actor | Action | OCM-io state | Fleet state |
|---|---|---|---|---|
| 1 | Fleet admin | Adopt the `default` cluster set | Two `ManagedCluster` objects, unchanged | Two targets, one registration fulfillment each |
| 2 | GitOps controller | Apply a label change to `cluster1` from `platform-config` | `ManagedCluster` updated; the applier owns the field | Target upserted with the new label by a successor fulfillment; target ID unchanged |
| 3 | Operator | Edit the same label outside the applier | Unchanged until the applier reconciles | Shim reports the observed edit as transient; the target follows the observed value, and shim status records it as observed with its read time; the shim does not claim a Git comparison |
| 4 | GitOps controller | Restore the declared label on its next reconcile | Declared value restored | Target follows; no error is reported |
| 5 | Cluster owner / SRE | Observe the cluster stop heartbeating | `ManagedClusterConditionAvailable=False` or `Unknown` | Target remains; health "not reported"; no false shrink |
| 6 | Fleet admin | Introduce `fleet-cluster1` through Fleet | No `ManagedCluster` yet | Target `fleet-cluster1` visible as unregistered |
| 7 | Shim | Drive OCM-io registration for `fleet-cluster1` | `ManagedCluster/fleet-cluster1` created and accepted | Target correlated to the registered identity |
| 8 | Fleet tenant | Select granted clusters by their labels, where Fleet enforces workspace grants | OCM-io placement decisions unchanged | Fleet placement selects only granted `ocm-*` targets |
| 9 | Fleet admin | Remove a workspace grant, where Fleet enforces workspace grants | Cluster and klusterlet untouched | Tenant no longer sees the target |
| 10 | Fleet admin | Delete the deployment that owns the cluster's current registration fulfillment | Cluster remains registered and managed | That target and its inventory item removed; no other target affected |
| 11 | OCM-io admin | Delete the `ManagedCluster` | OCM-io deletion lifecycle runs | Target already gone, or the shim deletes that cluster's registration deployment |
| 12 | Auditor | Ask which mapped facts are attested and which are observed | No OCM-io change | Each fact reports source, read time, and evidence class; inventory is observed, never attested |
| 13 | Shim operator | Stop the shim, let GitOps apply a change, then restart it | OCM-io state changes without interruption | Last-known inventory retained while stopped; reported stale after restart until reconverged |

The order matters: steps 9 and 10 are Fleet operations and step 11 is an OCM-io operation; none performs the other. If both happen, the shim must tolerate the target already being gone. Step 3 reports an observation, not a comparison against the applier's desired state, which the shim cannot see. Step 7 creates real membership through OCM-io's contract; a failure there leaves step 6's unregistered state in place rather than a placeholder `ManagedCluster` (PA-1, PA-2, GO-4, GAP-23). Steps 8 and 9 apply only where Fleet implements the workspace partition; otherwise cluster visibility is provider-wide and the tenant-scoped claim is not made (T-9, GAP-24).

---

## 7. OCM-io enhancement user stories and how they fare

The OCM-io enhancements repository contains user stories that existing users rely on. Introducing Fleet must not break them, and the shim must state which ones it mirrors into Fleet. "OCM-io side" means the behavior stays exactly as it is; "Fleet side" means what the shim offers in the inventory increment.

| OCM-io proposal | User story, in brief | OCM-io side | Fleet side | Status | Requirements / gap |
|---|---|---|---|---|---|
| **4 — cluster claims** | Claims published by the cluster describe platform, region, and other facts | Unchanged; claims remain in `status.clusterClaims` | Read as observed inventory facts; where a claim is used for Fleet selection it is projected into an exact-match label, with the loss declared | Confirmed, declared gap | M-6, Z-12; GAP-8 |
| **6 / 136 / 15 — placement selection** | Select clusters by labels or claims, by CEL, or by resource capacity | Unchanged | The label selector is exact-match only; no expressions, CEL, or capacity selection. Equivalence with an OCM-io `Placement` is not claimed | Confirmed, declared gap | M-2, M-6; GAP-8 |
| **20 — taint/toleration** | Keep work off offline or maintaining clusters; tolerate a taint to allow it | Unchanged | No toleration model; taint-based selection unavailable | Confirmed, declared gap | M-6; GAP-11 |
| **30 — clusterset override** | Apply per-set defaults | Unchanged | Adoption may use set membership as its declared scope; no override semantics are mirrored | Confirmed in part | M-2, Z-1 |
| **Work, status, rollout, and addon operations** (29, 231, 229, 224, 10, 47, 132, 58, 123, 76, 167, 105, 141, 146, 14) | Create and propagate work, read work status, roll out and roll back, configure addons, manage namespaces, reach the spoke through the hub | Unchanged | Not bridged in the inventory increment: no `ManifestWork` is created, no status is mirrored, no OCM-io addon enablement is mapped or changed, and no placement decision is translated. The only OCM-io write in scope is the Fleet-origin registration declared in UC-F1, which uses OCM-io's own registration contract | Excluded, except Fleet-origin registration (UC-F1) | D-9, W-29, P-9; scope rows 5, 7, 8 |
| **8 — OCM-io addon framework** | Run addons on selected clusters | Unchanged | Optional packaging shape for the shim itself; Fleet's own addon framework is not mapped (G-15) | Confirmed in part | H-1 – H-3 |

The pattern is deliberate: the shim's confirmed scope preserves the OCM-io side of every story and claims Fleet-side parity only for the inventory facts it maps. A row that says "Excluded" is the boundary of the inventory increment, not a defect in the story. Placement rows state what Fleet cannot express rather than implying that a projection reproduces OCM-io selection.

None of the proposals mapped here concerns delivery provenance: the work agent records what it applied, not who authorized it. That absence is why delivery provenance is not a claim of the inventory increment.

---

## 8. What must not happen

| ID | Negative use case | Why | Requirement |
|---|---|---|---|
| **UC-N1** | The shim writes an OCM-io status field or condition | Co-owned or controller-owned fields cannot be merged safely | X-7, S-11 |
| **UC-N2** | The shim creates, updates, or deletes a `ManifestWork` in the confirmed scope | Workload intent is not in scope | D-9, W-29, P-9 |
| **UC-N3** | A target survives its cluster leaving inventory, or one removal deletes several targets | A stale or over-broad removal is worse than no adoption | M-8, M-9, GAP-9 |
| **UC-N4** | A stale label is presented as fresh, or an empty value replaces last-known data | Freshness must be declared, not guessed | S-12; GAP-12 |
| **UC-N5** | A claim or another OCM-io placement selector is projected as if it were an ordinary label without declaring the loss | Fleet cannot reproduce arbitrary OCM-io placement selection; claim semantics and expressions would be silently lost | M-6, Z-12; GAP-8 |
| **UC-N6** | A tainted cluster is silently selected in Fleet | The OCM-io placement would have excluded it | M-6; GAP-11 |
| **UC-N7** | Fleet creates, renames, or deletes OCM-io membership outside the declared registration flow, or fabricates a `ManagedCluster` with no registered cluster behind it | Membership belongs to OCM-io's registration authority; a placeholder makes OCM-io report a cluster it does not manage | F-1, M-3; GAP-23 |
| **UC-N8** | A tenant receives OCM-io credentials or sees another tenant's clusters | Confused deputy and tenancy leak | T-1, T-4, T-7 |
| **UC-N9** | An inventory observation or an OCM-io apply status is presented as authenticated | Neither carries a signer or a binding to the exact content | PA-1, PA-3; GAP-14 |
| **UC-N10** | The shim writes a field a GitOps applier owns, or injects metadata into an applier-owned object to track the interaction | A field the applier owns is restored on reconciliation, the shim becomes a second reconciler, and injected metadata is not a contract the applier honors | GO-1, GO-2, C-2; GAP-16 |
| **UC-N11** | A Git commit signature is reported as evidence that a Fleet target's labels were authorized, without a configured profile | It authenticates the commit, not the inventory facts Fleet holds | GO-5; GAP-15 |
| **UC-N12** | A GitOps pipeline or an OCM-io apply is blocked because the shim is down or uninstalled | The shim must not sit on another writer's critical path | GO-8 |
| **UC-N13** | Fleet trust anchors, signer enrollments, or verification policy are projected into OCM-io objects, or an OCM-io object is treated as a trust root for Fleet delivery | Trust state is Fleet-side; projecting it makes OCM-io a second trust authority and exposes signer material | PA-8 |
| **UC-N14** | The shim commits to Git or calls a GitOps controller API to change an object it does not own | It makes the shim a second writer and bypasses the review and convergence path the applier enforces | GO-1, GO-2 |
| **UC-N15** | A Fleet-origin target is reported as registered before OCM-io has a real `ManagedCluster` behind it, or an unregistrable target disappears from inventory without a state | Registration is real membership or it is nothing; silencing the difference makes both inventories wrong | F-1, M-3; GAP-23 |
| **UC-N16** | A Fleet addon, its capabilities, target types, extension resources, or UI/CLI plugin is presented as an OCM-io addon object or as OCM-io addon enablement, or an OCM-io addon is presented as a Fleet addon | The two addon systems share a word, not a mechanism; presenting one as the other is a false authority claim | G-15; scope rows 5 and 16 |

Each story above cites its requirements, design basis, and gaps where they apply; there is no separate traceability table to keep in sync.
