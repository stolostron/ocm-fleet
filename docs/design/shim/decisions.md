# OCM-io × OCM Fleet Shim — Decisions and Tensions

Status: **living decision log**. This document records what has been decided and where requirements pull against each other. It is not normative by itself: a decision changes the requirements only when [requirements.md](requirements.md) is updated, and it changes coverage only when [coverage.md](coverage.md) is updated. Candidate mechanisms are in [baseline-hypotheses.md](baseline-hypotheses.md), upstream options in [enhancements.md](enhancements.md), and benefit claims in [analysis.md](analysis.md).

A decision is **Open** until it has an owner and a recorded answer. Closing one requires the answer, the date, and the requirement or coverage text it changes.

**Current state:** 10 recorded decisions (DR), 30 requirements decisions answered (Q-1 – Q-30; Q-2 by DR-1), 9 solution questions answered (SQ). No requirements decision is open. The approved scope remains cluster inventory together with Fleet-origin cluster registration; every other aspect stays candidate or excluded.

---

## 1. Recorded decisions

These are settled premises and direction choices, not solution designs; the baseline and enhancement documents remain hypotheses. They are recorded here so a first-time reader can see the premise without reconstructing it from the text.

| ID | Decision | Date | Recorded in |
|---|---|---|---|
| DR-1 | **Direction of introduction.** OCM-io is the installed system of record. Fleet is introduced to existing OCM-io users as an addition. Replacing OCM-io for existing clusters is out of scope. This closes Q-2. | 2026-10-26 | requirements §1, §3 (AD-1, AD-2, G-13); analysis §2 |
| DR-2 | **The shim runs as part of the Fleet installation**, not as a separate hub-only proxy. It includes the resource manager, its adapters, and the fleetlet wherever Fleet runs a target-side component. | 2026-10-26 | requirements §1, C-7, H-14 |
| DR-3 | **A target-side component is expected** wherever delivery is in scope. A hub-only deployment is supported but constrained, with declared gaps, not the default assumption. | 2026-10-26 | requirements C-7, H-14, Z-17; baseline-hypotheses §2 |
| DR-4 | **The OCM-io hub may be a `ManagedCluster`.** It is then an ordinary target for the shim, with OCM-io's control-plane components and the shim's own components excluded from delivery, adoption, deletion, and cleanup. Being the hub grants no privilege and is not implicit opt-in. | 2026-10-26 | requirements H-5, H-15, X-11, I-10, T-10, P-10; scope register row 15 |
| DR-5 | **Cleanup state has one declared spoke-side read path.** The fleetlet, where installed, reads `AppliedManifestWork` under the RBAC granted through its OCM-io addon delivery. That record names the work, hub hash, agent, and applied resources, and exposes an eviction start time, but carries no per-apply timestamp and no component version; the shim reports the time it read the record and its own version. A hub-only or platform-side-only deployment never scrapes spoke-local state. | 2026-10-26 | requirements W-17, S-6, C-7; baseline-hypotheses BF-3 |
| DR-6 | **The confirmed scope is cluster inventory** — the inventory *of clusters*, not resource inventory collected from clusters — together with Fleet-origin cluster registration (row 4). Every other aspect is candidate or excluded by default. | 2026-10-26 | requirements §4, §4.1, Z-12 |
| DR-7 | **Upstream changes are optional general enhancements.** No enhancement may be a prerequisite for a mapped aspect, benefit, or scope. The baseline path is always supported. | 2026-10-26 | requirements C-15 through C-20; enhancements §2 |
| DR-8 | **Benefits are stated per deployment mode.** A benefit that requires delivery scope is not presented as the reason to adopt the inventory-only increment. | 2026-10-26 | requirements AD-6, C-16; analysis §2.1 |
| DR-9 | **Fleet target lifecycle.** Fleet creates targets through a Fleet addon's connect input or a delivery output; it exposes no target relabel or deregistration API. A static target keeps its registration labels and cannot be removed. Adoption therefore uses one dedicated per-cluster registration fulfillment: a refresh is a successor fulfillment delivering the changed record, and deleting the deployment that owns the current fulfillment un-adopts exactly one cluster. The registration target and fulfillments are shim infrastructure, excluded from mapping and tenant inventory. | 2026-10-26 | requirements M-8, M-9; baseline-hypotheses BF-10; coverage GAP-12 |
| DR-10 | **Provenance is a delivery property; GitOps is an authority question.** Fleet provenance is evidence that a principal authorized exact content for a delivery, checked at a named locus — target-side, platform-side, or none — and never attached to an observed fact. A GitOps applier's desired state is authoritative for every field it contains; the shim never writes those fields, and a Git commit is a source claim unless a configured profile binds it to the exact platform content. Neither stance requires an OCM-io change. | 2026-10-26 | requirements §18 (PA-1 – PA-8, GO-1 – GO-8, X-12); analysis §6; coverage GAP-13 – GAP-22 |

**Q-2.** Answered by DR-1.

---

## 2. Requirements decisions (Q)

Each answer below is reflected in the cited requirements and coverage rows; the requirements text is authoritative.

### Q-1 Authority per aspect and per resource class

**Status:** Decided 2026-10-26. **Reflected in:** requirements §6, X-1 through X-12.

OCM-io owns cluster identity, membership, labels, claims, `ManagedCluster` status, OCM-io addon enablement, and placement decisions. Fleet owns the records it creates: targets, placements, deliveries, and inventory items, and it owns its own addon/extension/plugin framework, which is not mapped to OCM-io (scope row 16, G-15). In the approved scope no field is co-owned. Where both systems can express the same fact, OCM-io is the source for facts it already owns and Fleet holds a projection. Workload intent, if admitted, is Fleet's for Fleet-originated manifests while the work objects remain OCM-io's.

### Q-3 Unit of correspondence

**Status:** Decided 2026-10-26. **Reflected in:** requirements I-11, W-16.

The unit is per aspect. For cluster inventory, one `ManagedCluster` corresponds to one Fleet target, keyed by a persisted identifier rather than the Kubernetes UID. For workload intent, one Fleet intent corresponds to one delivery per target and one work object per target; each manifest is identified by group, version, kind, namespace, and name.

### Q-4 Coexistence posture

**Status:** Decided 2026-10-26. **Reflected in:** requirements P-1, P-11, G-13.

Coexistence is indefinite and per aspect. OCM-io keeps delivering the work it already delivers; Fleet delivers only work Fleet introduces. No cutover of existing workloads is required or supported, and each aspect has one active path per target.

### Q-5 Approved scope

**Status:** Decided 2026-10-26. **Reflected in:** requirements §4.1.

Cluster inventory (row 1) and Fleet-origin cluster registration (row 4) are confirmed. Cluster health (row 2), version/capacity facts (row 3), and OCM-io addon enablement state (row 5) are excluded from the first increment and remain candidates for a later decision. Cluster deletion (row 13) is handled only to the extent that the shim must not orphan; full lifecycle mapping is deferred. Rows 6 through 12, 14, and 16 stay excluded; row 16 is Fleet-internal and is not a cross-system fact. Registration uses OCM-io's own registration contract, unregistration is ownership-aware, and a target that cannot be registered stays visible as unregistered rather than being given a fabricated `ManagedCluster` (GAP-23).

### Q-6 Tenancy mapping

**Status:** Decided 2026-10-26. **Reflected in:** requirements §13, T-1 through T-10.

Fleet tenancy is a Fleet workspace and has no OCM-io counterpart. No OCM-io namespace or RBAC mapping is implied, and tenant identity is never encoded into OCM-io objects. The shim acts as a provider principal: Fleet authorization is evaluated for the requesting tenant, and OCM-io authorization is evaluated independently for the shim's identity. A tenant sees a cluster only when a target for it is granted to that tenant's workspace. This depends on Fleet implementing the workspace partition; where it does not, the shim operates at provider scope, reports tenant-scoped visibility unavailable, and claims no tenant isolation (GAP-24).

### Q-7 Status write-back

**Status:** Decided 2026-10-26. **Reflected in:** requirements S-11, X-7.

The shim writes no OCM-io status field in the approved scope. A future write-back would have to name the field, show that no OCM-io controller owns it, and be admitted as a new aspect.

### Q-8 Capability fidelity matrix

**Status:** Decided 2026-10-26. **Reflected in:** requirements F-7, Z-14; coverage §3 (F-7).

For cluster inventory, identity, membership, and labels are preserved. Claims are approximated because Fleet placement matches exact label values only, so claims must be projected into labels and claim-selector semantics, `matchExpressions`, and claim typing are lost. A claim name that collides with a label key resolves by a declared precedence and is reported, not silently merged. Taints and tolerations are unavailable: Fleet has no toleration model, and a label cannot express permission to select a tainted cluster. Freshness is bounded by the declared budget rather than immediate. Cluster health, capacity, and version are unavailable because they are out of scope. Fleet-origin registration preserves cluster identity and creates membership through OCM-io's own registration contract; a cluster that cannot run klusterlet stays visibly unregistered rather than being approximated (GAP-23). If delivery is later admitted, manifest content, namespace, and update strategy are preserved; rollout groups are approximated only if the shim implements the gating; tasks, preemption, non-Kubernetes, local, and Fleet addon-registered targets, and Fleet provenance are unavailable on the OCM-io path. Fleet's own addon, extension-resource, and plugin capabilities are not mapped to OCM-io and are not a fidelity claim; they remain Fleet-native (scope row 16, G-15).

### Q-9 Scale targets

**Status:** Decided 2026-10-26. **Reflected in:** requirements §15, N-1, N-4, V-6.

| Profile | Targets | Load | Freshness budget |
|---|---|---|---|
| Inventory-only | 2,000 | 10,000 label or claim changes per hour sustained, 1,000 in any minute; one registration fulfillment per target in steady state, two transiently during a refresh; 50 concurrent status updates | Change observed within 30 s p95 and projected to Fleet placement labels within 60 s p95; full convergence within 10 min after an outage |
| Delivery scope | 500 | 10,000 deliveries; 20 concurrent rollouts | Delivery accepted within 60 s p95; rollout progress reported within 60 s |

These are test profiles derived from Fleet's published 50/500/2,000 target assumptions, not a vendor limit. Raising them requires measurement.

### Q-10 Supported OCM-io versions and skew

**Status:** Decided 2026-10-26. **Reflected in:** requirements §1.1, C-9, U-1.

The baseline is OCM-io v1.4.x, with v1.4.0 as the declared reference version and the klusterlet and addon-framework of the same release. Hub and spoke must be within one minor version. Older minors are not claimed until validated. The Fleet version is declared per release.

### Q-11 Target-side presence and authorization

**Status:** Decided 2026-10-26. **Reflected in:** requirements C-7, W-17, S-6.

The fleetlet is a shim component wherever delivery is in scope. Its OCM-io addon delivery may grant cluster-scoped read (`get`, `list`, `watch`) of `appliedmanifestworks.work.open-cluster-management.io` and the namespaced reads it needs. It may not write `AppliedManifestWork`, may not self-grant privileges, and does not receive hub cluster-admin. Credentials reach it through OCM-io-addon-sanctioned mechanisms. A hub-only deployment has no fleetlet; spoke-local cleanup state and local observation are then unobservable and declared as such.

### Q-12 Removal and rollback

**Status:** Decided 2026-10-26. **Reflected in:** requirements U-4, U-5, AD-7.

"Leave objects in place" is the default. Uninstall stops writes, releases ownership, and completes or releases pending cleanup. OCM-io objects are not deleted. Fleet data may be retained or deleted by explicit choice; either way OCM-io returns to its pre-adoption behavior.

### Q-13 OCM-io addon packaging boundary

**Status:** Decided 2026-10-26. **Reflected in:** requirements §5, H-1, H-2, H-7, H-12, H-14.

In the expected shape, fleetlet is addon-packaged on every target where Fleet runs a target-side component. The resource manager runs outside OCM-io's addon mechanism. A fleetlet may be addon-installed on a self-registered hub like on any other target. The fleetlet opens one outbound connection to the resource manager and multiplexes control, access, delivery, status, and index channels over it; no inbound connection to the spoke is required. A deployment with no fleetlet uses Fleet's platform-side delivery agent where it can and declares the resulting gaps.

### Q-14 Meaning of "cluster inventory"

**Status:** Decided 2026-10-26. **Reflected in:** requirements Z-12, §4.1.

It is the inventory *of clusters*, not inventory collected from clusters. Resource inventory inside clusters is a separate excluded aspect: OCM-io-collected in-cluster inventory is OCM-io's, Fleet addon-reported inventory is Fleet's, and neither is mapped by the shim (scope rows 9 and 16, G-15).

### Q-15 Coverage versus OCM-io addon enablement

**Status:** Decided 2026-10-26. **Reflected in:** requirements Z-11, H-10.

Where the shim is addon-delivered, addon enablement selects coverage. The two must not disagree silently; a disagreement is surfaced as a condition.

### Q-16 Self-observation

**Status:** Decided 2026-10-26. **Reflected in:** requirements H-5, H-11, P-10.

The shim excludes its own addon objects, its addon namespace, and the work that delivers them. Exclusions are keyed by object identity or a stable label the shim controls, never by kind alone, so an upgrade that adds objects does not widen the exclusion or create a loop.

### Q-17 Cluster inventory freshness and completeness

**Status:** Decided 2026-10-26. **Reflected in:** requirements S-12, S-3, M-1.

A change is observed within 30 s p95 and reaches Fleet placement labels within 60 s p95 in steady state, and within 10 minutes after an outage at the declared profile. A label or claim change is applied by a successor registration fulfillment delivering the changed record (DR-9); it is not an in-place write. During an outage, last known values are retained and marked stale with the time they were last confirmed; nothing is deleted or replaced by an empty value. Complete inventory means every `ManagedCluster` in the declared scope is present with a freshness marker.

### Q-18 ManifestWork family usage mode

**Status:** Decided 2026-10-26. **Reflected in:** requirements W-29, D-9, P-9.

In the approved scope the shim writes none of the family. It may read its own addon's delivery object, which is excluded from mapping; no other work object carries a mapped fact.

### Q-19 Canonical ManifestWork/MWRS semantics

**Status:** Decided 2026-10-26. **Reflected in:** requirements W-25, C-9, U-1.

The field set and status shape are those shipped in OCM-io v1.4.0: `work.open-cluster-management.io/v1` for ManifestWork and `v1alpha1` for ManifestWorkReplicaSet. They are re-validated at every baseline change.

### Q-20 Fan-out unit if workload intent is admitted

**Status:** Decided 2026-10-26. **Reflected in:** requirements W-2, W-5, W-26.

MWRS is the fan-out unit where its feature gate is enabled and its fidelity is acceptable. Direct per-cluster ManifestWork is the fallback. The two are separate declared write modes and are never mixed within one intent.

### Q-21 Ownership of MWRS children

**Status:** Decided 2026-10-26. **Reflected in:** requirements W-1, W-18, I-11.

OCM-io's controller owns the generated children. Ownership is expressed by a placement-name label, an owner-key-hash label, and an owner annotation; the deprecated MWRS-name label is set only when its `namespace.name` value fits the 63-character label limit. There are no owner references. Fleet owns the intent, not the children.

### Q-22 First adoption increment

**Status:** Decided 2026-10-26. **Reflected in:** requirements AD-3, AD-5, AD-6; coverage §7.

Inventory-only is a complete supported mode and a usable foundation within its declared inventory scope (declared losses include GAP-8, GAP-11, GAP-12, GAP-23, GAP-24): one target pool that Fleet placement selects across, covering OCM-io-adopted clusters and native Fleet targets; labels and projected claims as placement inputs; and registration of Fleet-origin clusters into OCM-io through its own registration contract. Per-workspace tenant visibility is a requirement, not a current baseline property: Fleet's workspace partition is not yet implemented, so the shim must not claim it until Fleet enforces the grant (T-1, T-9; GAP-24). It is not a reason to adopt for users whose only need is rollout or delivery provenance; those require delivery scope. If the target users need those capabilities first, the scope baseline must change rather than the benefit story.

Each adopted cluster costs one dedicated registration fulfillment (M-8, M-9, GAP-12); the cost is Fleet's pricing of a target, and it does not change what the mode claims.

### Q-23 Baseline and enhancement matrix

**Status:** Decided 2026-10-26. **Reflected in:** requirements C-15 through C-20, Z-14, U-1.

The baseline is OCM-io v1.4.x. Enhancements are optional, never in a required path, and each must be validated individually against the baseline. Each enhancement's supported versions and the shim's behavior when it is absent are declared with the enhancement.

### Q-24 Hub as a managed cluster

**Status:** Decided 2026-10-26. **Reflected in:** requirements H-15, X-11, T-10, P-10.

The hub may be registered as a `ManagedCluster` and is then an ordinary target with the standard exclusions: `open-cluster-management*`, `kube-system`, `kube-public`, `kube-node-lease`, the Fleet namespace, and the shim's addon namespace. Delivery to the hub is limited to namespaces a provider names explicitly. Registration is not opt-in for any aspect.

### Q-25 Evidence class of cluster inventory

**Status:** Decided 2026-10-26. **Reflected in:** requirements PA-1, PA-7; coverage GAP-14.

Cluster inventory is a set of observations. Each fact records the OCM-io source object, its generation or resourceVersion where available, and the time the shim read it. It is never presented as attested, and it does not inherit the provenance of the registration delivery that created the Fleet target. The registration fulfillment's evidence, if any, covers the manifest it delivered at that generation — the projected values it carried — not the OCM-io object they were read from or their continued truth.

### Q-26 Verification locus per delivery path

**Status:** Decided 2026-10-26. **Reflected in:** requirements PA-2, PA-3; coverage GAP-13.

Three loci are recognized: target-side (the fleetlet), platform-side (the resource manager or a hub-side delivery agent verifies before applying with platform credentials), and none (the OCM-io work agent applies without a Fleet verifier). Target-side is the only locus that survives resource-manager compromise. Platform-side protects against external forgery but not against the manager itself. A path with no verifier is declared provenance-downgraded; apply status is observation, not evidence. Attestation is per deployment: a deployment created without a caller signature carries no evidence at any locus and is reported provenance-absent, not platform-verified.

### Q-27 Evidence binding, removal, history, and trust

**Status:** Decided 2026-10-26. **Reflected in:** requirements PA-4, PA-5, PA-6, PA-8; coverage GAP-18, GAP-19, GAP-20.

Evidence binds the exact content delivered and the principal. A projection retains the original identity and binding; the shim never re-signs for a producer. Removal is required to carry evidence equivalent to the delivery it reverses, and evidence and trust-state history are required to distinguish verified-then from verifiable-now. The baseline cannot meet either requirement for Fleet deliveries, so the shim reports removal and historical verification as recorded rather than attested and declares both gaps (GAP-18, GAP-19). Output binding covers the deployment's full signed manifest set; where a target filters manifests by accepted type, the delivery fails verification rather than receiving a verified subset. The shim does not re-sign a reduced set on a producer's behalf (PA-4); the remedies are producer-side signing of the exact set each target receives or restricting placement to targets that accept every signed type (GAP-20). Trust anchors and signer enrollments are Fleet-side; none is projected into OCM-io.

### Q-28 GitOps authority, drift, and rollback

**Status:** Decided 2026-10-26. **Reflected in:** requirements GO-1, GO-2, GO-6, GO-7; coverage GAP-16, GAP-22.

Where GitOps applies an object, its desired state is authoritative for every field it contains. The shim does not write, correct, or revert those fields, and does not act as a second reconciler. Divergence is drift or conflict; the shim names the competing manager where the API exposes it and does not force or add a retry. Fleet's own orchestration retries a failed delivery, which is a substrate loop the shim declares rather than an escalation it performs (GAP-22). A Git revert is an ordinary desired-state change; it becomes a conflict only where it would change a field the shim owns.

### Q-29 Declarative records and source claims

**Status:** Decided 2026-10-26. **Reflected in:** requirements GO-3, GO-4; coverage GAP-15, GAP-17.

The shim's own configuration, scope, adoption records, and intent must be expressible as declarative, idempotent records reconciled by the shim itself: applying one twice is safe, and deleting it removes exactly the unit it describes. Where the baseline offers only an imperative path, that is a declared limitation. GitOps is a writer of OCM-io objects, not a writer of Fleet intent: the shim records the OCM-io source object and read time it observed, and it does not attribute a Git repository, path, or revision to an object unless a configured profile binds that source to it. No source metadata is injected into OCM-io objects, and a source claim is not evidence of authorization.

### Q-30 Git identity and pipeline independence

**Status:** Decided 2026-10-26. **Reflected in:** requirements GO-5, GO-8.

A Git commit, author, or commit signature is not delivery provenance. It becomes provenance only when a configured profile defines how it authenticates the exact platform content; otherwise the shim reports it as a source claim, not as provenance. GitOps pipelines must continue to apply OCM-io objects when the shim is down or uninstalled; the shim introduces no webhook, finalizer, required field, or admission dependency that can block them.

---

## 3. Solution questions (SQ)

These do not block the requirements baseline. They shape how the options are described and what the shim may assume; no option is a dependency.

- **SQ-1** The vehicle is core only when the change is general and independently useful; otherwise a separately installed addon or a documented convention. None is needed for cluster inventory. **Status:** Decided 2026-10-26.
- **SQ-2** OCM-io v1.4.x is the baseline. Support ends when the baseline moves to the next minor, with one minor of overlap. **Status:** Decided 2026-10-26.
- **SQ-3** If E-2 is built, its hub inventory is advisory. OCM-io membership remains authoritative, and the shim does not wait for it. **Status:** Decided 2026-10-26.
- **SQ-4** If E-11 lands, Fleet owns rollout for Fleet-originated intent and OCM-io owns rollout for OCM-io-originated work. One owner per intent, never both. **Status:** Decided 2026-10-26.
- **SQ-5** Baseline and enhanced modes are supported side by side for the life of the baseline minor. An enhancement that becomes a default in OCM-io becomes part of the baseline at the next baseline bump. **Status:** Decided 2026-10-26.
- **SQ-7** E-9's standardized facts layer over addon-supplied facts. Standard-named facts win for their names; addon facts are preserved with their source. **Status:** Decided 2026-10-26.
- **SQ-8** The five-part eligibility test is sufficient, with "independently valuable to an OCM-io user who never installs Fleet" kept as a hard gate. **Status:** Decided 2026-10-26.
- **SQ-9** A self-registered hub is an ordinary target with the standard exclusions, not a separate mode. It gets a separate test row because self-observation and control-plane exclusion are involved. **Status:** Decided 2026-10-26.
- **SQ-10** Adoption uses one dedicated per-cluster registration fulfillment because Fleet has no target relabel or deregister API and a statically registered target is frozen and unremovable. Static connect for an adopted cluster is rejected; a shared registration fulfillment is rejected because it makes removal non-per-cluster. No OCM-io enhancement addresses this; the constraint is Fleet's. **Status:** Decided 2026-10-26. **Reflected in:** M-8, M-9, BF-10, GAP-12.

---

## 4. Tensions to resolve explicitly

These are not defects; they are places where requirements pull against each other. Each is resolved by a requirement or a recorded decision, and each remains a check on later changes.

- **C-2 vs I-5**: no metadata injection, yet operators need bidirectional discoverability. Correlation must live outside OCM-io objects.
- **X-1 vs C-6**: single authority, yet multiple writers exist by design. Authority must be per aspect, with contention as a detectable condition.
- **P-1 vs U-7**: one delivery path, yet a GitOps applier may own fields on the same object. The shim never becomes a second writer of those fields; it treats them as externally owned and surfaces contention as drift (GO-1, GO-2, GO-6).
- **S-4 vs O-1**: delta-only writes, yet full sync-state visibility. Visibility must not be achieved by writing status on every event.
- **T-6 vs F-2**: Fleet's provenance guarantees cannot be enforced by klusterlet, yet intent must still reach OCM-io targets. The downgrade must be declared, not hidden.
- **U-5 vs I-3**: adopting existing objects, yet refusing identity inheritance across delete/recreate. Adoption must bind to a concrete object incarnation, not just a name.
- **R-6 vs S-5**: backpressure and shedding, yet OCM-io freshness expectations. Shedding order must be defined so status freshness degrades before intent or deletion.
- **Z-1 vs F-10**: minimal scope, yet inventory-only must feel complete. The product boundary must be presented honestly rather than filled with approximations.
- **Z-6 vs operator expectations**: "not mapped" is a first-class state, but consumers will read absence as health. Coverage must be shown next to data, not buried in configuration.
- **H-3 vs R-1**: per-cluster enable/disable is the deployment unit, but disabling must not lose durable intent or cleanup obligations.
- **H-6 vs H-7**: least privilege for the addon, yet it needs credentials and configuration to reach Fleet. The delivery path for those must be sanctioned, not convenient.
- **H-5 vs H-11**: self-delivery must be ignored, yet the addon manager legitimately updates the shim's own objects. Exclusion must be precise, not a blanket ignore.
- **Z-8 vs X-9**: scope must be additive, but each addition needs a fresh authority decision. Expansion cannot be a pure config change.
- **W-7 vs the name**: MWRS looks like a Fulfillment and is not one. Structural similarity invites false equivalence, and the gap is exactly where rollout, generation, and target-type semantics live.
- **W-8 vs the strategy name**: MWRS calls its modes All and Progressive, but Progressive advances only when per-manifest `Progressing` conditions are supplied and current, and defaults to all clusters in one wave. The name promises more than the default delivers.
- **W-14 vs operator language**: `Applied` and `Available` appear on every work, and neither is health. Both will be read as "fine" by anyone who has not configured condition rules, while completion only appears when a rule produces it.
- **W-1 vs appearance**: generated child ManifestWorks look like ordinary objects and accept writes, but the MWRS controller owns them through labels and an annotation, not owner references, and will reconcile edits away.
- **W-18 vs cost**: aggregate status is cheap to read and insufficient; per-child status is truthful and multiplies read load by the number of selected clusters.
- **W-20 vs M-2**: MWRS selects clusters through OCM-io Placement, but Fleet needs targets and has its own placement. Sharing a selection is not the same as owning it.
- **W-26 vs C-9**: MWRS is not a stable contract across all supported OCM-io versions, yet the requirements are long-lived. Stability must come from declared fallbacks, not from assuming the API will not move.
- **M-6 vs placement**: OCM-io can select on labels and expressions, claims, taints, and tolerations; Fleet selects on exact label values only. Projecting claims into labels loses expressions and claim semantics and must be declared; taint and toleration semantics cannot be projected at all (GAP-8, GAP-11).
- **M-8/M-9 vs Fleet's target API**: adoption needs relabeling and removal, but Fleet has no target update or deregister API, while a statically registered target is frozen and unremovable. An unrelated delivery cannot be allowed to own the target, and a shared registration would make removal non-per-cluster. The per-cluster registration fulfillment is the resolution; its cost is declared in GAP-12.
- **Benefits vs scope**: the capabilities that most motivate adoption are split: fleet-wide query over clusters would need a target query surface, which Fleet does not have; tenancy needs a Fleet workspace partition that is not implemented (GAP-24); and rollouts and delivery provenance need workload delivery, which is excluded. Inventory-only adoption must be worth doing on its own, or the baseline in requirements §4 is wrong (AD-6).
- **AD-2 vs ambition**: every new benefit tempts Fleet to become authoritative for something OCM-io owns. Additive adoption constrains benefit design, not just synchronization.
- **C-16 vs benefit claims**: an enhancement can make a benefit real on newer OCM-io and unavailable on the baseline. Benefits MUST be stated per mode, not as if the best case were universal.
- **Enhancement dependency vs B-1**: a change that improves the shim but requires an upgrade contradicts the no-forced-upgrade promise. It can only be an optional accelerator, and the baseline path must remain supported.
- **GitOps vs ownership conventions**: a manifest cannot declare which fields its applier owns, and a GitOps tool cannot be made to respect such a declaration. Ownership is attributed per applier, so any "co-ownership" convention must govern status writers only; GitOps remains authoritative over everything it applies, and the shim must treat those fields as externally owned.
- **PA-1 vs F-7**: an observation is useful and current, but it is not attested. The evidence class must be shown next to the fact, not inferred from the delivery that carried it.
- **PA-2 vs deployment modes**: the strongest designed verification is target-side and needs a fleetlet; platform-side is weaker; no verifier is a declared downgrade. A mode matrix that says "delivery works" without naming the locus hides the difference.
- **PA-7 vs the delivery API**: provenance state is required per delivery and per target, but Fleet exposes no durable per-delivery result — only deployment state, pause text, and a live event stream without history or tenant scoping, whose target fields are unpopulated and whose delivery ID embeds the target. Deployment-level completion must not be stretched into per-target proof, so the per-delivery record is reported as partial (GAP-21).
- **GO-3 vs C-4**: the shim needs a declarative control surface, yet it must not alter what a GitOps applier writes to OCM-io. Its records are shim-owned and separate from the applier's manifests, so no existing pipeline has to adopt them or change.
- **GO-5 vs GitOps expectations**: a signed commit authenticates the commit, not the platform content the user authorized. Unless a profile binds the two, the shim must call it a source claim.
- **GO-1 vs PA-2**: an applier can restore the declared state, which makes the repository operationally authoritative, but the restore carries no Fleet delivery evidence. Applier authority and provenance must be reported separately; neither implies the other.
