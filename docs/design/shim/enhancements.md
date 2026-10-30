# OCM-io × OCM Fleet Shim — Upstream Enhancements

Status: **options for OCM-io's own process**. This document is not normative for the shim. It records changes OCM-io could make that would improve the integration *and* stand on their own for OCM-io users. Every entry is optional: the baseline in [baseline-hypotheses.md](baseline-hypotheses.md) must work without it (C-16), and the shim must never require one (C-18).

Nothing below is a design proposal or a commitment. Each entry is an option sketch, with a cost and an assessment, recorded so the requirements can be read against what might become possible.

Written for OCM-io maintainers first and Fleet engineers second. An enhancement that cannot be justified without mentioning Fleet does not belong here (C-15, C-18). Requirement IDs (`C-1`, `Z-14`, `W-17`, …) refer to [requirements.md](requirements.md); `E-*` are enhancements, `RS-*` rejected solutions, `BF-*` baseline fallback hypotheses in [baseline-hypotheses.md](baseline-hypotheses.md), `SQ-*` solution questions in [decisions.md](decisions.md), `B-*` benefits in [analysis.md](analysis.md) §2, and `GAP-*` declared gaps in [coverage.md](coverage.md) §4.

---

## 1. Rules

- **General only.** An upstream change must be independently valuable to OCM-io users who never install Fleet. Fleet is one consumer, never the justification.
- **Optional only.** No enhancement may become a prerequisite for a scope, aspect, benefit, or mode (C-16, C-18).
- **Additive and opt-in.** No behavior change for existing users, no new required fields, no forced upgrade.
- **Detectable.** Every mode difference must be observable and declared (C-17, Z-16).
- **No authority change.** No enhancement may introduce a second system of record or change who owns an existing OCM-io object (C-19).
- **Requirements win.** If an enhancement needs a MUST relaxed, the requirement document changes deliberately; the enhancement does not quietly assume it away.

---

## 2. Eligibility test for an upstream change

An OCM-io change qualifies as a general enhancement only if all five hold:

1. It is independently valuable to OCM-io users who never install Fleet — GitOps, policy, observability, cleanup, audit, disaster recovery, capacity.
2. It is additive and opt-in: no behavior change for existing users, no new required fields, no forced upgrade.
3. It uses OCM-io's vocabulary (conditions, update strategies, feedback rules, addon framework), not Fleet's (Fulfillment, workspace, provenance profile, delivery generation).
4. It stands on its own in OCM-io's enhancements process without mentioning Fleet.
5. It does not change existing OCM-io authority or introduce a second system of record.

A change that fails any of these is either rejected or belongs in the shim as a workaround.

---

## 3. Solution families

| Family | What it is | Lead time | Blast radius | Upgrade required | Independence |
|---|---|---|---|---|---|
| **Core API change** | New fields, conditions, or semantics in OCM-io CRDs and controllers | Slow | High | Yes for the new fields | Strong if general |
| **Convention** | Documented condition types, field-manager names, feedback defaults, ownership rules; no schema change | Fast | Low | No | Strong |
| **Optional addon** | A general OCM addon that supplies observability or features per cluster | Medium | Medium | No | Strong |
| **Shim-side workaround** | Correlation, polling, inference, declared degradation, external records | Immediate | Low | No | None needed |

Tier 1 favors conventions and optional addons, because they can be adopted without waiting for an OCM-io release and without forcing an upgrade (B-1, C-16). "Optional addon" here means an **OCM-io addon any user could run**, not the shim's own OCM addon packaging; fleetlet is part of the shim, not an enhancement.

---

## 4. Enhancement register

Tiers reflect cost and risk. Whether the shim needs an enhancement is stated in its entry. "Relaxes" means the requirement or benefit becomes easier to satisfy; it never means the requirement is removed. "Competes" means the enhancement absorbs a capability the adoption story currently attributes to Fleet.

| # | Enhancement | Tier | Likely vehicle in OCM-io | Requirements or benefits relaxed | Effort | Competes with Fleet |
|---|---|---|---|---|---|---|
| E-1 | Standard freshness and condition vocabulary | 1 | Convention, then core | S-3, S-5, W-9, W-15, O-1 | Low | No |
| E-2 | Hub-visible applied-resource inventory and deletion progress | 3 | OCM-io addon, then core | none in normal modes; narrows GAP-2 in hub-only constrained mode | High | No |
| E-3 | Default / well-known health feedback | 1 | Convention + core | W-15, S-6, S-10 | Medium | No |
| E-4 | Per-target status on `ManifestWorkReplicaSet` | 1 | Core | W-18, W-19, O-1 | Low–medium | No |
| E-5 | Stable cluster identity | 1 | Convention, then core | I-2, I-3, I-7, M-7 | Low | No |
| E-6 | Pause / hold on work objects | 1 | Core | P-5, U-9 | Low | No |
| E-7 | SSA status ownership and list-map conditions | 2 | Core (`sdk-go` + controllers) | none for the shim; X-3, S-2 for OCM-io status writers | Medium | No |
| E-8 | Signed intent and source provenance | 2 | Core | T-6, PA-3, PA-5, PA-6 (conditionally), GO-5, part of B-10 | Medium–high | Some (B-10) |
| E-9 | Standardized cluster facts | 2 | Convention + addon | S-8, part of B-7 | Medium | No |
| E-10 | Explicit membership and exclusion in Placement | 2 | Core | M-2, M-4 | Low–medium | No |
| E-11 | Rollout tasks, preemption, and arbitrary gates | 3 | Core | W-7, part of B-3 | High | Yes (B-3) |
| E-12 | Stable `MWRS` API and conversion guarantees | 2 | Core / process | W-25, W-26, U-1 | Medium | No |
| E-13 | Inventory / observation extension point | 3 | Core or addon | S-6, S-8 | High | Some (B-7) |
| E-14 | OCM-io addon lifecycle status unification | 2 | Convention + core | H-4, M-2 | Medium | No |
| E-15 | Large work bundles via references or digests | 3 | Core | D-6, W-13 | Medium–high | No |

---

## 5. Tier 1

These are the changes worth pursuing first. They are cheap, additive, independently valuable, and they remove the shim's worst blind spots: freshness, health, per-cluster visibility, identity, and a way to hold delivery.

### E-1 — Standard freshness and condition vocabulary

**Capability.** Every OCM-io status reports an `observedGeneration`. Conditions carry a standard type, status, reason, message, and timestamp, and per-manifest status records the last applied generation, last apply time, and last apply error. `Applied` (no apply error) and `Available` (object exists) keep their meanings everywhere, while `Progressing` and `Degraded` gain one documented meaning across `ManifestWork`, `MWRS`, addon status, and `ManagedCluster` instead of existing only where a user wrote a condition rule.

**Where it exists today.** The current coverage — which APIs carry `observedGeneration` and which do not — is recorded in [analysis.md §4](analysis.md#4-status-freshness-in-ocm-io-today). In short: the condition type already has the slot; the gap is in the writers, not the schema.

**Why it is general.** Every controller, dashboard, GitOps tool, policy engine, and operator benefits from being able to tell stale from fresh and from reading health consistently. This is a correctness fix, not a Fleet feature.

**Requirements relaxed.** S-3 (stale vs fresh), S-5 (freshness expectations), W-9 (lineage), W-15 (health absent unless a rule exists), O-1 (sync state). W-14 is unchanged: `Applied` and `Available` still must not be presented as health.

**Possible shape.** `status.observedGeneration` on every status subresource; per-condition `observedGeneration`; a documented condition vocabulary with reasons; per-manifest `lastAppliedGeneration`, `lastApplyTime`, `lastApplyError`. Additive; no new required fields.

**Baseline fallback.** BF-2: the shim records source generation and timestamps on its own side, and treats a missing `observedGeneration` as "unknown", never as "current".

**Cost and risk.** Low. Mostly controller plumbing and documentation. The main risk is vocabulary churn; freeze the semantics early and extend additively.

**Assessment.** Convention first (documented types and reasons), API fields second. This is the single highest-leverage change.

### E-3 — Default / well-known health feedback

**Capability.** Health, readiness, and completion are computed from well-known status for built-in kinds without every user authoring JSONPath or CEL rules. `Available` is not redefined: it still reports existence. An explicit opt-out remains.

**Why it is general.** Health should not depend on per-manifest configuration. `Available` is present for every applied object, but it says only that the object exists; readiness, rollout progress, and completion are absent unless a rule is written. Most users never write one.

**Requirements relaxed.** W-15 (health absent unless a rule exists), S-6 (observation fidelity), S-10 (meaning carried with value), and improves B-8.

**Possible shape.** Well-known condition rules for built-in kinds, enabled by default, with documented per-kind semantics, explicit opt-out, and no attempt to interpret arbitrary CRDs. Feedback results carry `observedGeneration`.

**Baseline fallback.** BF-4: report existence as existence and health as "not reported" unless a rule is configured; configure rules only where the shim is permitted to.

**Cost and risk.** Medium. The per-kind mappings are the work; over-claiming health is the risk. Keep the default conservative: report only what is mechanically derivable.

**Assessment.** Convention plus a conservative core default.

### E-4 — Per-target status on `ManifestWorkReplicaSet`

**Capability.** The MWRS status exposes per-selected-cluster state — child reference, applied/available, generation — instead of only aggregate counts.

**Why it is general.** A fleet-wide object that cannot be debugged per cluster forces every consumer to walk children. That is a general usability gap.

**Requirements relaxed.** W-18, W-19, O-1.

**Possible shape.** A bounded per-cluster list with a child reference and conditions, with pagination or a summary/detail split for large selections. Aggregate counts remain, but are explicitly derived from the per-cluster data.

**Baseline fallback.** BF-5: read child `ManifestWork`s, handle children that do not exist yet or are terminating, and never expand counts into per-target conditions.

**Cost and risk.** Low–medium. Size at scale is the only real issue.

**Assessment.** Small and general; high value if OCM-io adopts it.

### E-5 — Stable cluster identity

**Capability.** A cluster-supplied stable identity is exposed on `ManagedCluster` status, distinct from the object name and from the Kubernetes UID, and documented as surviving re-registration.

**Why it is general.** Inventory, policy, observability, and cost reporting all need an identity that does not change when a cluster is re-registered or renamed. OCM-io already has cluster claims; this makes one authoritative and visible.

**Requirements relaxed.** I-2, I-3, I-7, M-7.

**Possible shape.** A documented standard claim or status field, uniqueness expectations, and behavior when it is missing or duplicated. Do not make it required for registration.

**Baseline fallback.** BF-6: the shim uses a composite key — cluster name, creation timestamp, and the best available claim — and refuses to inherit state when the composite changes.

**Cost and risk.** Low. Spoofing and duplicate identities must be handled explicitly.

**Assessment.** Convention first; status exposure second.

### E-6 — Pause / hold on work objects

**Capability.** Delivery can be held and resumed without changing desired state, with the hold visible in status and a reason.

**Why it is general.** Maintenance windows, incident response, and a clean boundary for a path change are everyday operational needs.

**Requirements relaxed.** P-5 (safe path change), U-9 (defined operational actions).

**Possible shape.** A `pause`/`hold` field with a reason, a status condition, and precise semantics: does it stop new applies, stop drift repair, or stop both? Answer that in the API, not in prose.

**Baseline fallback.** BF-8: the shim sequences a path change explicitly and accepts a bounded dual-path window, detecting conflicts rather than preventing them.

**Cost and risk.** Low, provided the semantics are exact. Ambiguity here creates a worse failure than no feature.

**Assessment.** Core, small, high operational value.

---

## 6. Tier 2 and Tier 3

### E-2 — Hub-visible applied-resource inventory and deletion progress

**Capability.** The hub can answer: what did this work apply, at which generation, which resources were removed from the current spec, what is still deleting, and what is blocked on a finalizer.

**What already covers it.** `AppliedManifestWork` is a spoke-local record naming the work, hub hash, agent, and applied resources (group, resource, namespace, name, uid, version), with an `evictionStartTime` once eviction begins. Fleet's fleetlet **is part of the shim** (H-14) and reads it under W-17, including on a self-registered hub. The shim therefore has this capability wherever a fleetlet is present; the platform-side-only delivery variant and the hub-only constrained mode remain without it (GAP-2), so a hub-side copy would narrow that gap. The record carries no apply timestamp and no component version, so the fleetlet reports its read time and its own version. The "missing" half — which generation was applied, and when — comes from work conditions and from E-1, not from the ledger.

**Who actually needs E-2.** OCM-io users and tools with no Fleet at all: an auditor, a console, DR tooling, or a user who wants cleanup state on the hub without running an agent. The **hub-only constrained mode** would also consume it, but E-2 does not remove spoke-side work: the reporter would be the klusterlet work-agent, which already runs there, sending a new hub-bound projection. Hub-only mode remains constrained and declared (GAP-2).

**Why it is general.** Cleanup, audit, disaster recovery, and orphan detection are general OCM-io operational problems, and the hub genuinely cannot see any of it. Fleet is a customer of that general gap, not the reason for it.

**Requirements relaxed.** None in normal modes: the shim's own requirements (I-6, R-9, S-7) are met by the fleetlet read. In the hub-only constrained mode it would narrow GAP-2, but the shim still must not depend on it. E-2's main effect is what *other* hub-side consumers can see.

**Possible shape.** A bounded hub-side projection of applied resources: identity, generation applied, state (`Applied`, `Deleting`, `Orphaned`, `Unknown`), last transition, and a reference to any blocking object. Bound the list, paginate it, or expose a summary plus an on-demand detail call. Do not copy arbitrary resource payloads.

**Scale.** The data is proportional to clusters × applied resources, and deletion progress is continuous while a cluster is being torn down — often when it is least reachable. Per-object size limits rule out copying the ledger into a status field, so any workable design is a summary plus a paginated or on-demand detail call: a new API and a new reporting path, not a bounded status addition. Every design must state its bound, its retention, and what it reports when the spoke stops sending.

**Baseline fallback.** BF-3: this *is* the baseline: fleetlet reads `AppliedManifestWork` under W-17 and reports the time it read the record and its own version, going unavailable when it stops. In the hub-only constrained mode, cleanup state is declared unobservable and OCM-io finalizers are trusted to finish; neither path treats the read as authority to delete.

**Cost and risk.** High. A new reporting path from every spoke, a new bounded API with pagination and retention, status size at scale, privacy of resource identity, and the temptation to turn it into a full inventory store. The hub-side copy is also a cache of the spoke ledger, so it must carry its own source and timestamp or it cannot be distinguished from stale data (GAP-1). An addon can prototype it; core would be a larger commitment.

**Assessment.** Tier 3 by cost and risk, and not a shim dependency: the shim must not wait for it, depend on it, or claim it as requirement relief. It is a general OCM-io option for hub-side cleanup visibility; whether OCM-io provides it is OCM-io's decision, and the hub-only constrained mode would consume it only in that case.

### E-7 — SSA status ownership and list-map conditions

**Capability.** Status writers use server-side apply with stable, published field-manager names and apply only the condition types they own. Concurrent writers then cannot drop each other's conditions, and a conflict surfaces as an error or a condition instead of a silent overwrite.

**What it does not do.** It does not make ownership declarable. A manifest does not say "this field is mine", and no convention constrains a GitOps tool: the applier owns every field in its desired state. E-7 governs *writers of status*, not authors of objects. It assumes the status subresource is the boundary, which it is for `ManagedCluster`, `ManifestWork`, `MWRS`, and `ManagedClusterAddOn`.

**Why it is general.** OCM-io's own status writes are JSON merge patches computed between the old and new status (`sdk-go`'s `PatchStatus`). A JSON merge patch replaces the whole `conditions` array, so two controllers writing different condition types race and the last writer wins. The addon controllers already share this path. Fixing it helps every OCM-io status producer, not only third parties.

**Requirements affected.** For OCM-io's status contract: X-3 (contention becomes detectable rather than silent) and S-2 (merge semantics). It does not relax X-7 for the shim: under BF-7 the shim still does not write OCM-io status, so no shim requirement depends on E-7.

**Possible shape.**
- Change the status patcher, or its callers, to SSA with an explicit field manager per writer, or to a per-condition patch. Keep merge-patch behavior only where a writer legitimately owns the whole status.
- Each writer applies only its own condition types. Removing a condition means applying its absence, not rewriting the list.
- Ensure every `conditions` list is `+listType=map` with `+listMapKey=type`. Most OCM-io APIs already are; `ManagedClusterSet` and `ManagedClusterSetBinding` are not.
- Publish the field-manager names and the migration behavior for `managedFields` written by the old patcher.

**Baseline fallback.** BF-7: the shim reads status and merges on the Fleet side; it never writes OCM-io status, so it neither needs nor provides E-7.

**Cost and risk.** Medium. It touches `sdk-go` and every controller that writes status, and it changes how `managedFields` ownership is attributed during upgrade. A convention alone cannot deliver it; the writer mechanism is the substance.

**Non-goal.** Co-ownership with GitOps. GitOps owns whatever its desired state contains and cannot be bounded by an in-object convention. The shim must treat every GitOps-managed field as externally owned regardless of E-7.

**Assessment.** Tier 2 by cost and risk, and not a shim dependency. It is relevant only if OCM-io addresses status-writer concurrency for its own reasons; nothing in the shim depends on it.

### E-8 — Signed intent and source provenance

**Capability.** Work objects carry a verifiable source and signature, with a policy that decides what is trusted.

**Why it is general.** Supply-chain provenance, GitOps source verification, and compliance are general concerns.

**Requirements relaxed.** T-6, PA-3, GO-5; PA-5 and PA-6 only if the profile also authenticates removal and retains the verification record. It also serves part of B-10. It does **not** remove the need for Fleet-side attestations; it reduces the declared downgrade (GAP-13, GAP-15, GAP-18, GAP-19).

**Possible shape.** A source reference, a signature, a verification result condition, an optional retained record naming the policy in force at apply time, and a policy hook. The profile must bind the signer to the exact content and purpose; a commit signature alone authenticates the commit, not the platform content. No Fleet-specific provenance profile in OCM-io.

**Baseline fallback.** BF-14: the shim records the downgrade and the verification locus in Fleet and declares exactly what the OCM-io path cannot enforce. It does not synthesize evidence for that path.

**Cost and risk.** Medium–high: key management, revocation, and policy surface.

### E-9 — Standardized cluster facts

**Capability.** Version, capacity, platform, region, and other stable cluster facts live in a documented place with freshness, instead of being specific to one OCM-io addon.

**Why it is general.** Dashboards, placement, and capacity planning all need them.

**Requirements relaxed.** S-8. It supplies facts that B-7 would query; Fleet's query surface covers extension resources, not targets. Scope is unchanged (Z-15): version, capacity, platform, region, and similar facts remain separate aspects that need their own decision (Z-12). The enhancement makes them available from a documented, freshness-bearing source, so a future decision to map one starts from a real source instead of an OCM-io-addon-specific one.

**Possible shape.** Standard claims or status fields, with `observedGeneration` and a documented staleness model.

**Baseline fallback.** Read OCM-io addon-provided info when present; declare the gaps per cluster.

**Cost and risk.** Medium: data ownership and freshness.

### E-10 — Explicit membership and exclusion in Placement

**Capability.** Include/exclude specific clusters, stable decisions, and documented behavior for transient unreachability, if not already fully expressible.

**Why it is general.** Pinned clusters, carve-outs, and maintenance are ordinary placement needs.

**Requirements relaxed.** M-2, M-4.

**Possible shape.** Extend predicates or add explicit membership semantics; document that transient failure never deselects. Check what current `Placement` already supports before proposing anything.

**Baseline fallback.** Labels and selectors; the shim reports divergence between intended and selected sets.

**Cost and risk.** Low–medium.

### E-11 — Rollout tasks, preemption, and arbitrary gates

**Capability.** Beyond the `All`, `Progressive`, and `ProgressivePerGroup` strategies MWRS already has: ordered tasks, preemption of an in-flight wave, arbitrary gates expressed as conditions, and rollout state that survives a controller restart. The existing strategies are not the gap.

**Why it is general.** Safer fleet-wide changes are a long-standing OCM-io need, and today a Progressive rollout that never receives `Progressing` conditions never advances.

**Requirements relaxed.** W-7 (Fulfillment semantics on the OCM-io path), part of B-3. W-8 is unchanged: the shim must still implement and test any staging it promises.

**Possible shape.** A rollout strategy with ordered groups, gates expressed as conditions, and explicit preemption semantics. This is a substantial design, not a field addition.

**Baseline fallback.** The shim orchestrates multiple work objects externally, or declares rollout semantics unavailable.

**Cost and risk.** High. It also **competes directly with Fleet's rollout strategy** — see §9. If OCM-io gains this, decide which engine owns rollout for a given target before building anything (SQ-4).

### E-12 — Stable `MWRS` API and conversion guarantees

**Capability.** A v1 contract with conversion tests and a documented status shape. In the supported v1.4.x baseline the API is `v1alpha1` behind the `ManifestWorkReplicaSet` feature gate, which is off by default.

**Why it is general.** Anyone building on MWRS needs it.

**Requirements relaxed.** W-25, W-26, U-1.

**Possible shape.** API graduation, conversion tests, deprecation policy.

**Baseline fallback.** Pin versions, detect capabilities, fall back to per-cluster `ManifestWork` creation where allowed.

**Cost and risk.** Process-heavy, technically low.

### E-13 — Inventory / observation extension point

**Capability.** A general hub-side way for an OCM-io addon to report inventory and observations, with a common shape.

**Why it is general.** Every observability addon reinvents this.

**Requirements relaxed.** S-6, S-8. It serves the inventory side of B-7; Fleet's query surface covers extension resources, not targets. It overlaps Fleet's inventory model, so it must be a transport shape, not a competing store.

**Possible shape.** An OCM-io addon capability and a documented envelope; OCM-io stores and serves, it does not become the query engine.

**Cost and risk.** High; scope creep into a second inventory system is the main danger.

### E-14 — OCM-io addon lifecycle status unification

**Capability.** A consistent status vocabulary and per-cluster view across `ClusterManagementAddOn` and `ManagedClusterAddOn`.

**Why it is general.** OCM-io addon operators and users need one health story.

**Requirements relaxed.** H-4, M-2.

**Cost and risk.** Medium.

### E-15 — Large work bundles via references or digests

**Capability.** Work objects can reference large content by digest instead of embedding it, within API limits.

**Why it is general.** Large bundles hit etcd limits today.

**Requirements relaxed.** D-6, W-13.

**Cost and risk.** Medium–high; content addressing, availability, and trust.

---

## 7. Rejected solutions

These fail the eligibility test, weaken requirements, or destroy the adoption story.

| # | Rejected | Why |
|---|---|---|
| RS-1 | Add multi-tenancy to core OCM-io | Architectural, not additive, not opt-in, and it absorbs B-9. Tenancy should remain a Fleet capability over OCM-io facts. |
| RS-2 | Reshape OCM-io around Fulfillment's three axes | Fleet-specific vocabulary and authority model. E-11 covers the user-facing need generally. |
| RS-3 | Require correlation annotations or labels | Fails C-2 and GO-1: the applier treats injected metadata as drift. A general external-reference concept may qualify, but only if designed for portals and GitOps generally. |
| RS-4 | Dynamic proto APIs / no-CRD extensibility in core OCM-io | A Fleet architectural bet, not a general enhancement. |
| RS-5 | Move Fleet's control plane into OCM-io, or make MWRS Fleet's delivery engine | Inverts the direction of introduction and makes Fleet a feature of the incumbent. |
| RS-6 | Change klusterlet delivery semantics to match Fleet's delivery contract | Not general, not additive, and directly violates C-1/C-8. |
| RS-7 | Declare field ownership in object metadata or manifests | Ownership belongs to the applier, not to the object. GitOps owns every field in its desired state, and any other value of those fields is drift it will restore; a declaration cannot bound it, and existing users will not change their manifests to negotiate with a controller. The real defect is merge-patch status writes, addressed by E-7. |
| RS-8 | Have the shim write Git or call a GitOps controller API to deliver intent | Makes the shim a second writer of the applier's desired state, bypasses the review and convergence path, and turns a provenance question into a source-control credential problem. Git may carry signed intent only when a provenance profile binds it (GO-5), not because the shim commits. |

---

## 8. Delivery vehicle: core, addon, or convention

| Criterion | Core | Addon | Convention |
|---|---|---|---|
| Time to availability | Slowest | Medium | Fastest |
| Requires upgrade | Yes | No | No |
| Independent value | Highest if general | High | High if adopted |
| Operational cost | Lowest long-term | Per-cluster | Lowest |
| Risk of Fleet coupling | High if not disciplined | Medium | Low |

By cost and blast radius, E-1 and E-5 fit OCM-io's convention route, and E-3 pairs a convention with a conservative core default; E-9 pairs a convention with an optional addon; E-14 pairs a convention with a core change; E-4, E-6, E-7, E-8, E-10, and E-12 would require a core change (E-12 is process-heavy). E-7 is a writer-mechanism change in `sdk-go`, not a documentation convention. A convention or addon can be revisited as a core change later if OCM-io finds it worth doing; none of these classifications is a commitment by OCM-io.

---

## 9. Strategic consequences

- **Cannibalization.** E-11 would absorb part of B-3 only if it reaches Fleet's rollout semantics; the existing MWRS strategies do not. E-8 absorbs part of B-10, E-13 overlaps B-7, and core multi-tenancy would absorb B-9. The adoption story then rests on the separate control plane, heterogeneous targets, managed resources, federation, dynamic extension APIs, and tenancy over OCM-io facts. That may be sufficient, but the benefit table must be revised deliberately rather than eroding silently.
- **Two modes forever.** Every optional enhancement doubles the test surface and the support matrix. The shim must never assume the enhanced path, or B-1 is false.
- **OCM-io's release cycle.** A core change follows OCM-io's own design, review, implementation, and release process, which the shim cannot assume or schedule. Until OCM-io adopts it, only the baseline path and the shim-side workarounds are available.
- **No Fleet-shaped changes.** A change that only makes sense because Fleet exists is not a general enhancement, even if it helps the shim. General improvements are acceptable even if they help competitors.

---

## 10. Sequencing

The order below reflects dependencies between options, not a schedule this directory controls or an approved plan; any item would follow OCM-io's own process.

1. **Phase 0 — no-enhancement baseline.** The candidate baseline paths and scope registry. No enhancement is assumed. See [baseline-hypotheses.md §5](baseline-hypotheses.md#5-a-possible-first-increment).
2. **Phase 1 — conventions and small fixes.** E-1 and E-5 fit OCM-io's convention route; E-4 and E-6 would be core changes in OCM-io.
3. **Phase 2 — fidelity and status hygiene.** E-3, E-8, E-9, E-10, E-12, E-15, gated on independent value. E-7 follows OCM-io's own status-writer work and is not a shim dependency.
4. **Phase 3 — deliberate.** E-11, E-13, and E-14, and only where the recorded authority split for rollout, inventory, and the hub target (Q-1, Q-4, Q-24) supports them.

E-2 is not sequenced here and is not on the shim's path: it is relevant only if OCM-io chooses to provide hub-side cleanup visibility for non-Fleet consumers.

Each phase must show a measurable benefit per AD-5 and must not add a prerequisite (C-16).

---

## 11. Traceability

| Solution | Requirements or benefits relaxed | Requirements untouched |
|---|---|---|
| E-1 | S-3, S-5, W-9, W-15, O-1 | X, I, P, T, R, N, U, V; W-14 unchanged |
| E-2 | none in normal modes; narrows GAP-2 in hub-only mode | I-6, R-9, S-7 (met by the fleetlet read); X-7, P-1, P-3, T, N |
| E-3 | W-15, S-6, S-10 | X-7, S-9, W-14 |
| E-4 | W-18, W-19, O-1 | X, P, I |
| E-5 | I-2, I-3, I-7, M-7 | I-1, I-4, I-5, I-6 |
| E-6 | P-5, U-9 | P-1, P-3, P-4, X |
| E-7 | none for the shim; X-3, S-2 for OCM-io status writers | X-7 (the shim stays read-only), X-1, X-5, X-10 |
| E-8 | T-6, PA-3, GO-5, part of B-10; PA-5 and PA-6 conditionally | T-1 through T-5, T-7, T-8; PA-1, PA-2, PA-4, PA-7, PA-8; GO-1 – GO-4, GO-6 – GO-8 |
| E-9 | S-8, part of B-7 | Z-1 through Z-17 |
| E-10 | M-2, M-4 | M-1, M-3, M-5, M-6 |
| E-11 | W-7, part of B-3 | W-1 through W-6, W-8, X-8 |
| E-12 | W-25, W-26, U-1 | C-9, U-2 |
| E-13 | S-6, S-8, part of B-7 | X, T, Z |
| E-14 | H-4, M-2 | H-1, H-3, H-6 |
| E-15 | D-6, W-13 | D-1, D-4, D-5 |

Nothing in this table removes the need for scope decisions (requirements §4), authority decisions (X), tenancy mapping (T), identity correlation (I-1, I-4, I-5, I-6), coexistence (P, U), or verification (V).

Solution questions are answered in [decisions.md §3](decisions.md#3-solution-questions-sq).
