# OCM-io × OCM Fleet Shim — Requirements

Status: **requirements baseline; no implementation is included**. This document is normative. It states what introducing OCM Fleet to an existing OCM-io installation must satisfy; it does not select an architecture, sync algorithm, data model, deployment topology, or API. Every solution — baseline or enhanced — must satisfy every MUST here. Where the baseline cannot fully meet a MUST, [coverage.md](coverage.md) records a declared gap: the requirement stays in force, and the shortfall is reported rather than presented as success.

Background and rationale live in [analysis.md](analysis.md). Decisions, including the answers to the requirements questions, live in [decisions.md](decisions.md). Candidate baseline mechanisms live in [baseline-hypotheses.md](baseline-hypotheses.md), upstream options in [enhancements.md](enhancements.md), and the requirement-to-path mapping in [coverage.md](coverage.md). Concrete use cases and user stories are in [use-cases.md](use-cases.md); they illustrate these requirements and add none.

Identifiers are stable across revisions; gaps in the numbering are intentional.

**Review path.** §1 states the premise, §2 the constraints, §4 the confirmed scope, §6 the authority split, and §18 the provenance and GitOps rules; those five sections answer whether the shim is viable and honest. §4.2 states the confirmed increment and its declared losses on its own, so the normative case does not depend on the lower documents. §10 records ManifestWork semantics for a delivery scope that is not admitted by default, so it can be skipped until §4 admits delivery — except W-29, which forbids writing the ManifestWork family now. The complete limitation ledger is in [coverage.md §4](coverage.md#4-declared-gaps).

**Scope is deliberately partial.** The integration is not required to mirror OCM-io into OCM Fleet, or the reverse. It is required to map a small, explicitly enumerated set of aspects, chosen for value, and to make everything unmapped visibly unmapped. Cluster inventory (row 1) and Fleet-origin cluster registration (row 4) are the confirmed aspects; every other aspect is a candidate, excluded by default, or excluded until §4 changes. Fleet's own addon, extension, and plugin framework is excluded by default and is not the same thing as OCM-io addon enablement (row 16, G-15). Mapping more is not progress by itself.

Normative terms: **MUST** / **MUST NOT** are non-negotiable. **SHOULD** is expected unless a documented reason prevents it. **MAY** is optional. **OPEN** is not used in this revision: every requirement decision is recorded in [decisions.md](decisions.md) §2.

---

## 1. Premise

Two independently complete systems are being brought together, as incumbent and newcomer:

- **OCM-io** is an existing, installed Open Cluster Management deployment. It is not forked or patched for Fleet's benefit. Any upstream change Fleet benefits from must stand on its own as a general improvement and must remain optional (§1.1). Its hub is a Kubernetes API server holding `ManagedCluster`, `ManifestWork`, `ManagedClusterAddOn`, `Placement`, and related objects, reconciled by OCM-io controllers; its spokes run the klusterlet (`registration-agent`, `work-agent`) and addon agents.
- **OCM Fleet** is introduced alongside it. It has a resource manager server that owns its own APIs, persistence, authorization, and orchestration: `Fulfillment` (manifest × placement × rollout), targets, Fleet addons (its internal extension unit, not OCM-io addons, and not mapped to OCM-io; scope row 16, G-15), deliveries, inventory, platform and extension resources, and a designed tenant/workspace partition.

Both systems can express desired state, observe actual state, act on the same clusters, and report status. They are **not isomorphic**, and neither may be reshaped to fit the other. The baseline OCM-io must keep working unchanged; OCM Fleet can evolve, and OCM-io may gain general enhancements, but neither may force a change on the other's users.

The **shim** is the entire boundary surface: any component, configuration, controller, adapter, CRD, addon (including the shim-owned Fleet addon), agent, or operational procedure that causes state to cross between the two systems, in either direction. The requirements apply to it regardless of where it runs or what it is called. The candidate mechanism for the confirmed increment runs on the Fleet side — a controller that reconciles adopted inventory and a delivery-only Fleet registration provider — with no component inside OCM-io's control plane and no target-side shim component. Where Fleet delivers through a target-side component, the **fleetlet** is also a shim component (H-14); whether spoke-local state is visible then depends on the deployment shape, not on the boundary. The shim is not a separate proxy sitting between two unchanged systems.

Six framing facts shape the whole document. They are inputs, not solutions:

- **Direction is settled.** OCM-io is the installed system of record for the facts and objects it owns. Fleet is the newcomer, introduced to existing OCM-io users to add capabilities — not to replace registration, placement, or delivery that already works. No requirement in this document assumes Fleet displaces OCM-io for existing clusters.
- **OCM Fleet, or the part of it the shim needs, may be installed and operated as an OCM addon.** If so, its lifecycle, placement, health, and credentials come from OCM-io's addon mechanism — and the shim is itself delivered by the system it observes. This is permitted and expected; it is not a modification of OCM-io. It is also not required (§5).
- **The shim does not need to map everything.** Partial, asymmetric, and one-directional coverage is the normal shape. The design goal is the smallest set of aspects that produces the required outcomes, not parity.
- **ManifestWork is load-bearing even when it is out of scope.** It is the only desired-state API the klusterlet acts on, it is how OCM-io addons (including Fleet or the shim, when packaged as one) reach clusters, and its status and cleanup contract constrain every operation that touches OCM-io delivery. §10 treats it as a first-class subject even under an inventory-only scope.
- **The hub is a cluster too.** OCM-io can register its own hub as a `ManagedCluster` and install OCM-io addons on it. When that is done, the hub is a target like any other: where the shim has a spoke-side component, it runs there through the same addon mechanism, observation is local, and the same delivery and cleanup rules apply. Being the hub grants no exemption and no extra authority over OCM-io's own control plane (H-15, X-11).
- **Evidence belongs to a delivery, and authority belongs to a writer.** Fleet provenance is checked by a verifier and never attaches to an observed fact; OCM-io's work agent is not a verifier. GitOps is authoritative for the fields its desired state contains, and the shim never becomes a second writer of them. The two are separate: evidence is about deliveries, authority is about fields, and neither implies the other (§18).

The systems' structural differences, the benefits of introduction, and the ManifestWork contract that motivates the requirements are described in [analysis.md](analysis.md). They are inputs to this document, not requirements.

### 1.1 Compatibility baseline and permitted upstream changes

The shim MUST work against the following without requiring any change:

- OCM-io controller binaries and their reconciliation logic on the hub.
- Klusterlet components and addon agents on the spoke.
- The addon-framework and its lifecycle semantics.
- Existing CRD schemas, conversion behavior, and status subresource contracts.
- Existing hub RBAC, admission configuration, and API server settings that OCM-io depends on.
- Existing user manifests and GitOps pipelines that operate OCM-io objects.

**Supported baseline for this revision:** OCM-io v1.4.x, with v1.4.0 as the declared reference version and the klusterlet and addon-framework from the same release. Hub and spoke MUST be within one minor version of each other. Older minors are not claimed until validated; C-9 requires the shim to declare what it supports and to fail safe elsewhere.

Fleet is not version-pinned in this revision. The requirements are written against the Fleet interfaces observed in its source and running deployments — implemented delivery, inventory, and addon capability registration — not against its designed-only extension features. A release MUST declare the Fleet version and the supported Fleet/OCM-io combination (C-9).

This is the compatibility baseline, not a prohibition on OCM-io ever improving. A **general upstream enhancement** may be used when it passes C-15 through C-20. When it is absent, the shim MUST fall back to the baseline; the baseline remains authoritative for support, testing, and adoption. Installing a new addon, a new workload, or a new CRD that OCM-io does not already own is **not** a change to OCM-io, provided it is additive and removable (C-3, C-10).

### 1.2 Adoption invariants (AD)

- **AD-1** Introduction MUST be additive and opt-in. With Fleet removed, OCM-io MUST behave as it did before, and existing users MUST notice no difference.
- **AD-2** Fleet MUST NOT become the authority for any fact OCM-io already owns: registration, cluster identity, OCM-io addon enablement, placement decisions, or workload state. It consumes those facts; it does not replace them.
- **AD-3** Every claimed benefit MUST be independently adoptable. No benefit may require an aspect excluded by §4.
- **AD-4** Adoption MUST NOT regress an existing OCM-io capability, SLO, or operational procedure.
- **AD-5** Benefits MUST be measurable per increment: what improved, for which clusters, at what cost.
- **AD-6** The first increment MUST be useful with cluster inventory alone (F-10). A benefit that appears only after workload or delivery scope is admitted MUST NOT be presented as the reason to adopt.
- **AD-7** Removal MUST restore the pre-adoption state on the OCM-io side. Data Fleet owns may remain in Fleet; OCM-io objects and behavior must be as they were.

---

## 2. Constraints (C)

- **C-1** The shim MUST NOT fork, patch, or require modification of OCM-io source, binaries, CRDs, controllers, klusterlet, or addon-framework. It MUST function on the baseline without an OCM-io upgrade.
- **C-2** The shim MUST NOT inject annotations, labels, finalizers, owner references, or ad-hoc fields into existing OCM-io custom resources for tracking, correlation, or loop suppression. GitOps would treat them as drift (§18); other controllers may not tolerate them. It MAY use a first-class field introduced by a general upstream enhancement (C-15), but MUST NOT depend on one.
- **C-3** The shim MUST NOT change the schema of any existing OCM-io CRD. Any API surface it adds MUST be additive, separately named, and removable without altering OCM-io objects.
- **C-4** The shim MUST NOT require changes to user manifests, GitOps pipelines, or existing OCM-io operational procedures, and MUST NOT invalidate them.
- **C-5** Every write the shim makes into OCM-io MUST be a valid operation under OCM-io's existing schema, admission, and update semantics, indistinguishable in kind from a write by any other authorized client.
- **C-6** The shim MUST NOT assume it is the only writer to a cluster, a namespace, a `ManifestWork`, or a workload object. GitOps, OCM-io controllers, addon agents, and operators may all write.
- **C-7** Any target-side component the shim introduces to an OCM-io-managed cluster MUST be delivered and run through OCM-io's existing addon mechanism. This is the **expected** case wherever delivery is in scope, not an exception: the fleetlet is a shim component. A spoke-side shim component is part of the cluster it runs on and MAY read what its granted RBAC allows, including `AppliedManifestWork`, subject to W-17 and S-6; it MUST NOT grant itself privileges or write OCM-io's spoke-local bookkeeping. A **hub-only** shim MUST NOT assume it can observe spoke-local state that OCM-io does not expose, and the resulting gaps MUST be declared per aspect (Z-17); for cluster inventory it is complete, because those facts come from OCM-io's API, not the spoke. The shim itself MAY be addon-packaged (§5).
- **C-8** On targets reachable only through OCM-io, the shim MUST use OCM-io's delivery contract as-is. It MUST NOT require klusterlet behavior that OCM-io does not provide.
- **C-9** The shim MUST declare its supported OCM-io and Fleet versions, the supported combination, and version skew, and MUST fail safe, not corrupt state, on an unsupported version or combination.
- **C-10** The shim MUST be installable and removable without leaving OCM-io objects in an invalid, finalizer-blocked, or orphaned state.
- **C-11** Adoption MUST be opt-in and scoped. Installation alone MUST NOT begin mutating OCM-io objects or Fleet state.
- **C-12** The shim MUST NOT widen OCM-io's authorization model or require OCM-io users to hold privileges they do not already hold.
- **C-13** Treating the shim or Fleet components as an OCM addon MUST NOT be used to bypass any OCM-io contract. All addon-framework semantics — registration, install strategy, enablement, lease and health, removal — still apply.
- **C-14** Scope MUST NOT be assumed. No aspect is mapped by default, and no unmapped aspect may be approximated, synthesized, or implied.
- **C-15** Any upstream OCM-io change the shim relies on MUST be general, independently valuable without Fleet, additive, opt-in, backward compatible, and versioned. It MUST be useful to OCM-io users who never install Fleet.
- **C-16** The shim MUST work on baseline OCM-io without any such enhancement. Enhancements MAY improve fidelity; they MUST NOT be prerequisites for any mapped aspect, benefit, or scope.
- **C-17** The shim MUST detect whether an enhancement is present and MUST declare the degraded behavior when it is absent. A mode difference MUST be observable, never silent.
- **C-18** No enhancement may be proposed, shaped, or required solely to serve Fleet, and no OCM-io user may be forced to adopt one.
- **C-19** An enhancement MUST NOT change existing OCM-io authority, introduce a second system of record for an object OCM-io owns, or alter behavior for users who do not opt in. A GitOps applier owns the fields in its desired state, but it does not make the applier a second system of record for the object that contains them (GO-1, X-12).
- **C-20** Enhancement availability MUST be part of the declared support matrix (C-9). An unsupported combination MUST fail safe, not guess.

---

## 3. Required outcomes (F)

At minimum, the integration must make all of the following possible. Each is a functional requirement; how it is achieved is out of scope here.

- **F-1 Cluster inventory and Fleet-origin registration** — Clusters registered in OCM-io become Fleet targets through a defined correspondence, whether they were present at adoption or register later. A cluster known to Fleet and absent from OCM-io MUST be registered into OCM-io through OCM-io's own registration contract, so it appears as a `ManagedCluster` and the two identities are correlated; where the cluster cannot register, the target MUST remain visibly unregistered instead (GAP-23). The shim MUST NOT fabricate a `ManagedCluster` for a cluster it cannot register, and MUST NOT rename or delete membership it did not create; unregistration is ownership-aware.
- **F-2 Intent propagation** — Where an aspect is in scope, Fleet intent can be made effective through OCM-io's unmodified delivery path.
- **F-3 Observation propagation** — What OCM-io observes about a registered cluster becomes available in Fleet, within OCM-io's existing feedback surface. Where delivery is in scope and a target-side component is used, the fleetlet is a shim component and its observations are the expected path; observation MUST NOT require an agent mechanism outside OCM-io's addon framework, and an aspect that needs a spoke-side component MUST declare that dependency (Z-17).
- **F-4 Durable correlation** — Every cross-system relationship is recoverable after restart of either side, without relying on in-object metadata.
- **F-5 Conflict detection** — Concurrent or divergent intent from both sides is detected and surfaced, not silently resolved by ordering.
- **F-6 Safe lifecycle** — Create, update, delete, orphan, adopt, and roll back all have defined cross-system behavior, including when one side is unavailable.
- **F-7 Fidelity accounting** — For every mapped aspect and every Fleet capability, the integration declares whether it is preserved, approximated, or unavailable when OCM-io is the delivery or observation path.
- **F-8 Operability** — Operators can see sync state, lag, conflicts, coverage, and errors per cluster and per object from outside OCM-io's objects, and can quarantine a scope without disabling the whole integration.
- **F-9 Migration** — Pre-existing OCM-io objects can be adopted, and the integration can be removed or reversed, without downtime or data loss.
- **F-10 Minimum viable coverage** — A configuration that maps only cluster inventory MUST be a complete, supported mode of operation for the inventory facts in Z-12: no mapped fact is missing, and every approximation is declared. It does not reproduce OCM-io's selection or tenancy semantics. Its declared losses and costs are GAP-8, GAP-11, GAP-12, GAP-23, and GAP-24 (coverage §4).
- **F-11 Progressive coverage** — Additional aspects can be enabled independently, in any subset, without changing the behavior of already-enabled aspects.

---

## 4. Scope and coverage (Z)

The scope is a design output, not an assumption. Everything here governs *how much* is mapped and *how honestly* the boundary is presented.

- **Z-1** Default scope is empty. No aspect is mapped unless it is individually selected and recorded.
- **Z-2** Scope MUST be selectable per aspect and per direction. With the direction settled, the default for OCM-io-owned facts is OCM-io → Fleet; Fleet → OCM-io is admitted only for aspects Fleet legitimately introduces, such as registration of a Fleet-origin cluster. OCM-io addon enablement is not such an aspect: OCM-io is its authority in every dimension (AD-2, X-10; §4.1 row 5, §6 authority table). A Fleet addon is a different framework and is excluded by default (row 16, G-15). The two directions are never assumed to be symmetric.
- **Z-3** An aspect is eligible only if all of these hold: one side is the only reliable source for it; mapped consumers make decisions on it; no native path already lets those consumers read it; authority for the aspect is unambiguous (X); and translation fidelity is acceptable (F-7).
- **Z-4** An aspect MUST be excluded if any of these hold: it can be consumed natively across the boundary; it is co-owned or controller-owned and cannot be merged safely; its churn-to-value ratio is poor; it carries credentials or tenant-sensitive data that cannot be partitioned; or its meaning is lost beyond an acceptable threshold.
- **Z-5** Mapping MUST cover whole aspects. The shim MUST NOT map a fragment of an aspect in a way that makes the fragment misleading — for example, cluster health conditions without their reasons or freshness.
- **Z-6** Unmapped aspects MUST be representable as "not mapped" and MUST be distinguishable from empty, unknown, and not-applicable on every consumer surface.
- **Z-7** Coverage MUST be reportable per cluster, per aspect, and per direction, and MUST be visible to operators and to Fleet consumers.
- **Z-8** Scope expansion MUST be additive: enabling a new aspect MUST NOT require re-adopting existing objects, rewriting correlations, or taking downtime.
- **Z-9** Scope narrowing or disablement MUST stop new writes, MUST NOT delete data or objects on either side, and MUST leave both sides valid. It is not a Fleet placement exclusion: an existing target remains visible to Fleet placement, and the shim MUST report the target out of scope rather than claim that Fleet selection has stopped.
- **Z-10** A mapped aspect MUST NOT depend on an unmapped one being present. Cluster inventory coverage MUST be useful without workload, status, OCM-io addon, or placement coverage.
- **Z-11** Where the shim is OCM-io addon-delivered, coverage scope MUST be derivable from OCM-io addon enablement scope. The two MUST NOT disagree silently; a disagreement MUST be surfaced as a condition.
- **Z-12** "Cluster inventory" means the inventory *of clusters*: cluster identity, membership presence, and the labels and claims needed for placement. Claims used for placement MUST be projected into Fleet target labels, because Fleet placement matches exact label values only and cannot evaluate match expressions (M-6). Cluster taints are not projected: Fleet has no toleration model, so taint-based selection is declared unavailable rather than approximated (M-6, GAP-11). Cluster inventory does not, by itself, include health conditions, capacity, workload status, or resource inventory inside clusters; each of those is a separate aspect with its own decision.
- **Z-13** The shim MUST NOT map the same logical fact through more than one aspect. Overlapping coverage creates two authorities and two failure modes.
- **Z-14** Coverage and fidelity MUST be declared separately for the baseline and for each optional enhancement present.
- **Z-15** An enhancement MUST NOT change which aspects are in scope. It may only improve what a mapped aspect can promise.
- **Z-16** A change in enhancement availability MUST be observable and MUST NOT silently alter writes, status, or ownership.
- **Z-17** For every target and every mapped aspect, the shim MUST declare whether a spoke-side component is present and whether the aspect depends on it. An aspect that depends on one MUST report "not observed" while it is absent, MUST NOT report a stale value as current, and MUST NOT silently downgrade to a hub-side approximation.

An external writer such as a GitOps applier is not a mapping side. It changes the desired state of objects a mapped system stores; the shim observes the result and never writes the fields the applier owns (GO-1, GO-2). A future aspect that observes objects an external writer also writes MUST say so in its scope-register row.

### 4.1 Scope register

Cluster inventory (row 1) and Fleet-origin cluster registration (row 4) are approved. The rest are recorded so they are decided deliberately, not inherited by accident. "Excluded by default" means it must not be built unless a later decision reverses it.

| # | Aspect | Direction | Status | Reason | Authority |
|---|---|---|---|---|---|
| 1 | **Cluster inventory**: cluster identity, presence, labels/claims needed for placement | OCM-io → Fleet | **CONFIRMED** | Fleet cannot place work without knowing which clusters exist and how to select them; OCM-io is the only reliable source. Fleet matches exact label values only, so claims are projected into labels; taints and tolerations have no Fleet equivalent and are declared unavailable | OCM-io |
| 2 | Cluster health / availability conditions | OCM-io → Fleet | Candidate; excluded from the first increment | Useful for target status, but read-only mirror only; never written back | OCM-io |
| 3 | Cluster version / capacity / platform facts | OCM-io → Fleet | Candidate; excluded from the first increment | Version and capacity are baseline `ManagedCluster` status; platform and similar facts depend on addon- or claim-supplied information, so availability is uneven across installations | OCM-io |
| 4 | **Fleet-origin cluster registration**: Fleet target → OCM-io `ManagedCluster` | Fleet → OCM-io | **CONFIRMED** | A cluster introduced through Fleet must be visible in OCM-io inventory. Registration uses OCM-io's own registration contract, so OCM-io remains the registration authority; a target that cannot be registered stays visible as unregistered (GAP-23) | OCM-io for registration; Fleet for the target that identifies the cluster |
| 5 | OCM-io addon enablement state (`ClusterManagementAddOn` / `ManagedClusterAddOn`) as a Fleet-visible fact | OCM-io → Fleet | Excluded from the first increment | Needed only if OCM-io addon state is to be surfaced in Fleet as addon state; a Fleet addon is a different framework and is not its counterpart (row 16), and a `ManagedClusterAddOn` read as a generic Kubernetes object is not mapped addon enablement and does not create a Fleet addon; projecting OCM-io addon objects into Fleet would be a new decision under this row | OCM-io when mapped |
| 6 | Placement decisions (`Placement` / `PlacementDecision` ↔ Fleet placement) | Either | Excluded by default | Two placement authorities; Fleet placement stays Fleet's unless explicitly bridged | None |
| 7 | Workload intent (Fleet Fulfillment ↔ `ManifestWorkReplicaSet` / `ManifestWork`) | Both | Excluded by default | MWRS is the closest structural analogue and has built-in rollout, but lacks tasks, preemption, arbitrary gates, independently versioned strategies, non-Kubernetes, local, and Fleet addon-registered targets, and Fleet addon-generated content; its feature gate is alpha and off by default; not required for cluster inventory | Fleet for its own intent; OCM-io for the work objects |
| 8 | Workload status / feedback | OCM-io → Fleet | Excluded by default | Requires feedback rules; high churn; Fleet inventory is the better surface | OCM-io |
| 9 | Resource inventory inside clusters | OCM-io → Fleet | Excluded by default | OCM-io-collected in-cluster resource inventory and Fleet addon-reported inventory are different facts; neither is mapped by the shim. Fleet's inventory-reporting addons are Fleet-internal (row 16). Distinct from cluster inventory (row 1) | OCM-io for OCM-io-collected facts; Fleet for Fleet addon-collected facts; no shared field |
| 10 | Policy / governance objects | Either | Excluded | Different model entirely | None |
| 11 | Registration, CSR, bootstrap as a mapped aspect | OCM-io → Fleet | Excluded | OCM-io-only security surface; it must never be proxied or translated into Fleet. The declared Fleet-origin registration (row 4) drives that contract for a cluster the shim introduces and completes its approval step: it approves the bootstrap CSR and accepts the resulting `ManagedCluster` only where the installation does not automate those steps, using only the permissions the contract already defines (C-12). It exposes no registration credential or security surface to Fleet | OCM-io |
| 12 | Secrets and credentials | Either | Excluded | C-2, T-5 | None |
| 13 | Cluster deletion / deregistration | Both | Candidate; the full deletion lifecycle is deferred | Removing an adopted target when its cluster leaves inventory is an obligation of row 1 adoption (M-5, M-9), not a separate mapped aspect. Mapping the full deletion lifecycle — deregistration events, drain, teardown ordering — needs a later decision | OCM-io for membership; Fleet for its own records |
| 14 | Fleet platform resources unrelated to clusters | Either | Excluded | Not a cross-system fact | None |
| 15 | Hub cluster as a target (hub registered as a `ManagedCluster`) | OCM-io → Fleet | Confirmed in part — inventory adoption follows row 1; delivery to the hub is outside the first increment | A self-registered hub is an ordinary `ManagedCluster` and appears in inventory like any other, with its role visible. Workloads and OCM-io addons on it are subject to H-15 and X-11, which exclude OCM-io's control plane and the shim's own components | Fleet for delivered workloads; OCM-io for its control plane |
| 16 | Fleet addons, extension resources, and plugins (catalog, capabilities, delivery agents, UI/CLI extensions) | — | Excluded by default; no cross-system fact today | Fleet-internal framework with no OCM-io counterpart: a Fleet addon is a Fleet-internal capability bundle with an internal Defined → Enabled → Connected lifecycle and no runtime addon-registration API, not a `ClusterManagementAddOn` or `ManagedClusterAddOn`, and it has no per-cluster enablement. If the shim's registration capability is provided by a Fleet addon (candidate BF-10), M-8 requires it to be delivery-only: it exposes no extension resource or plugin to tenants and is not tenant inventory | Fleet |

**Scope boundary.** "Cluster inventory" means the inventory *of clusters* (row 1), not inventory *collected from clusters* (row 9). Row 1 is confirmed in the OCM-io → Fleet direction and row 4 in the Fleet → OCM-io direction; the latter registers a real cluster through OCM-io's own contract rather than mapping OCM-io's registration surface. Row 9 is excluded by default: OCM-io-collected in-cluster resource inventory and Fleet addon-collected inventory are different facts, and neither is mapped by the shim; Fleet's inventory-reporting addons are Fleet-internal (row 16). Row 16 is not a cross-system fact: it records Fleet's own addon, extension-resource, and plugin framework, distinct from both OCM-io addon enablement (row 5) and in-cluster resource inventory collected by a Fleet addon (row 9). A future decision to surface Fleet addon state would be a new aspect with its own authority decision, not a clarification of row 5.

**What binds now.** In the confirmed inventory-only increment, the delivery-only requirements are inactive: §8 D-1–D-8, §10 W-1–W-28, §11 P-3/P-5/P-8/P-10, the delivery row of §15, and the delivery cases of V-10 bind only when §4 admits delivery. D-9, W-29, and P-9 apply now and forbid emitting, owning, or deleting the ManifestWork family. §5 governs only the addon-packaging option. Every other section applies to the inventory facts, with the per-aspect conditions stated in §4 and the conditional outcomes stated in §3.

### 4.2 The confirmed increment

The confirmed increment has two parts, and they are not symmetric:

| Part | Direction | What crosses | What does not cross |
|---|---|---|---|
| Cluster inventory (row 1) | OCM-io → Fleet | Identity, membership presence, and the labels and claims used for Fleet placement, projected into exact-match target labels | Health, capacity, version, in-cluster resource inventory, OCM-io addon enablement |
| Fleet-origin registration (row 4) | Fleet → OCM-io | A real cluster registered through OCM-io's own contract, correlated to one Fleet target | Fabricated membership, a placeholder `ManagedCluster`, or a proxied registration surface |

The increment requires no OCM-io change, no enhancement, and no target-side shim component. A Fleet-side controller and a delivery-only Fleet registration provider are the candidate mechanism (BF-10); this document constrains their behavior (M-8, M-9) but does not require that decomposition. Fleet has no runtime addon-registration API — addons are compiled into the resource manager and enabled at startup — so the provider is part of the Fleet build. That build dependency is the only Fleet-side prerequisite, and it is a use of implemented Fleet delivery, not a mapping of Fleet's addon framework (row 16, G-15). No implementation is included in this directory.

These five declared losses shape the scope decision; a reader approving the increment is accepting them:

| Gap | What is lost | Consequence |
|---|---|---|
| GAP-8 | Fleet cannot reproduce OCM-io `Placement` selection: exact label equality only; claims lose claim-selector and expression semantics | Fleet selection is a projection, never an equivalent `Placement` |
| GAP-11 | Taints and tolerations have no Fleet equivalent | Taint-based selection is unavailable and must not be silently ignored |
| GAP-12 | Fleet has no target relabel or deregister API; refresh is a successor fulfillment, and target labels carry no freshness | One fulfillment per cluster, two transiently during a refresh; stale labels remain selectable, with staleness visible only in shim status |
| GAP-23 | Registration needs the cluster to run klusterlet and cooperate | A target that cannot register stays visibly unregistered; no placeholder |
| GAP-24 | Fleet's workspace partition is not implemented in the baseline | Inventory is provider-scoped; tenant-scoped visibility must be reported unavailable |

Other declared gaps also apply to inventory facts and are listed in coverage §4: uneven freshness (GAP-1), identity across re-registration (GAP-5), cleanup ownership (GAP-9), observed facts not attested (GAP-14), a Git commit reported as a source claim rather than provenance (GAP-15), and the lack of a declarative shim API (GAP-17). GAP-3 concerns cluster health, which row 1 excludes; GAP-16 concerns writes to GitOps-owned fields, which this increment does not perform. The remaining gaps concern delivery or other excluded aspects.

---

## 5. Hosting as an OCM addon (H)

OCM Fleet, or the part of it the shim needs on a managed cluster, may be packaged and operated as an OCM addon. These requirements govern that option. They do not make addon packaging mandatory.

- **H-1** The shim MUST be operable both addon-packaged and not. No requirement in this document may depend on the addon packaging choice, except where explicitly stated.
- **H-2** Addon packaging MUST use OCM-io's existing registration and enablement objects and MUST NOT bypass install strategy, `Placement`, or the addon manager.
- **H-3** Per-cluster enable/disable MUST be the unit of shim presence on that cluster. Disable MUST be a clean stop: stop new writes, release leases and ownership, retain data per U-4.
- **H-4** The addon's lease and conditions MUST reflect shim health and MUST be visible to OCM-io operators without opening the Fleet UI.
- **H-5** The shim's own delivery objects — the `ManifestWork` that creates it, its own addon registration, its own namespace — MUST be excluded from any mapping it performs, or handled by an explicit, tested rule. Exclusions MUST be keyed by object identity or a stable label the shim controls, never by kind alone. Self-observation MUST NOT produce loops. Where the hub cluster is itself a `ManagedCluster`, OCM-io's own control-plane workloads and the shim's own components MUST be excluded by the same rule.
- **H-6** The shim addon MUST operate with only the privileges OCM-io grants it. It MUST NOT require hub cluster-admin, addon-framework changes, or access to clusters where it is not enabled.
- **H-7** Credentials and configuration the addon needs to reach Fleet MUST be delivered through addon-sanctioned mechanisms, MUST NOT be placed in OCM-io custom resources, and MUST NOT be injected into existing objects.
- **H-8** Version skew between hub-side shim components and addon-delivered components MUST be supported within a declared window. Upgrades MUST be rolling and MUST NOT require touching OCM-io.
- **H-9** In disconnected or restricted environments, an unreachable Fleet MUST NOT fail the cluster or the addon manager. The addon MUST degrade, report, and resume.
- **H-10** Enabling the addon on a cluster MUST count as explicit opt-in for the scope defined in §4 — and MUST NOT implicitly enable any excluded aspect.
- **H-11** The shim MUST tolerate repeated addon reconciliation, `ManifestWork` updates, placement changes, and agent restarts without duplicating ownership or cleanup.
- **H-12** If Fleet itself is the OCM addon, rather than a small shim component, every other requirement still applies unchanged. Packaging MUST NOT change the answers to authority (X), scope (Z), or tenancy (T).
- **H-13** Removing or disabling the addon MUST NOT delete OCM-io objects or Fleet data outside the declared cleanup mode (U-4), and MUST NOT leave correlations that later resume writing.
- **H-14** The shim is not assumed to be hub-only. Where Fleet's fleetlet is installed on a target, it **is** a shim component, and every requirement on "the shim" applies to it. Its presence MUST follow the same addon lifecycle as any other shim component, and its absence MUST be declared per aspect (Z-17). A hub-only deployment is complete for cluster inventory, whose facts come from OCM-io's API; it is constrained only for an aspect that needs spoke-local observation, and those gaps MUST be declared.
- **H-15** The OCM-io hub cluster MAY be registered as a `ManagedCluster`. When it is, the shim MUST support installing its spoke-side component there through the same addon mechanism, and the ordinary delivery, observation, and cleanup rules apply. The shim MUST NOT infer privilege from the cluster being the hub, MUST NOT target OCM-io's own control-plane workloads without an explicit decision, and MUST NOT treat the hub's registration as automatic opt-in for any aspect.

**Packaging and connectivity.** Packaging is a deployment choice, not a requirement (H-1, G-12). The expected shape is fleetlet as an OCM addon on every target where Fleet runs a target-side component; the resource manager runs outside OCM-io's addon mechanism. The fleetlet opens one outbound connection to the resource manager and multiplexes the control, access, delivery, status, and index channels over it, so Fleet does not need an inbound connection to the spoke. A deployment with no fleetlet has no delivery, status, or index channel on spoke targets; Fleet's platform-side Kubernetes delivery agent can still apply content there with platform-side verification, and the resulting limits MUST be declared per aspect (Z-17).

---

## 6. Authority and ownership (X)

The central requirement. Everything else depends on it.

- **X-1** For every mapped aspect of every object, the shim MUST have exactly one declared authority at a given time. Authority MUST be explicit, inspectable, and stable across restarts.
- **X-2** Authority MUST be definable per aspect — spec, status, metadata, membership, lifecycle/deletion — not only per object. OCM-io splits these between users/GitOps, controllers, and the klusterlet; Fleet splits them between intent, fulfillment lifecycle, and Fleet addon-reported state.
- **X-3** The shim MUST NOT create a second writer of an aspect. An external writer such as a GitOps applier may already write it; the shim MUST detect the contention and surface it as a condition, not loop or flap (GO-1, GO-2).
- **X-4** The shim MUST preserve the semantics of "cluster is runtime authority" where OCM-io provides it: observed state from the cluster MUST NOT be overwritten by unconfirmed platform intent.
- **X-5** Authority transitions (adopt, change the active path for new intent, pause, roll back, release) MUST be explicit operations, MUST be reversible, and MUST NOT lose intent or observed history. A pause stops the shim's own writes for an aspect; it is not a pause of either platform's controllers (GAP-6).
- **X-6** The shim MUST NOT treat its own echo as external change, and MUST NOT rely on metadata injected into OCM-io objects to tell the difference (C-2).
- **X-7** Where OCM-io controllers own a field (for example `ManagedCluster.Status`, `ManifestWork.Status`, addon status), the shim MUST NOT write it except through a declared, compatible path, and MUST preserve values it does not own.
- **X-8** The authority model MUST be expressible per aspect and resource class, because the overlaps differ: cluster registration, workload manifests, OCM-io addon enablement, placement, and inventory are not the same problem.
- **X-9** Enabling a new aspect MUST include an authority decision for it. An aspect without a declared authority MUST NOT be mapped.
- **X-10** Fleet MUST NOT become authoritative for any fact OCM-io already owns. Introduction is additive (AD-2); the authority Fleet gains is over what Fleet introduces.
- **X-11** On a self-registered hub, Fleet authority over the cluster MUST NOT extend to OCM-io's control-plane components, their namespaces, or their configuration. Excluded by default: `open-cluster-management*`, `kube-system`, `kube-public`, `kube-node-lease`, the Fleet namespace, and the shim's addon namespace. Those objects are excluded from delivery, adoption, deletion, and cleanup unless a separate, explicit decision says otherwise; the same rule applies to the shim's own components.
- **X-12** Authority is never split within a single field: each field has at most one writer. If two writers would touch the same field, one MUST be declared the source and the other MUST write only its own derived records, outside OCM-io objects. A GitOps applier owns the fields in its desired state; the shim treats that field set as outside its write set rather than as a competing authority (GO-1, GO-2).

**Authority baseline.** Authority is fixed per aspect and per field class, not per object:

| Aspect | Intent / spec | Status / observation | Membership and lifecycle |
|---|---|---|---|
| Cluster inventory (confirmed) | OCM-io | OCM-io; Fleet values are projections | OCM-io |
| Fleet-created records: targets, placements, deliveries, inventory items | Fleet | Fleet | Fleet |
| Workload intent (excluded by default) | Fleet for Fleet-originated intent; OCM-io owns the work objects | OCM-io | Fleet for its records; OCM-io for work cleanup |
| OCM-io addon enablement (excluded from the first increment) | OCM-io | OCM-io | OCM-io |
| Fleet addon/extension state (excluded by default) | Fleet | Fleet | Fleet |

No field is co-owned by OCM-io and Fleet. Where both systems can express the same fact, one is the source and the other a projection; for every fact OCM-io already owns, OCM-io is the source (X-10, AD-2). An external applier may own a field set in an object OCM-io stores; that ownership is tracked separately and the shim never writes it (GO-1, GO-2). Delivery evidence is not an aspect shared with OCM-io: attestations, trust anchors, signer enrollments, and verification policy stay Fleet-side and are never projected into OCM-io objects (PA-8). The full decision is recorded in [decisions.md](decisions.md) §2.

---

## 7. Identity and correlation (I)

- **I-1** Every cross-system pair MUST have a durable, bidirectional correlation recoverable after restart, without modifying OCM-io objects.
- **I-2** Kubernetes UID MUST NOT be the Fleet-side identity. The integration MUST handle UID change, deletion and recreation under the same name, and objects that never had a UID on the Fleet side.
- **I-3** Name reuse MUST NOT cause a new object to inherit the identity, status, cleanup obligations, or history of a deleted predecessor.
- **I-4** Correlations MUST support 1:N and N:1. One Fleet intent commonly maps to one `ManifestWork` per cluster; one OCM-io cluster may appear as one Fleet target used by many fulfillments.
- **I-5** Correlation MUST be discoverable by an operator in both directions without reading OCM-io object metadata.
- **I-6** Orphan detection MUST work without a complete event history. If either side was down while the other deleted something, the shim MUST still converge.
- **I-7** Renames MUST be either fully supported or explicitly declared unsupported with a safe failure mode. OCM-io names are frequently immutable; Fleet names may differ.
- **I-8** Generated names MUST be deterministic, valid under OCM-io naming rules, and stable across retries and leader changes.
- **I-9** Correlation MUST record which aspect set it covers and which direction it serves, so partial coverage does not masquerade as full correlation.
- **I-10** A self-registered hub MUST be identified and correlated like any other `ManagedCluster`. The shim MUST NOT special-case it in identity, and MUST NOT assume the cluster hosting the hub is unreachable, privileged, or exempt from the ordinary rules.
- **I-11** The correlation unit is per aspect. For cluster inventory it is one OCM-io `ManagedCluster` to one Fleet target, keyed by a persisted identifier that is not the Kubernetes UID. For workload intent it is one Fleet intent to one delivery per target to one work object per target, with each manifest keyed by group, version, kind, namespace, and name.

---

## 8. Desired-state translation (D)

Applies only to aspects admitted by §4. The confirmed increment admits no desired-state aspect: D-1–D-8 are inactive, and D-9 binds (§4.2).

- **D-1** Fleet intent MUST be translatable into content OCM-io will apply correctly, preserving every semantic OCM-io needs: manifest identity, namespace, ordering assumptions, and per-manifest update strategy (`Update`, `CreateOnly`, `ReadOnly`, `ServerSideApply`).
- **D-2** Translation MUST be explicitly lossy-aware. Anything Fleet can express that OCM-io cannot represent MUST be declared, and MUST NOT silently degrade into different behavior.
- **D-3** Fleet MUST NOT accidentally change OCM-io drift-repair semantics or override a GitOps applier's correction behavior. `CreateOnly` and `ReadOnly` mean the platform will not force convergence; treating them as `Update` is a correctness violation. A repeated correction by another writer is drift to surface, not a write to win (GO-6).
- **D-4** Multiple Fleet intent sources MUST NOT produce conflicting instructions for the same concrete resource identity (group, version, kind, namespace, name) on the same cluster. Conflicts MUST be detected before apply.
- **D-5** Generation and version must be preserved across translation: a change on one side MUST be traceable to the corresponding change on the other, and a stale intent MUST never overwrite a newer one.
- **D-6** The shim MUST respect OCM-io object limits (size, name length, label/annotation validity) and MUST split or reject bundles that exceed them rather than produce invalid objects.
- **D-7** The shim MUST define behavior for manifest content it cannot faithfully generate: CRDs, cluster-scoped resources, namespaced resources outside the intended namespace, and objects with existing owners.
- **D-8** Where Fleet's manifest generation is Fleet addon-driven, invalidation or regeneration MUST produce a deterministic, explainable change set on the OCM-io side.
- **D-9** If workload intent is not in scope, the shim MUST NOT emit `ManifestWork` changes for it, including as a side effect of another mapping.

---

## 9. Observed-state and status translation (S)

Applies only to aspects admitted by §4.

- **S-1** Status MUST flow in the declared direction: OCM-io conditions and per-manifest status into Fleet inventory/delivery state, and Fleet state into OCM-io status only where OCM-io's consumers expect it and the field is not owned by an OCM-io controller.
- **S-2** Status MUST be merged by condition type or equivalent identity, never wholesale replaced. Unknown conditions and unknown fields MUST be preserved.
- **S-3** Every status value crossing the boundary MUST carry enough information to distinguish stale from fresh (observed generation, source, timestamp).
- **S-4** Status updates MUST be delta-only and deduplicated. High-frequency controller churn MUST NOT produce proportional database or API writes on either side.
- **S-5** The shim MUST NOT become the reason OCM-io misses its own freshness expectations (its status and apply requeue intervals). It SHOULD NOT add a dominant delay on top of them.
- **S-6** Observations requiring more detail than OCM-io's default feedback surface MUST be obtained in one of three ways: through OCM-io's existing feedback rules; through the shim's own spoke-side component, fleetlet where present, reading only what its granted RBAC allows (W-17); or declared unavailable. A hub-only component MUST NOT read spoke-local state directly.
- **S-7** Deletion and eviction progress MUST be observable in both systems; a deletion blocked on a finalizer on either side MUST be visible as such.
- **S-8** Inventory reported by one side MUST be identifiable as a projection, with its source and freshness, and MUST NOT be presented as authoritative if it is not.
- **S-9** A status field mapped read-only MUST remain read-only in every path. Read-only must not be a default that a later feature can silently override.
- **S-10** A status value crossing the boundary MUST carry its meaning as well as its value: existence, health, or completion, and the mechanism that produced it. The shim MUST NOT present an apply-path signal (`Applied`, `Available`) as health, and MUST NOT synthesize a health condition when no rule reports one.
- **S-11** The shim writes no OCM-io status field in the approved scope. A future write-back MUST name the field, show that no OCM-io controller owns it, and be admitted as a new aspect.
- **S-12** Inventory freshness MUST be declared and observable. In steady state a change MUST be visible within the budget in §15; during an OCM-io or Fleet outage the last known values MUST be retained, marked stale with the time they were last confirmed, and MUST NOT be deleted or replaced by an empty value.

---

## 10. The ManifestWork family (W)

ManifestWork is load-bearing even for a scope that excludes workload delivery: it is the only desired-state primitive the klusterlet work-agent acts on, it is how an OCM-io addon such as fleetlet reaches a cluster, and its finalizer contract governs cleanup. A shim that never writes one still has to read its semantics correctly; one that writes one inherits all of them.

The guarantees, non-guarantees, and the MWRS-versus-Fulfillment comparison are set out in [analysis.md](analysis.md#3-the-manifestwork-family-in-context), because they describe OCM-io, not a requirement on the shim. The resemblance between an MWRS and a Fulfillment is shallow; treating the two as equivalent is the most likely integration error.

Nothing here changes §4. Delivery is outside the confirmed increment (§4.2), and workload delivery remains excluded by default. The section records the contract so that decision can be made with the real semantics in view, and so the requirements are already stated if the scope is later expanded.

### 10.1 Requirements (W)

These apply only if the corresponding aspect is admitted by §4. They are stated here so the admission decision is informed.

**Ownership and write paths**

- **W-1** ManifestWorks generated by an MWRS MUST be treated as owned by OCM-io's controller. The shim MUST NOT edit their spec, delete them individually, or add manifests to them. Child ownership is expressed by labels and an annotation, not by owner references; the shim MUST NOT infer control from, or add, owner references.
- **W-2** The shim MUST NOT create both an MWRS and direct ManifestWorks for the same cluster and manifest set. Exactly one creator per work object.
- **W-3** An MWRS or ManifestWork the shim did not create MUST be treated as externally owned. Adoption requires an explicit binding, MUST NOT rewrite its spec, and MUST be reversible.
- **W-4** The shim MUST NOT depend on OCM-io's generated child names or naming conventions for correlation. Any name it generates itself MUST be deterministic, within API limits, and stable across retries.
- **W-5** Direct ManifestWork creation and MWRS-based fan-out MUST be separate, declared write modes, never mixed within one intent.
- **W-6** If workload delivery is out of scope, the shim MUST NOT create or modify either API, including as a side effect of another mapping.

**Semantic fidelity**

- **W-7** A single MWRS MUST NOT be presented as an equivalent of a Fulfillment. The fidelity matrix (F-7) MUST state, per Fulfillment semantic, whether it is preserved, approximated, or unavailable — including three independently versioned strategies, rollout groups, preemption, Fleet addon-generated manifests, and non-Kubernetes, local, and Fleet addon-registered targets.
- **W-8** The shim MUST NOT present an MWRS rollout strategy as equivalent to Fleet rollout groups. MWRS has `All`, `Progressive`, and `ProgressivePerGroup` strategies, but advancement requires `Progressing` conditions with an observed generation matching the rollout; without them a progressive rollout never completes. The default strategy is `All`, and `Progressive` with `maxConcurrency` unset also applies to all clusters in one wave; `maxFailures` defaults to zero. Any staging, soak, or failure tolerance the shim promises MUST be implemented and tested by the shim itself.
- **W-9** Fulfillment generation and MWRS generation MUST NOT be treated as interchangeable. The shim MUST maintain a declared lineage from each Fleet intent change to the corresponding OCM-io spec change, and back, sufficient for audit and rollback.
- **W-10** The shim MUST preserve update strategies and feedback rules exactly. It MUST NOT add, remove, or rewrite `manifestConfigs` to gain observation, drift repair, or ordering.
- **W-11** The shim MUST preserve `deleteOption` semantics and MUST NOT choose a propagation policy implicitly. Every Fleet-side delete MUST map to a declared OCM-io behavior, including `Orphan` and `SelectivelyOrphan`.
- **W-12** The shim MUST NOT assume atomicity or ordering within a work object or across the child works of one intent, and MUST NOT depend on apply order for correctness.
- **W-13** The shim MUST validate object size and manifest count before writing, and MUST reject or split rather than emit objects OCM-io will refuse.

**Observation**

- **W-14** `Applied` and `Available` MUST remain distinct and MUST NOT be presented as health. `Applied` reports that the last apply attempt produced no error. `Available` reports that the object exists; it says nothing about readiness, replica count, or liveness. Health or completion is reported only when a condition rule produces it, and MUST be labelled as that rule's result, not as the meaning of `Available`.
- **W-15** The shim MUST distinguish three cases and report them differently: no feedback or condition rule is configured; a rule is configured but has not reported; a rule reports a value. The first two MUST be reported as "not reported" with the reason, never as healthy or failed. Absence of a condition is not a negative condition.
- **W-16** Per-manifest status MUST be correlated to Fleet manifest intent by a stable key, never by list position.
- **W-17** Cleanup state MUST come from a declared, authorized source.
  - A spoke-side shim component — the fleetlet, wherever one is installed, including on a self-registered hub — reads `AppliedManifestWork` and the resources it names. The object identifies the work, hub hash, and agent, and lists applied resources by group, resource, namespace, name, uid, and version. Its creation time marks when the ledger entry was created, not when the current spec was applied; an applied-resource `uid` is refreshed from the live object when the ledger reconciles, so it records the incarnation last observed and is not an apply-time marker. Status carries `evictionStartTime` when the work is missing from the hub or the hub hash has changed; that is an eviction signal, not an apply timestamp. The shim MUST report the time it read the record and its own version alongside it.
  - The RBAC granted to that component through its OCM-io addon MUST be limited to cluster-scoped read (`get`, `list`, `watch`) of `appliedmanifestworks.work.open-cluster-management.io` and the namespaced reads it needs. The component MUST NOT write or delete `AppliedManifestWork`, and MUST NOT treat its read as a hub-visible record.
  - A hub-only shim MUST NOT scrape spoke-local `AppliedManifestWork`. It MAY consume cleanup state OCM-io surfaces through hub-side status; anything not surfaced is unobservable to it and MUST be declared.
  - Either path MUST report state as unavailable while the observing component is not running, and MUST NOT present stale state as current (S-3).
- **W-18** MWRS status is an aggregate: `summary` and `placementSummary` counters plus the `PlacementVerified`, `ManifestworkApplied`, and `PlacementRolledOut` conditions. Child ManifestWorks carry a placement-name label, an owner-key-hash label, and an owner annotation instead of owner references; the deprecated MWRS-name label is present only when the `namespace.name` value fits the 63-character label limit. The shim MUST identify them by that metadata. If Fleet needs per-cluster state, the shim MUST read the child ManifestWorks and MUST handle children that are not yet created or are terminating. A counter the controller does not populate MUST be reported as unknown, not as zero.
- **W-19** Aggregate counts MUST NOT be expanded into per-target conditions, and per-target conditions MUST NOT be collapsed into counts without declaring the loss.

**Placement ownership**

- **W-20** For an MWRS it creates, the shim MUST NOT select clusters independently of OCM-io `Placement`, and MUST NOT edit `Placement` or `PlacementDecision`.
- **W-21** Placement changes MUST be treated as OCM-io actions. When a cluster is deselected, the shim MUST reconcile its own records to the child being removed and MUST surface the resulting intent change rather than silently losing a Fleet target.
- **W-22** Transient cluster unavailability MUST NOT be interpreted as deselection. Placement decision changes and status noise MUST be distinguished.

**Security and versioning**

- **W-23** The shim MUST NOT encode tenant identity or Fleet correlation into names, labels, or annotations of OCM-io objects. Tenant scoping stays in Fleet.
- **W-24** Fleet authorization MUST NOT be sufficient to create or edit an MWRS or ManifestWork unless OCM-io RBAC separately permits the mapped identity (T-7).
- **W-25** The shim MUST declare the ManifestWork and ManifestWorkReplicaSet API versions, feature gates, and fields it relies on, and MUST fail safe on unknown versions or fields. The `ManifestWorkReplicaSet` gate is alpha and off by default in the supported baseline; the shim MUST detect it and MUST NOT assume it is enabled.
- **W-26** MWRS MUST NOT be required for any core outcome while it remains an alpha or beta API. A declared fallback — direct per-cluster ManifestWork, or no fan-out where delivery is out of scope — MUST exist and be tested.
- **W-27** A `Complete` condition stops reconciliation: work-level `Complete=True` freezes the whole work, and a per-manifest `Complete` freezes that manifest, until the condition no longer holds. The shim MUST NOT set a completion condition on intent it may later change, and MUST NOT treat `Complete=True` as evidence that a later edit was applied.
- **W-28** Under `ServerSideApply` with `force=false` (the default), a field owned by another manager produces `Applied=False` with a message naming the conflict. The failure is recorded per manifest and aggregated into the work-level `Applied`; the work can remain `Available` from an earlier successful apply. The shim MUST surface the conflict (F-5), MUST NOT read the surviving `Available` as convergence, and MUST NOT silently retry with force unless a declared ownership decision permits it. Force MUST NOT be used over a field an external applier such as GitOps owns (GO-1).
- **W-29** In the approved inventory-only scope the shim writes none of the ManifestWork family. It may read the work object that delivers its own addon, and that object is excluded from mapping (H-5); no other work object carries a mapped fact.

---

## 11. Delivery-path interaction (P)

Applies to every mapped aspect through the path that carries it. In the confirmed increment only cluster inventory is mapped, and it is projected into the target record rather than delivered: P-3, P-5, P-8, and P-10 do not bind, and the remaining rules apply where they can (§4.2). P-9 binds now: the shim writes no `ManifestWork` for workload intent.

- **P-1** For each target, steady state MUST have exactly one active delivery path for each mapped aspect that is delivered to the cluster: OCM-io klusterlet/`ManifestWork`, or a Fleet delivery agent (platform-side, or through a fleetlet). Any dual-path period MUST be explicit, bounded, and surfaced as a condition. Cluster inventory is not delivered to the cluster: it is projected into the target record through a declared adoption path that satisfies M-8, and that projection is its only path. This document does not select the mechanism; [baseline-hypotheses.md](baseline-hypotheses.md) BF-10 is a candidate.
- **P-2** The shim MUST NOT assume it or either platform is the only actor on a workload. GitOps, operators, and third-party controllers may also mutate it.
- **P-3** A stale delivery attempt MUST NEVER win over a newer intent, regardless of which path carried it. Fleet generation fencing and OCM-io replay convergence MUST NOT interleave into an out-of-order apply.
- **P-4** Removal MUST be ownership-aware. The shim MUST NOT delete a resource still claimed by the other path, by GitOps (GO-1), or by another Fleet intent.
- **P-5** A change of active delivery path MUST be safe when it is limited to intent introduced after the transition or to an aspect with no workload under the outgoing path: no intent gap, no double apply, no double delete, no orphaned `AppliedManifestWork`, and no orphaned Fleet delivery records. Moving work OCM-io already delivers is out of scope (P-11).
- **P-6** The shim MUST function with either side unavailable, within declared bounds, and MUST converge without operator intervention when both return.
- **P-7** All cross-boundary operations MUST be idempotent and safe under at-least-once delivery and replay.
- **P-8** The shim MUST preserve OCM-io's cleanup contract: `ManifestWork` finalizers must complete, `AppliedManifestWork` must not be orphaned, and deletion propagation options must be honored.
- **P-9** If workload delivery is out of scope, the shim MUST NOT create, update, own, or delete `ManifestWork` objects for workloads. Its presence MUST be invisible to the OCM-io delivery path.
- **P-10** A self-registered hub MUST obey the same single-path rule as any other target. OCM-io's own control-plane components are never a delivery target, and a `ManifestWork` that carries the shim's own addon MUST NOT be treated as workload intent.
- **P-11** Coexistence is indefinite and per aspect: OCM-io keeps delivering the work it already delivers, and Fleet delivers only work Fleet introduces. No cutover of existing workloads is required or supported.

---

## 12. Membership and placement (M)

- **M-1** Membership changes on either side (join, leave, deregistration, target removal) MUST be reflected with defined semantics and MUST NOT be inferred from transient reachability.
- **M-2** Placement decisions from OCM-io (`ManagedClusterSet`, `Placement`, addon install strategy) and Fleet (pool, placement strategy) MUST NOT fight. When both select a target, the outcome MUST be deterministic and explainable.
- **M-3** A cluster known to OCM-io but absent from Fleet, and a target known to Fleet but absent from OCM-io, MUST both be representable and visible. The shim MUST NOT fabricate membership in either direction: registering a real Fleet-known cluster through OCM-io's registration contract is not fabrication, while a `ManagedCluster` with no registered cluster behind it is.
- **M-4** A target becoming unreachable MUST NOT cause declarative placement to shrink or workloads to be removed.
- **M-5** Cluster deletion MUST have a defined cross-system behavior: whether workloads are cleaned up, orphaned, or retained, and how the other side records it.
- **M-6** Labels and cluster claims that OCM-io exposes MUST be available to Fleet placement when relevant, without copying them into mutable hub fields. This applies to targets adopted from OCM-io inventory. A Fleet-origin target is Fleet's own record: row 4 registers the cluster into OCM-io but does not adopt its OCM-io inventory back, so labels and claims acquired on the `ManagedCluster` after registration are not projected onto the target (GAP-12). Because Fleet placement matches exact label values only — its selector is a conjunction of `key=value`, with no match expressions — claims used for selection MUST be projected into Fleet target labels; the projection MUST preserve key and value and MUST declare what it loses, including OCM-io `claimSelector` semantics, `matchExpressions` selection, and the distinction between a claim and a label. Where a claim name collides with an existing label key, the projection MUST apply a declared, deterministic precedence and MUST report the collision; the value MUST NOT alternate between refreshes. Cluster taints and `Placement` tolerations have no Fleet target equivalent: the shim MUST either expose them through a declared approximation with its loss stated, or declare taint- and toleration-based selection unavailable, and MUST NOT silently ignore a taint that an OCM-io placement would honor (GAP-11). Equivalence with an arbitrary OCM-io `Placement` MUST NOT be claimed: selection kinds Fleet cannot express remain OCM-io-only and are declared (GAP-8).
- **M-7** If Fleet targets are derived from cluster inventory, target identity MUST be stable across OCM-io label and claim changes of the same cluster. A cluster deleted and re-registered under the same name is a new incarnation: it MUST get a new target and MUST NOT inherit the predecessor's identity, cleanup obligations, or history (I-3), unless a declared stable cluster identity proves it is the same cluster (GAP-5).
- **M-8** An adopted target MUST NOT be owned by any fulfillment that carries workload intent, and deleting an unrelated fulfillment MUST NOT remove it. The shim's registration unit — the fulfillment whose output produces adopted targets, if one is used — MUST be dedicated to that purpose, carry no workload for any managed cluster, and be excluded from the mappable set and from tenant-visible inventory. Where the registration capability is provided by a Fleet addon, that addon MUST be delivery-only: it MUST NOT declare a managed-resource, inventory-resource, UI, or CLI capability, so it exposes no consumer-visible extension resource or plugin. Deleting the registration unit is the declared un-adoption operation, and the shim MUST declare which targets it removes. Un-adoption MUST be durable across reconciliation: a target removed this way MUST NOT be silently re-created while its cluster remains in scope, and re-adoption MUST be an explicit operation.
- **M-9** Every adopted target MUST have a declared update path for the labels and claims Fleet placement uses, and a declared removal path for when its cluster leaves inventory. A target whose labels cannot be updated, or that cannot be removed, MUST NOT be registered; declining to register is preferable to leaving a stale or unremovable target selectable. The removal unit and its blast radius MUST be visible to the operator before removal (GAP-12).

---

## 13. Tenancy, authorization, and security (T)

- **T-1** Fleet's tenancy model partitions a provider from consumer tenants with a workspace tree; OCM-io has no such partition. The shim MUST NOT leak one Fleet tenant's intent, observations, names, or errors into another, and MUST NOT expose one tenant's resources to OCM-io users who could not already see them.
- **T-2** If two Fleet tenants reference the same OCM-io cluster or object, ownership and conflict behavior MUST be explicit. Shared OCM-io namespace is not a synchronization mechanism.
- **T-3** Every write the shim makes MUST be attributable to a principal and recorded in an audit trail, including the source object and generation that caused it.
- **T-4** The shim MUST NOT impersonate users, and MUST NOT introduce credential modes that bypass OCM-io's authorization. Spoke access continues to follow OCM-io's existing registration and addon credential model.
- **T-5** Credentials and secrets for cross-system operation MUST NOT be stored in OCM-io custom resources, annotations, or other surfaces OCM-io does not intend for configuration.
- **T-6** Provenance and attestation claims (§18) MUST NOT be overstated. Where OCM-io is the delivery path, the shim MUST declare which Fleet provenance or authorization guarantees are preserved and which are not enforceable by klusterlet.
- **T-7** The shim MUST NOT become a confused deputy: a principal authorized on one side MUST NOT gain authority on the other beyond what its mapped identity allows.
- **T-8** Fleet's tenant/workspace authorization MUST be evaluated before any OCM-io write, and OCM-io's authorization MUST be evaluated independently; passing one MUST NOT imply passing the other.
- **T-9** Cluster inventory is installation-wide on the OCM-io side: OCM-io has no tenant partition of it. Making it available in Fleet MUST NOT expose one tenant to clusters outside its workspace, and MUST NOT let a tenant infer the existence of clusters it cannot access. Where the deployed Fleet baseline does not yet enforce workspace grants, the shim MUST treat tenant-scoped visibility as unavailable and MUST NOT expose inventory to a tenant on the assumption that Fleet will enforce the partition (GAP-24).
- **T-10** A self-registered hub MUST NOT be offered to a consumer tenant by default. Using the hub cluster as a target requires an explicit provider decision, and the exclusions in X-11 MUST be enforced regardless of which tenant requests the delivery.

**Tenancy baseline.** Fleet tenancy is a Fleet-side partition (workspace) and has no counterpart in OCM-io; it MUST NOT be encoded into OCM-io objects (T-1, W-23). The shim acts as a provider principal: Fleet authorization is evaluated for the requesting tenant, and OCM-io authorization is evaluated independently for the shim's own mapped identity. No Fleet tenant receives OCM-io credentials, impersonation, or RBAC. A tenant sees a cluster only when a target for that cluster is granted to its workspace, so cluster visibility is per-workspace rather than global to consumers. The workspace partition is a design goal of the Fleet platform; where a deployed baseline does not implement it, the shim operates at provider scope and reports tenant-scoped visibility unavailable rather than inferring isolation (GAP-24). The full decision is recorded in [decisions.md](decisions.md) §2.

---

## 14. Reliability, failure, and data integrity (R)

- **R-1** No accepted intent may be lost. A change MUST be durable on its originating side before the shim acknowledges or propagates it.
- **R-2** The integration MUST converge after either side restarts, after a network partition, and after an extended outage. No event replay history may be assumed.
- **R-3** Duplicate events, duplicate deliveries, and repeated reconciles MUST be idempotent.
- **R-4** Partial failure MUST be per-target observable. One failing cluster MUST NOT block or distort the result reported for others.
- **R-5** The shim MUST NOT assume or require cross-system transactions. Its consistency guarantee MUST be stated explicitly (eventual, replayable convergence) and tested as such.
- **R-6** The shim MUST bound its queue and memory, apply backpressure, and MUST NOT thundering-herd the hub API server, the Fleet server, or the database after recovery.
- **R-7** Behavior under clock skew, API throttling, admission rejection, and conflict (`409`) MUST be defined and MUST NOT corrupt correlation or ownership.
- **R-8** If more than one shim instance can run, concurrency MUST be safe: no double apply, no lost update, no duplicated cleanup, no split-brain authority.
- **R-9** A delete that cannot complete (finalizer, unavailable cluster) MUST remain visible and resumable, never silently dropped.
- **R-10** Archived or soft-deleted records MUST retain enough identity to detect resurrection and to refuse inheriting old cleanup obligations (see I-3).
- **R-11** Failure or disablement of the shim MUST NOT make either system dependent on it for its own correctness. OCM-io must keep working without the shim; Fleet's other functions must keep working without the mapped aspects.

---

## 15. Scale and performance (N)

- **N-1** The integration MUST state the scale it supports — clusters, objects per cluster, concurrent fulfillments, event rate — and MUST be validated at that scale before being considered done. The declared profile is the table at the end of this section.
- **N-2** Hub API load MUST be bounded. Per-object polling, per-event full list, and unbounded watches MUST NOT be required for correctness.
- **N-3** Database write amplification MUST be bounded; status-only churn MUST NOT produce spec writes, generation bumps, or strategy versions.
- **N-4** Propagation latency budgets (top-down and bottom-up, normal and degraded) MUST be declared and measured.
- **N-5** Under overload the shim SHOULD shed non-critical status/observation work before intent or deletion work, and MUST report that it is shedding.
- **N-6** The shim MUST remain correct when a Fleet intent fans out to a large number of OCM-io objects, and when one OCM-io cluster is referenced by many Fleet intents.
- **N-7** Cost MUST scale with mapped scope. An inventory-only configuration SHOULD NOT incur watch, storage, or API load proportional to workload volume.

**Declared scale profile.** N-1 and N-4 are validated against these numbers before a release:

| Profile | Targets | Inventory / deliveries | Freshness budget |
|---|---|---|---|
| Inventory-only | 2,000 | 10,000 label or claim changes per hour sustained, with bursts up to 1,000 in any minute provided the hourly total is not exceeded; one registration fulfillment per target in steady state, two transiently during a refresh; 50 concurrent status updates | Change observed within 30 s p95 and projected to Fleet placement labels within 60 s p95; full convergence within 10 min after an outage |
| Delivery scope | 500 | 10,000 deliveries; 20 concurrent rollouts | Delivery accepted within 60 s p95; rollout progress reported within 60 s |

These numbers are a test profile derived from Fleet's published design assumptions (50, 500, and 2,000 target profiles), not a vendor limit. Raising them requires a new measurement, not a new promise. The full decision is recorded in [decisions.md](decisions.md) §2.

---

## 16. Compatibility, lifecycle, and operations (U)

- **U-1** The shim MUST tolerate OCM-io API version skew and conversion behavior, and MUST NOT pin a version it cannot move away from.
- **U-2** Shim and Fleet upgrades MUST be rolling and MUST NOT require touching OCM-io. In-flight cross-system operations MUST survive the upgrade or be safely retried.
- **U-3** Fleet-side schema or capability evolution MUST NOT produce invalid OCM-io objects; unknown-field rejection MUST be handled explicitly.
- **U-4** Uninstall MUST stop writes, release ownership, complete or release pending cleanup, and leave both sides valid. A documented "leave objects in place" mode MUST exist and is the default; deletion of Fleet records or OCM-io objects requires an explicit choice.
- **U-5** Backfill/adoption of existing OCM-io objects MUST work without rewriting them and without a maintenance window.
- **U-6** Scope selection (clusters, namespaces, labels, resource types, aspects) MUST be explicit and MUST be excludable per object, so a single misbehaving scope can be quarantined.
- **U-7** Coexistence with GitOps ownership MUST follow GO-1, GO-2, and GO-6: the shim MUST NOT fight server-side apply ownership, and repeated correction of shim writes MUST be attributed and surfaced as drift rather than met with a forced or escalating write.
- **U-8** The shim MUST document which OCM-io operations remain available, degraded, or unavailable while it is installed.
- **U-9** Enabling or disabling an aspect MUST be an operational action with a defined ordering, not a config flag whose effect depends on restart order.

---

## 17. Observability and verification (O, V)

- **O-1** Sync state per object and per cluster (in sync, pending, conflict, blocked, failed, not mapped) MUST be queryable from Fleet without modifying OCM-io objects.
- **O-2** Metrics MUST cover lag, error rate, conflict count, queue depth, write rate, and drops, labeled by cluster and scope.
- **O-3** Logs and events MUST carry correlation identifiers linking the OCM-io object and the Fleet resource.
- **O-4** A dry-run/explain capability SHOULD show what the shim would write and why.
- **O-5** Every write MUST be auditable with actor, source generation, and resulting generation.
- **O-6** Health and readiness MUST distinguish "shim healthy" from "converged" and from "covered".
- **O-7** Coverage and fidelity MUST be reportable in the same place as health, so an operator cannot mistake partial scope for a failure or a full sync.

Verification of the whole document:

- **V-1** Every requirement MUST have at least one automated test or an explicitly accepted manual check, traceable by ID.
- **V-2** Tests MUST run against a real OCM-io hub and spoke and a real Fleet instance, not mocks alone, for anything touching the boundary.
- **V-3** A conformance check MUST prove OCM-io is unmodified: no CRD schema changes, no injected metadata on OCM-io objects, no new required fields, no rejected writes attributable to the shim.
- **V-4** Chaos tests MUST cover: shim down during delete, hub down, Fleet down, cluster unreachable, duplicate events, leader change, clock skew, and API throttling.
- **V-5** Conflict tests MUST be deterministic and MUST prove no silent last-writer-wins for co-owned aspects.
- **V-6** Load tests MUST run at the declared scale in N-1 and record the latency budgets in N-4.
- **V-7** Upgrade and uninstall MUST be tested from every supported starting state, including a partially converged one.
- **V-8** Scope tests MUST prove that a disabled aspect produces zero reads and zero writes, and that an inventory-only configuration passes the full suite for its declared scope: label and claim refresh within the freshness budget; un-adoption that removes exactly the one target whose current registration fulfillment was deleted; a Fleet-origin cluster registered through OCM-io's contract appearing as one correlated `ManagedCluster`; and a target that cannot be registered remaining visibly unregistered with no fabricated `ManagedCluster` (M-8, M-9, GAP-23).
- **V-9** OCM-io addon-hosted deployments MUST be tested for enable, disable, re-enable, placement change, agent restart, addon-manager reconciliation, and uninstall.
- **V-10** Provenance tests MUST cover each declared verification locus: target-side rejection of tampered content, a wrong generation where the signer pins one, and an expired attestation; platform-side rejection of external forgery without claiming resource-manager resistance; the declared downgrade when no verifier exists; provenance-absent reporting for an unsigned deployment; the paused state when a delivery is held for fresh credentials or its target's trust bundle cannot be loaded; the partial per-delivery record where Fleet exposes no durable delivery read, including what the live event stream does and does not provide, and the declared limits on removal evidence, historical verification, and output binding to a filtered manifest set.
- **V-11** GitOps tests MUST prove that the shim writes no GitOps-owned field, that a delivery conflicting with another field manager fails without forcing the apply and names that manager where the conflict detail is observable, that repeated correction is surfaced as drift within the declared bound, that a revert is reconciled as a normal change, that applying a declared shim record twice is safe and that deleting one removes exactly the unit it describes, that every imperative-only surface is declared as a limitation, and that GitOps applies succeed with the shim down or uninstalled.

---

## 18. Provenance and source authority (PA, GO)

Fleet provenance is cryptographic evidence that a principal authorized an exact delivery. A signer produces it, the resource manager carries it, and a verifier checks it before apply — at the target or on the platform; the resource manager stores and couriers evidence but is not its author. It is a delivery capability, not a property of inventory. OCM-io has no verifier for it: the work agent records what it applied, not who authorized it or a digest of the content. GitOps is a writer and often the authority for the objects it applies, but a Git commit is not provenance by itself. The requirements below keep those claims separate.

- **PA-1** Every mapped aspect MUST declare, for each fact it carries, an evidence class: authenticated delivery evidence for a delivery action, an observed fact with source and freshness for external state, or no evidence. An observed fact MUST NOT be presented with, or inherit, the provenance of a delivery that carried a projection of it; a downgraded delivery is reported as observed, with the missing verification named.
- **PA-2** Every delivery path the shim uses MUST declare where evidence is verified — target-side, platform-side before apply, or not at all — and what assurance that locus provides. Verification by the resource manager MUST NOT be described as protection from a compromised resource manager.
- **PA-3** Where OCM-io's work agent performs the apply, Fleet provenance is not verified by the applier. The shim MUST mark the delivery provenance-downgraded, MUST NOT describe `Applied` or `Available` as authenticated delivery, and MUST NOT synthesize evidence from OCM-io status.
- **PA-4** Evidence MUST bind the exact content delivered and the principal that authorized it. If the shim projects Fleet intent into another representation, it MUST retain the original evidence identity and content binding in its own records, and MUST NOT re-sign or synthesize evidence on a producer's behalf.
- **PA-5** Removal and withdrawal MUST carry evidence equivalent to the delivery they reverse, and that evidence MUST remain queryable for as long as the delivery record is retained.
- **PA-6** The shim MUST retain enough evidence and trust-state history to distinguish "verified under the policy in force at delivery time" from "verifiable now," including after issuer, key, or policy changes.
- **PA-7** Provenance state — verified, failed (the verifier rejected the evidence), expired (the attestation's validity window has passed), absent (including a downgraded path with no verifier), or paused (the delivery is held before a verification result exists) — MUST be visible per delivery and per target to principals entitled to see the delivery. The evidence class of every mapped fact MUST be visible to principals entitled to see that fact, without requiring access to the other system's objects or exposing another tenant's evidence.
- **PA-8** Trust anchors, signer enrollments, and verification policy are Fleet-side state. The shim MUST NOT project them into OCM-io objects or treat an OCM-io object as a trust root for Fleet delivery.
- **GO-1** Where a GitOps applier manages an OCM-io object or a workload object the shim also touches, the applier's desired state is authoritative for every field it contains. The shim MUST NOT write, correct, or revert those fields, MUST NOT write to the applier's source of desired state to change them, and MUST NOT inject metadata into the object to track or suppress the interaction.
- **GO-2** The shim MUST NOT act as a second reconciler for a GitOps-owned object. Divergence between Fleet intent and the GitOps-applied state MUST be reported as drift or conflict, not converged by repeated writes.
- **GO-3** The shim's own configuration, scope, adoption records, and intent MUST be expressible as declarative, idempotent records reconciled by the shim itself: applying the same record twice is safe, and deleting a record removes exactly the unit it describes, without an out-of-band imperative step for normal operation. A change available only through an imperative path is a declared limitation, not an assumed workflow.
- **GO-4** The shim MUST report the source of an inventory fact as the OCM-io object and read time it observed. It MUST NOT attribute a Git repository, path, or revision to an OCM-io object unless a configured profile binds that source to the object, and it MUST NOT inject source metadata into OCM-io objects. A source claim is not evidence of authorization.
- **GO-5** A Git commit, author, or commit signature MUST NOT be treated as delivery provenance unless a configured provenance profile defines how it authenticates the exact platform content. Otherwise it is a source claim and MUST be reported as one.
- **GO-6** When a competing writer repeatedly corrects the shim, the shim MUST detect the correction, identify the competing manager where the API exposes it, report its own intended value alongside the observed value that replaced it, report the applier's declared source where the shim has access to it, and MUST NOT escalate: it MUST NOT force the apply, claim the field, or add a retry of its own. A correction count without a manager identity is still a reportable drift, not a reason to retry. Where the delivery substrate repeats a failed delivery, the shim MUST report the repetition as drift, never as progress or convergence (GAP-22).
- **GO-7** A Git revert or rollback MUST be observed and reconciled as an ordinary desired-state change, not as an error, attack, or permanent conflict. Where the reverted state conflicts with a shim-owned field, GO-6 governs.
- **GO-8** GitOps pipelines MUST continue to apply OCM-io objects when the shim is down, degraded, or uninstalled. The shim MUST NOT introduce a webhook, finalizer, required field, or admission dependency that blocks them.

**Provenance baseline.** For the confirmed inventory increment, five requirements are load-bearing: PA-1, an observed fact is never an attestation; GO-1 and GO-2, the shim never writes or reverts a field an applier owns; GO-4, an inventory fact's source is the OCM-io object and read time; and GO-8, GitOps does not depend on the shim. The delivery-scope detail below applies only when §4 admits delivery.

Evidence has three possible verification loci: target-side, where the delivery agent verifies before apply and a compromised resource manager cannot substitute content; platform-side, where the resource manager or a hub-side agent verifies before applying with platform credentials, which protects against external forgery but not against the manager itself; and none, where the applier has no Fleet verifier and the delivery is downgraded. Attestation is per deployment, not per path: a deployment created without a caller signature carries no evidence, and the same agent applies it without invoking a verifier. The shim reports that delivery as provenance-absent, never as platform-verified, and does not submit an unsigned deployment as if it carried evidence. Generation binding is opt-in: the verifier compares the signed expected generation with the delivery's generation only when the signer pins one, so a signed input that omits it does not by itself reject a replay at another generation. A delivery held because its credentials were rejected or expired is reported paused, not failed, until it resumes or is withdrawn. An attestation rejection is different: Fleet also records it as `auth_failed` and pauses the fulfillment, but a verification result exists, so the shim reports failed provenance alongside a held delivery. A target whose trust bundle cannot be resolved fails before verification runs: Fleet records an auth failure and pauses the fulfillment, but no verification result exists, so the shim reports paused and names the configuration error rather than a rejection. Resuming a signed deployment is a signed mutation: it requires the signer's new signature at the next generation and replaces the stored provenance, so the shim can drive a resume but cannot author the evidence.

Fleet records a delivery's verification outcome only as a state and a human-readable message, and exposes no durable, queryable per-delivery result: the deployment's read surface carries its state and, for a paused fulfillment, the failing delivery's or target's text in `pause_reason`, while a permanent failure's status reason is not mapped at all; a live UI event stream also broadcasts each delivery's state and message to connected clients, unauthenticated and without history or replay, naming the delivery by an ID that embeds the target in the current implementation while leaving the event envelope's target fields unpopulated (GAP-19, GAP-21). Telling a credential pause, an expired attestation, and another verification failure apart therefore rests on pause text or a live delivery event and on what the shim recorded when it submitted the intent; per-delivery outcomes are reported as partial, never inferred from deployment completion. Where evidence exists, it binds exact content and the authorizing principal and survives projection without re-signing; observed facts carry source, generation, and read time and never carry an attestation. Four limits are declared rather than assumed away: the implemented delete path produces no verifiable signed removal evidence (GAP-18); there is no evidence log or attestation-read API, so removal and historical verification are recorded, not attested (GAP-19); there is no durable per-delivery result, so per-target verification state is reported as partial (GAP-21); and output binding cannot bless a filtered subset because it compares the delivery to the full signed manifest set, so a target that filters manifests by type rejects the delivery (GAP-20). PA-4, PA-5, and PA-6 state the requirement; where an enhancement can close the gap, [enhancements.md](enhancements.md) names the option, and the remainder is a Fleet API or delivery-protocol implementation limit.

**GitOps baseline.** GitOps owns every field in its desired state, so another writer's value for one of those fields is drift the applier restores; the shim therefore treats those fields as externally owned and never writes them. It does not become a second reconciler: divergence is drift or conflict, attributed where the API exposes the competing manager, and the shim escalates neither the write nor the retry. Fleet treats a non-auth delivery failure as retryable and repeats the delivery without an attempt cap, so a conflict recurs until the intent or the ownership changes; the deployment does not enter a failed state for it and shows no pause reason, so the repetition is visible only where a shim component receives the delivery result first-hand, for example by subscribing to the live delivery event stream (GAP-22). The shim cannot stop that loop by editing the intent — Fleet has no update surface — and withdrawing the intent is constrained by ownership-aware removal (P-4), so it reports the repetition as the same drift and declares the limit rather than calling it convergence. A revert is an ordinary change. The shim's own records are required to be declarative and idempotent so applying one twice is safe and deleting it removes exactly the unit it describes; where the baseline exposes only an imperative surface, that limit is declared (GO-3, GAP-17). A GitOps controller writes OCM-io objects, not Fleet intent: the shim records the OCM-io source object and read time it observed, does not attribute a Git revision it cannot see, and never injects source metadata into OCM-io objects (GO-4). A commit signature becomes provenance only when a configured profile binds it to the exact platform content; otherwise it is a source claim. GitOps pipelines do not depend on the shim being alive.

---

## 19. Non-goals (G)

- **G-1** Modifying, forking, or patching OCM-io, its CRDs, controllers, klusterlet, or addon-framework.
- **G-2** Replacing OCM-io's controllers, delivery, cleanup, or registration behavior on targets that use them.
- **G-3** Requiring existing OCM-io users, manifests, or GitOps pipelines to change.
- **G-4** Semantically translating arbitrary unknown CRDs. Only declared mappings are in scope.
- **G-5** Providing cross-system ACID transactions or a single global serialization point.
- **G-6** Making OCM-io multi-tenant, or making Fleet's tenancy visible to OCM-io users who are not mapped to it.
- **G-7** Guaranteeing Fleet capabilities that the OCM-io delivery path cannot enforce (see F-7 and T-6).
- **G-8** Solving the design of either platform. Both are inputs.
- **G-9** Mapping every OCM-io concept, or achieving bidirectional parity. Coverage is chosen, not maximal.
- **G-10** Being a general-purpose bridge for arbitrary CRDs or arbitrary API-to-API synchronization.
- **G-11** Making Fleet the authority for OCM-io objects or aspects outside the approved scope.
- **G-12** Requiring addon packaging, or making any requirement conditional on it.
- **G-13** Replacing OCM-io for existing users, or migrating their clusters, OCM-io addons, placements, or workloads off it.
- **G-14** Requiring a big-bang migration, a maintenance window, or adoption of an excluded aspect before any benefit is realized.
- **G-15** Mapping, mirroring, or bridging Fleet's addon, extension-resource, or plugin system with OCM-io's addon-framework, or projecting either side's addon enablement into the other. OCM-io addon enablement remains OCM-io's (scope row 5); a Fleet addon is a Fleet-internal capability bundle with no OCM-io object counterpart and no per-cluster enablement. If the shim's registration capability is provided by a Fleet addon, M-8 requires it to be delivery-only; it is infrastructure, not a mapped aspect (scope row 16). This does not restrict packaging the shim itself as an OCM-io addon (§5, H-1 – H-15), which carries the shim component, not Fleet's addon framework.

---

Recorded decisions are in [decisions.md](decisions.md). The mapping from each requirement family to a baseline path, an enhancement, a declared gap, or a recorded decision is in [coverage.md](coverage.md). `requirements.md` remains authoritative: a solution or a decision record never relaxes a MUST silently.
