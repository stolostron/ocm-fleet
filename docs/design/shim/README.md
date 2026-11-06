# OCM-io × OCM Fleet Shim

This is the design home for introducing **OCM Fleet** to an existing **OCM-io** installation. It contains the requirements the introduction must satisfy, candidate baseline mechanisms that would satisfy them on unmodified OCM-io, optional upstream changes that could improve the result, and the decision and coverage registers that keep the three honest.

**The short version:** OCM-io remains the system of record for its clusters; Fleet gains targets it can place on, built from those clusters' identity, labels, and claims; and a cluster introduced through Fleet registers into OCM-io as a real managed cluster. The projection is deliberately partial: its losses are declared, not hidden.

**Status: design only.** There is no installable shim in this directory. "Confirmed" means the fact and required behavior are in approved scope; every mechanism is a candidate, and no implementation is included.

It is written for several readers — OCM-io maintainers, Fleet engineers, and operators — so it is organized by what each needs, not by the order in which it was discovered. Start with a reading path below.

**If you read one document for the requirements, read [requirements.md](requirements.md).** It is the only normative document; every MUST is there, and no other document can relax one. §4.2 states the confirmed increment, what it does not do, and the five declared losses that shape it, so the normative case is readable on its own. It is not a build plan. [use-cases.md](use-cases.md) is the fastest way to see the increment in practice, [README §The first increment](#the-first-increment) states its shape and its build dependency, and [coverage.md](coverage.md) is the full gap ledger. The rest is rationale, recorded decisions, candidate mechanisms, or optional upstream work.

---

## What the shim is

The **shim** is the entire boundary surface between the two systems: any component, configuration, controller, adapter, CRD, addon (including a shim-owned Fleet addon), agent, or procedure that causes state to cross between them, in either direction.

The candidate shape for the confirmed increment is Fleet-side: a controller that reconciles the adopted inventory and a delivery-only Fleet registration provider. The shim needs no component inside OCM-io's control plane and no target-side component of its own; a Fleet-origin cluster runs OCM-io's klusterlet as part of OCM-io membership, not as a shim component. Where Fleet delivers through a target-side component, the **fleetlet** is also a shim component; a hub-only deployment is then constrained for spoke-local observation, but it is complete for cluster inventory. The shim is not a separate proxy sitting between two unchanged systems.

The **direction** is fixed: OCM-io is the installed system of record for the facts and objects it owns, and Fleet is introduced alongside it to add capabilities OCM-io does not have. Fleet does not replace OCM-io's registration, placement, or delivery for existing clusters. A cluster that Fleet introduces is registered into OCM-io through OCM-io's own registration contract, so OCM-io remains the registration authority.

Two claims stay separate. **Provenance** is cryptographic evidence that a principal authorized an exact delivery; its assurance exists only where a verifier checks it, so the shim names the verification locus for every path instead of treating an apply status as authorization. **GitOps** is an existing writer whose reconciliation makes the declared state the operative one: where an applier owns a field, the shim never writes or reverts it, and a Git commit is a source claim rather than provenance unless a configured profile binds it to the exact content.

The **scope** is deliberately partial. Cluster inventory — the inventory *of clusters* — is confirmed in the OCM-io → Fleet direction, and Fleet-origin cluster registration in the Fleet → OCM-io direction. Everything else is candidate or excluded by default, including Fleet's own addon, extension, and plugin framework (requirements §4.1 row 16, G-15), which is a different thing from OCM-io addon enablement. Mapping more is not progress by itself; making unmapped aspects visibly unmapped is part of the design.

The **hub is a cluster too**. OCM-io can register its own hub as a `ManagedCluster` and install OCM-io addons on it. When that is done, the hub is an ordinary target for the shim, with OCM-io's control plane and the shim's own components excluded from delivery and cleanup.

---

## Status

| Area | State |
|---|---|
| Implementation | **None.** There is no installable shim in this directory. "Confirmed" means the fact and required behavior are in approved scope; every mechanism is a candidate in [baseline-hypotheses.md](baseline-hypotheses.md) unless a story says it is implemented. |
| Requirements | Normative baseline, including provenance and GitOps authority (§18). All requirements decisions are recorded; no requirement is blocked. |
| Scope | Confirmed: cluster inventory OCM-io → Fleet, and Fleet-origin cluster registration Fleet → OCM-io. Other aspects are candidate or excluded, including Fleet's own addon/extension/plugin framework (row 16). Tenant-scoped visibility is not claimable in the baseline: inventory is provider-scoped until Fleet implements workspaces (GAP-24). |
| Baseline hypotheses | Candidate mechanisms and deployment modes; not implemented or approved. |
| Use cases | Practical stories for the two confirmed aspects, mapped to requirements; illustrative, not normative. |
| Upstream enhancements | Options only; none is required, scheduled, or accepted by OCM-io. |
| Decisions | Every requirements decision is recorded; no requirement is waiting on one. |
| Coverage | Mapped by requirement family, with 24 declared gaps in [coverage.md §4](coverage.md#4-declared-gaps). |

Nothing in this directory is approved for implementation except the requirements baseline. Enhancements are options for OCM-io's own process, not commitments.

---

## Reading paths

**If you maintain OCM-io**

1. [requirements.md §1–§2](requirements.md#1-premise) — the premise and C-1 through C-20, including the constraints that keep every upstream change optional.
2. [requirements.md §4.1](requirements.md#41-scope-register) rows 5 and 16 and [§19](requirements.md#19-non-goals-g) G-15 — OCM-io addon enablement is OCM-io's; Fleet's own addon/extension/plugin framework is not mapped.
3. [use-cases.md §1](use-cases.md#1-how-to-read-a-story) and [§7](use-cases.md#7-ocm-io-enhancement-user-stories-and-how-they-fare) — the status vocabulary, then the existing OCM-io user stories and what the shim preserves of each.
4. [analysis.md §1–§3](analysis.md) — what the two systems are and how the APIs differ.
5. [enhancements.md §1–§4](enhancements.md) — the eligibility test and the enhancement register.
6. [requirements.md §18](requirements.md#18-provenance-and-source-authority-pa-go) — what the shim may and may not claim about provenance and GitOps, and why neither requires an OCM-io change.
7. [coverage.md §4](coverage.md#4-declared-gaps) — what the shim declares it cannot do without you.
8. [decisions.md §3](decisions.md#3-solution-questions-sq) — the solution questions, including packaging and enhancement vehicles.

The key question to ask of this directory: *is any recorded enhancement not independently valuable to an OCM-io user who never installs Fleet?* If yes, it does not belong in the enhancement register.

**If you build Fleet or the shim**

1. [requirements.md](requirements.md) — §1–§4 (including §4.2), §6, §12, and §18 first; those bind the confirmed increment. §5 applies only to addon packaging, and the delivery-only material is identified in §4's "What binds now": §8 D-1–D-8, §10 W-1–W-28, §11 P-3/P-5/P-8/P-10, the delivery row of §15, and V-10's delivery cases. D-9, W-29, and P-9 bind now. Read the whole document before implementing; for the addon boundary, start with §4.1 rows 5 and 16 and §19 G-15. V-8 is the acceptance checklist for the confirmed scope.
2. [use-cases.md](use-cases.md) — the situations the implementation must handle, and the negative cases it must refuse.
3. [baseline-hypotheses.md](baseline-hypotheses.md) — candidate deployment modes and the sixteen fallback hypotheses.
4. [coverage.md](coverage.md) — which requirement is satisfied by which path, and which are gaps.
5. [analysis.md §2](analysis.md#2-why-introduce-fleet-to-an-existing-ocm-io-installation) — the benefits B and the per-mode view behind those claims.
6. [requirements.md §18](requirements.md#18-provenance-and-source-authority-pa-go) and [use-cases.md §4](use-cases.md#4-ocm-io--fleet-use-cases) (UC-O15, UC-O16, UC-O19, UC-O20) — the provenance and GitOps boundary, and the evidence each path can actually provide.
7. [decisions.md §2](decisions.md#2-requirements-decisions-q) — the recorded answers behind each conditional requirement.
8. [enhancements.md §10](enhancements.md#10-sequencing) — which options depend on which, and in what order they could follow.

**If you operate the result**

1. [requirements.md §4.2](requirements.md#42-the-confirmed-increment) — what the confirmed increment is, what it does not do, and the five declared losses.
2. [use-cases.md §6](use-cases.md#6-a-day-in-the-life) — adoption, change, outage, and removal as an operator sequence.
3. [baseline-hypotheses.md §2](baseline-hypotheses.md#2-where-the-shim-runs) — the four deployment modes and what each can observe.
4. [baseline-hypotheses.md §4](baseline-hypotheses.md#4-packaging-and-compatibility) — baseline, enhanced, mixed, and unsupported combinations.
5. [use-cases.md §4](use-cases.md#4-ocm-io--fleet-use-cases) — the provenance and GitOps stories (UC-O15, UC-O16, UC-O19, UC-O20) show which evidence exists on each path and which is absent (terms: glossary above; requirements §18).
6. [coverage.md §4–§5](coverage.md#4-declared-gaps) — all declared gaps and the decisions that shape what you can trust.
7. [requirements.md §15](requirements.md#15-scale-and-performance-n) — the declared scale profile a release must validate before support is claimed.

---

## Review in ten questions

Read [requirements.md](requirements.md); it is the normative source. These ten questions are the review; each names where the answer lives.

1. **What is confirmed?** Two aspects: cluster inventory OCM-io → Fleet, and Fleet-origin cluster registration Fleet → OCM-io. Nothing else (requirements §4 and §4.2, Z-1 – Z-3, AD-6).
2. **Who owns each fact?** OCM-io owns cluster identity, membership, labels, claims, `ManagedCluster` status, OCM-io addon enablement, and placement decisions; Fleet owns the records it creates — targets, placements, deliveries, inventory items; no field is co-owned (requirements §6, X-1 – X-12).
3. **How is a cluster identified and kept fresh?** No UID correlation; every mapped value declares its source and read time; stale is never empty (requirements §7, §11, I-2, S-12).
4. **Can Fleet create membership?** Only by driving OCM-io's own registration contract; a fabricated `ManagedCluster` is forbidden (requirements §12, F-1, M-3; GAP-23).
5. **What does provenance prove, and where?** Only what a verifier checks; inventory is an observation, never an attestation; the OCM-io work-agent path is downgraded (requirements §18, PA-1 – PA-3).
6. **What happens when GitOps owns a field?** The applier remains authoritative; the shim surfaces drift and never becomes a second writer (requirements §18, GO-1 – GO-3).
7. **Are OCM-io addons and Fleet addons the same thing?** No. OCM-io addon enablement is OCM-io's (row 5); Fleet's own addon/extension/plugin framework has no OCM-io object counterpart and is excluded (row 16, G-15). That exclusion is a scope decision, not a statement that Fleet's framework is unfinished; admitting it later requires a new scope decision (X-9), not a Fleet addon upgrade. The candidate mechanism does use Fleet's implemented delivery capability as shim infrastructure, because Fleet has no runtime addon-registration API and the provider is part of the Fleet build; that is a build dependency, not a mapped aspect.
8. **What is not covered?** 24 declared gaps, each with a mitigation or an accepted loss (coverage §4).
9. **Does inventory-only stand on its own?** Yes, with declared losses, and it claims no delivery provenance (F-10, AD-6; coverage §7).
10. **Is any upstream change required?** No; every enhancement is optional and independently valuable to an OCM-io user (C-1, C-15 – C-20; enhancements §1).

If a change to any document makes one of these answers false, the change is wrong.

The decision this set supports: approve or reject the two confirmed aspects, and accept or reject the five declared losses in requirements §4.2. It does not authorize implementation.

---

## Document map

| Document | Contains | Normative? | Changes when |
|---|---|---|---|
| **README.md** | Orientation, glossary, reading paths, review questions, status | No | The directory's shape or status changes |
| **[requirements.md](requirements.md)** | Premise, constraints C, outcomes F, scope Z and register, hosting H, authority X, identity I, desired state D, observed state S, ManifestWork W, delivery P, membership M, tenancy T, reliability R, scale N, lifecycle U, observability/verification O/V, provenance and source authority PA/GO, non-goals G, adoption invariants AD | **Yes** | A requirement is deliberately added, changed, or retired; IDs stay stable |
| **[analysis.md](analysis.md)** | Systems at a glance, benefits B, per-mode benefit view, ManifestWork guarantees and MWRS comparison, current OCM-io freshness, target lifecycle (§5), scope of the analysis (§7) | No | The underlying facts or the rationale change |
| **[decisions.md](decisions.md)** | Recorded decisions DR, requirements decisions Q, solution questions SQ, tensions | No (it points at the normative text) | A decision is recorded or revised |
| **[baseline-hypotheses.md](baseline-hypotheses.md)** | Candidate deployment modes, fallback hypotheses BF, derived constraints PK | No (it must satisfy the requirements) | A candidate fallback is tested, changed, or withdrawn; it never relaxes a MUST |
| **[use-cases.md](use-cases.md)** | Actors, direction map, use cases UC for both confirmed aspects, upstream OCM-io user-story mapping, negative cases | No (it illustrates, and every story cites the normative IDs) | A story is added or its status changes because a requirement, gap, or scope decision changed |
| **[enhancements.md](enhancements.md)** | Eligibility test, solution families, enhancements E, rejected solutions RS, delivery vehicle, strategic consequences, enhancement sequencing, traceability | No | An option is added, adopted by OCM-io, or withdrawn |
| **[coverage.md](coverage.md)** | Requirement-to-path mapping, per-requirement exceptions, declared gaps GAP, decision dependencies, verification coverage, benefit claims, register maintenance (§8) | No (it is the ledger) | A requirement, path, gap, or decision changes |

The dependency direction is one-way: **requirements** would be satisfied by candidate paths in **baseline-hypotheses** and improved by **enhancements**; **analysis** supplies facts and rationale; **decisions** records choices; **coverage** records the result. No lower document may relax a higher one. All cross-references use stable IDs (`C-1`, `E-2`, `BF-3`, `Q-4`, `GAP-2`), so moving text between files does not break them.

---

## Glossary

**OCM-io terms**

- **Hub / spoke** — the OCM-io control-plane cluster and the managed clusters it registers.
- **Klusterlet** — the spoke agent: `registration-agent`, `work-agent`, and addon agents.
- **`ManagedCluster`** — an OCM-io registration and status object for a cluster. The hub itself can be registered as one.
- **`ManifestWork`** — the namespaced, per-cluster bundle of manifests the work-agent applies.
- **`ManifestWorkReplicaSet` (MWRS)** — an OCM-io object that fans one work template out to clusters selected by `Placement`.
- **`AppliedManifestWork`** — the spoke-local ledger of what the work-agent applied; not a hub-side status object.
- **`ManagedClusterAddOn` / `ClusterManagementAddOn`** — per-cluster and cluster-wide addon registration and configuration.
- **`Placement` / `PlacementDecision`** — OCM-io's cluster selection objects.
- **Addon (OCM-io)** — an addon managed by OCM-io's addon-framework: a `ClusterManagementAddOn` for cluster-wide registration and configuration, a `ManagedClusterAddOn` per cluster, an addon agent on the spoke, and a lease and conditions on the hub. It is not a Fleet addon. The shim itself may be packaged as one (H-1 – H-15).
- **Addon-framework** — OCM-io's mechanism for delivering and running an addon on selected clusters.

**Fleet terms**

- **Resource manager** — the Fleet server that owns Fleet's APIs, persistence, authorization, and orchestration.
- **Addon (Fleet)** — Fleet's platform extension unit: a Fleet-internal capability bundle (a descriptor declaring managed-resource, inventory-resource, and/or delivery capabilities; UI plugin and CLI extension are design-only), with an internal Defined → Enabled → Connected lifecycle and no runtime addon-registration API. It is not a Kubernetes object or CRD and has no per-cluster enablement. In this checkout addons are compiled into the resource manager and tracked in memory; the design allows out-of-process addons with their own trust boundaries. It is not an OCM-io addon and has no OCM-io object counterpart (scope row 16, G-15).
- **Extension resource** — a Fleet addon-defined API type that Fleet compiles when the Fleet addon connects, carrying management and/or inventory capabilities; a **managed resource** is the variant that declares management capability. It is served under the Fleet addon's proto package and dual-registered under the platform's canonical service; it is not mapped to OCM-io (scope row 16).
- **Fulfillment** — Fleet intent: manifest × placement × rollout.
- **Deployment** — Fleet's API object recording delivery intent and its lifecycle state; each deployment owns exactly one fulfillment, and deleting the deployment deletes that fulfillment and the outputs it owns. The installation as a whole is called the **Fleet installation**; the **resource manager** is its stateful server. "Deployment object" means the API object.
- **Target** — a thing Fleet can deliver to: in this checkout a Kubernetes or kind cluster, or a target registered by a Fleet delivery addon. Fleet's domain model also names `platform` and `local` types, but no delivery capability in this checkout registers them.
- **Target type** — Fleet's delivery identifier for a target, declared by the delivery capability that handles it: `kubernetes` and `kind` in this checkout, or any type a Fleet addon declares. It determines which delivery agent can deliver and is a Fleet concept, not an OCM-io object. A Fleet addon-registered target is a target whose type was declared in a Fleet addon's connect input.
- **Fleetlet** — Fleet's target-side transport and shared-services component. It connects local Fleet addon processes to the resource manager and is the designed location for target-side delivery verification, the only locus that survives a compromised resource manager; that verification is designed, not yet implemented. Resource application and domain behavior are supplied by Fleet addons. In this directory, "the fleetlet reads" or "the fleetlet observes" means the target-side shim behavior reached through the fleetlet. It is a shim component wherever Fleet runs a target-side component; Fleet's platform-side delivery agent is the alternative where none is installed.
- **Delivery** — Fleet's record of intent accepted and acted on for a target.
- **Inventory / observation** — Fleet's per-target facts, properties, observations, and conditions.
- **Fleet placement** — Fleet's selection of targets within a fulfillment, by one of three strategies: an explicit target list (`static`), every target (`all`), or exact label match (`selector`). Only the label selector consumes projected OCM-io labels, and it cannot reproduce an OCM-io `Placement` (GAP-8).
- **Workspace / tenant** — Fleet's designed partition for provider and consumer access, not yet implemented in the baseline (GAP-24). OCM-io has no equivalent.

**Shim terms**

- **Shim** — see above: the whole boundary surface, including the Fleet installation and fleetlet.
- **Cluster inventory** — the inventory *of clusters*: identity, membership presence, and the labels and claims used for placement. It is not resource inventory collected from inside clusters (scope row 9) and not workload or health state (Z-12).
- **Aspect** — a coherent set of facts that may be mapped, such as cluster inventory or OCM-io addon enablement. Listed in requirements §4.1.
- **Coverage** — which aspects are mapped, for which clusters, in which direction.
- **Fidelity** — how much of an aspect's meaning survives the mapping: preserved, approximated, or unavailable.
- **Authority** — which side owns a given aspect of a given object at a given time.
- **Baseline** — unmodified OCM-io within the declared supported versions.
- **Enhancement** — an optional, general OCM-io change that improves fidelity but is never required.
- **Declared gap** — a limitation the shim surfaces rather than hides; recorded in coverage §4.
- **Mode** — a deployment shape: inventory-only (cluster inventory and Fleet-origin registration, with no workload delivery), delivery, read-only mirror, or hub-only constrained. A self-registered hub is not a separate mode.
- **Registration fulfillment** (the *registration unit* in M-8) — the one Fleet fulfillment per adopted cluster whose delivery carries that cluster's identity, labels, and projected claims to the shim's registration target; a refresh is a successor fulfillment that upserts the same target ID, and deleting the deployment that owns the current one un-adopts exactly that cluster (BF-10, GAP-12).
- **Registration target** — the shim's Fleet target that receives registration manifests; it is infrastructure, never adopted, and never shown as tenant inventory.
- **Registration provider** — the shim-owned Fleet delivery provider that, in the candidate mechanism, is registered through a delivery-only Fleet addon in the resource manager deployment and returns a `ProvisionedTarget` carrying the adopted target's ID and labels; it is shim infrastructure, not a mapped Fleet addon, and it is not OCM-io's klusterlet `registration-agent`.
- **Registration delivery** — the Fleet delivery performed by a registration fulfillment; it carries no cluster workload.
- **Un-adoption** — deleting the deployment that owns a cluster's current registration fulfillment, which removes exactly that cluster's target and its `target:<id>` inventory item. It is durable: while the cluster remains in scope the shim does not silently re-adopt it, and re-adoption is explicit (M-8).
- **Adopted cluster** — a cluster present in OCM-io inventory that the shim has made a Fleet target. OCM-io keeps ownership of its identity, membership, labels, and claims; the Fleet target is a projection.
- **Fleet-origin cluster** — a cluster introduced through Fleet and registered into OCM-io through OCM-io's own contract. It becomes a real `ManagedCluster`; the shim never fabricates one, and a target that cannot register stays unregistered (GAP-23).
- **Unregistered target** — a Fleet-origin target with no OCM-io membership, because registration could not complete. It stays visible in Fleet and visibly unregistered; there is no placeholder `ManagedCluster` (GAP-23).
- **Shim status** — the shim's own per-target and per-aspect report of coverage, freshness, registration state, and errors. It is not an OCM-io object status and not a Fleet target status; the requirements fix what it must say (F-8, Z-6, Z-7), not its API.

**Provenance and GitOps terms**

- **Provenance** — cryptographic evidence that a principal authorized exact content for an exact purpose, and why the target's trust state recognizes that principal. It is evidence about a delivery, not a property of an observed fact.
- **Attestation** — the self-contained bundle a delivery verifier checks: signed intent, the exact delivery action, and the material needed to resolve the signer.
- **Signer / trust bundle** — the principal whose signature authorizes content, and the trust roots and enrollments a verifier consults to recognize that principal. Fleet keeps both as trust state; neither is projected into OCM-io.
- **Verification locus** — where evidence is checked: target-side, platform-side before apply, or not at all. The locus determines what a compromised resource manager can and cannot do.
- **Evidence class** — what backs a mapped value or delivery: authenticated delivery evidence, an observed fact with source and freshness, or no evidence.
- **Provenance state** — the verification outcome for a delivery to a target: verified, failed (the verifier rejected the evidence), expired (the attestation's validity window has passed), absent (including a downgraded path with no verifier), or paused (held before a verification result exists). Visibility is required per delivery and per target, but Fleet exposes no durable, queryable per-delivery result, so the shim reports what it observed at deployment level or on the live event stream and marks the per-delivery record partial (GAP-21).
- **Provenance-downgraded** — a declared delivery path whose applier has no Fleet verifier. Its deliveries report provenance state absent and are never described as authenticated; the downgrade names the verification that is missing (PA-3).
- **Live delivery event stream** — Fleet's UI WebSocket, which broadcasts per-delivery progress and result state with a message to connected clients. It is unauthenticated, retains no history or replay, and leaves the envelope's target fields unpopulated — the current implementation embeds the target in the delivery ID, but the stream cannot be relied on as a durable, tenant-facing read.
- **Source claim** — a statement of where content says it came from, such as a repository, revision, and applier identity. It is not evidence that a principal authorized the exact content delivered; only a configured provenance profile can bind it to that content.
- **GitOps authority** — the applier's desired state is authoritative for every field it contains; the shim treats those fields as externally owned and never writes them.
- **Field ownership** — the writer whose desired state includes a field, recorded per field manager where the API exposes it. It is distinct from authority over a mapped aspect (X-12): a GitOps applier may own a field set inside an object OCM-io stores.
- **Drift** — divergence between a writer's desired state and the observed object. When another writer owns the field, drift is surfaced rather than converged.

---

## Conventions

- **MUST / MUST NOT** are non-negotiable. **SHOULD** is expected unless a documented reason prevents it. **MAY** is optional. **OPEN** is not used in the current baseline: every requirement decision is recorded in decisions.md §2.
- **Identifiers are stable.** Gaps in numbering (for example, no SQ-6) are intentional and preserve history; a retired identifier may remain as a closure note rather than be reused.
- **ID families:** `C` constraints, `F` outcomes, `B` benefits, `Z` scope, `H` hosting, `X` authority, `I` identity, `D` desired state, `S` observed state, `W` ManifestWork, `P` delivery paths, `M` membership, `T` tenancy, `R` reliability, `N` scale, `U` lifecycle, `O` observability, `V` verification, `PA` provenance and attestation, `GO` GitOps and source authority, `G` non-goals, `AD` adoption invariants. Solution options: `BF` baseline fallback hypothesis, `E` enhancement, `RS` rejected solution, `PK` packaging/compatibility. Decisions: `DR` recorded, `Q` requirements, `SQ` solution. Coverage: `GAP`. Use cases: `UC`.
- **Partial scope is a feature.** "Not mapped" is a first-class state and must never be presented as health.
- **Bare "addon" means an OCM-io addon; "Fleet addon" means Fleet's own extension unit.** The shim's packaging is always "the OCM-io addon that carries the shim" or "the shim's OCM-io addon".

---

## The first increment

The confirmed increment has two parts. **Cluster inventory, OCM-io → Fleet:** existing OCM-io clusters become Fleet targets with the identity, membership, labels, and claims Fleet placement needs. Fleet matches exact label values only, so this is a projection, never an equivalent `Placement`. **Fleet-origin registration, Fleet → OCM-io:** a cluster introduced through Fleet is registered through OCM-io's own registration contract, appears as a real `ManagedCluster`, and is correlated to its Fleet target. Registration means the cluster runs OCM-io's klusterlet; a cluster that cannot do so stays visibly unregistered.

```
OCM-io -> Fleet   ManagedCluster labels/claims --read--> shim controller --delivery--> Fleet target
                                                                                      (exact-match labels)

Fleet -> OCM-io   Fleet target --shim drives--> OCM-io registration contract --> real ManagedCluster
```

The increment must be complete, supported, and useful on its own within that scope (F-10, AD-6). It requires no OCM-io change, no enhancement, and no target-side shim component. In the candidate mechanism, OCM-io-origin targets are created through Fleet's own registration delivery, which carries no workload and runs nothing on the cluster. That mechanism has one Fleet-side dependency: the delivery-only registration provider, which is part of the Fleet build because Fleet has no runtime addon-registration API (BF-10). It is shim infrastructure, not a mapped Fleet addon (row 16).

The increment makes Fleet's target records and selection inputs correct, and makes Fleet-origin clusters visible in OCM-io. It is for an organization that already runs OCM-io and wants Fleet placement to consume the same cluster set, without re-registering existing clusters or moving delivery off OCM-io. It does not move workload delivery off OCM-io, and it claims no delivery provenance for cluster facts: they are observations with a source and read time, never attestations (PA-1). Its declared losses are GAP-8, GAP-11, GAP-12, GAP-23, and GAP-24 (requirements §4.2; coverage §4, §7).

Which benefits that increment can honestly claim — and which must wait for delivery or workload scope — is set out in [coverage.md §7](coverage.md#7-benefits-and-the-first-increment). Inventory-only is a complete supported mode, but it is not a reason to adopt the shim for users whose only need is rollout or delivery provenance; if those users are the target, the scope baseline must change.
