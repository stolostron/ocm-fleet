# OCM-io × OCM Fleet Shim — Baseline Hypotheses

Status: **working hypotheses, not an approved solution**. This document explores how the shim could satisfy the requirements on unmodified OCM-io, with no upstream enhancement. It selects no implementation and approves nothing; it names candidate deployment shapes and fallback mechanisms so that coverage has something concrete to point at.

Requirements are in [requirements.md](requirements.md). Upstream options are in [enhancements.md](enhancements.md). Recorded decisions are in [decisions.md](decisions.md). The requirement-to-path mapping, including declared gaps, is in [coverage.md](coverage.md). Benefit claims are in [analysis.md §2](analysis.md#2-why-introduce-fleet-to-an-existing-ocm-io-installation).

---

## 1. Rules

- **Baseline first.** The shim must work on baseline OCM-io (C-1, C-16). A solution that only works with an upstream enhancement does not solve the baseline problem; it accelerates it.
- **Hypotheses, not commitments.** Every mode and every BF below is a candidate way to meet a requirement. None is selected, scheduled, or approved; an implementation may satisfy the requirement differently, and V-1 decides when it is actually covered.
- **The shim runs as part of the Fleet installation.** The expected shape is the Fleet resource manager plus adapters, and the fleetlet on every cluster Fleet delivers through it (H-14). A hub-only adapter is complete only for aspect facts that OCM-io already surfaces on the hub, such as cluster inventory; it may not be assumed for an aspect that needs spoke-local observation, and fleetlet is never an outside observer where present. "The hub cannot see it" is a statement about one deployment shape, not about the boundary.
- **Detectable.** Every mode difference must be observable and declared (C-17, Z-16).
- **Traceable.** Each fallback names the requirements it satisfies and the requirements it cannot fully satisfy.
- **Requirements win.** If a solution needs a MUST relaxed, the requirement document changes deliberately; the solution does not quietly assume it away.

---

## 2. Where the shim runs

Presence of a spoke-side component follows the aspect, not the installation (Z-17, H-14):

| Mode | Hub side | Spoke side | What it can observe |
|---|---|---|---|
| **Inventory-only** (F-10) | Fleet resource manager with its inventory controller and delivery-only registration provider; OCM-io registration approvals for Fleet-origin clusters (bootstrap CSR, `ManagedCluster` acceptance) where the installation does not automate them | none required by the shim; a Fleet-origin cluster runs OCM-io's klusterlet after registration | `ManagedCluster` facts from the hub; no cleanup or resource state |
| **Delivery** | Fleet + OCM writer/reader | **fleetlet, as an OCM addon**; Fleet's platform-side delivery agent is the no-fleetlet variant | its own deliveries, `AppliedManifestWork`, resource finalizers, local status |
| **Read-only mirror** | Fleet adapter | none required, or fleetlet where already present | OCM-io conditions and feedback; no local detail |
| **Hub-only constrained** | Fleet adapter, no target-side component | none | only what OCM-io surfaces on the hub; every gap declared |

The first three are normal. The fourth is supported but is the exception, and it is the only one where E-2 would matter to the shim.

Provenance follows the same split (terms are defined in [requirements.md §18](requirements.md#18-provenance-and-source-authority-pa-go) and the [README glossary](README.md#glossary)). Inventory-only has no delivery to attest: its facts are observations with source and read time, never evidence (PA-1). In the expected shape, delivery verifies target-side only where a fleetlet is present; a platform-side verifier protects against external forgery but not against the resource manager; a delivery applied by OCM-io's work agent has no verifier and is reported provenance-downgraded (PA-2, PA-3). Read-only mirror observes the same OCM-io apply results and creates no Fleet evidence. Hub-only constrained mode has no target verifier; its verification, where it exists, is platform-side, with the residual exposure declared. The delivery row's fleetlet is the expected target-side shape. Fleet's platform-side Kubernetes delivery agent is the no-fleetlet variant: it applies to the target's API server using credentials from the target's properties on the attested path or the caller's token on the unsigned path, and reports delivery results; it has no target-side agent, so it reads no `AppliedManifestWork`, and its verification is platform-side only when the deployment carries an attestation. A deployment created without a signature reaches the same agent with no verifier at all and is reported provenance-absent. The shim names the locus and does not treat either path as equivalent to a fleetlet.

**How targets enter Fleet.** Fleet exposes no target-create, relabel, or deregister API. A target is created in one of two implemented ways: declared as a static target in a Fleet addon's connect input, registered with `RegisterOrVerify` and rolled back if the connect fails, with labels fixed for the target's life; or upserted from a delivery's provisioned-target output together with a `target:<id>` inventory item, where a new delivery replaces the target's content and deleting the deployment that owns the fulfillment removes every target it still owns (GAP-9). A third path — registration by a fleetlet over its control channel — is defined in the protocol but has no server handler. Adoption needs label refresh and removal, so it uses the delivery-output path through one dedicated registration fulfillment per cluster (BF-10, M-9). A static target cannot be relabeled and survives the Fleet addon's Disconnect and disable; it is not an adoption path. A target ID must not be registered both statically and through a delivery, because cleanup deletes it with the delivery.

The registration delivery is Fleet-internal: it carries no workload, runs no component on the spoke, and does not make inventory-only a delivery mode. "Delivery" in the mode table means delivering OCM-io workload intent.

**The hub cluster is not a separate mode.** OCM-io can register its own hub as a `ManagedCluster` and install addons on it. When that is done, the hub cluster is the delivery row: fleetlet can run there through the same addon mechanism, observation is local, and the same rules apply. "Hub side" and "spoke side" in the table are logical roles — on a self-registered hub they are the same cluster. Two exclusions follow (H-15, X-11): OCM-io's own control-plane components and the shim's own components are not workload targets, and a self-registered hub never counts as implicit opt-in for any aspect.

The inventory-only mode is the confirmed first increment. It must remain a complete, supported mode for the inventory facts in Z-12 (F-10, AD-6), with its declared losses (GAP-8, GAP-11, GAP-12, GAP-23, GAP-24), not a stepping stone that only becomes useful once delivery is admitted. [use-cases.md](use-cases.md) traces what each mode can and cannot do for an operator.

---

## 3. Candidate fallback hypotheses (BF)

Candidate approaches for when no enhancement is present. Each is a hypothesis the requirements imply, named so coverage can refer to it; none is a selected design.

- **BF-1 Correlation without object metadata.** A durable external mapping keyed by cluster identity, group/version/kind, namespace/name, and a concrete incarnation marker. Detect name reuse and refuse inheritance (I-1, I-3).
- **BF-2 Freshness without `observedGeneration`.** Store the source generation and timestamp on the shim side; treat missing observations as unknown, never current (S-3).
- **BF-3 Cleanup without hub inventory.** Default: fleetlet reads `AppliedManifestWork` under granted RBAC. The record names the work, hub hash, agent, and applied resources; it carries no apply timestamp and no component version, so the shim reports the time it read the record and its own version. Its `evictionStartTime` is reported as an eviction signal when the work is missing from the hub or the hub hash changed. The shim reports unavailable when the fleetlet stops. Hub-only constrained mode: never scrape `AppliedManifestWork`, never infer deletion completion; rely on OCM-io finalizers and declare cleanup state unobservable (W-17, R-9).
- **BF-4 Existence versus health.** `Applied` reports the last apply attempt; `Available` reports that the object exists, not that it is ready. The baseline reports both as apply-path signals, never as health, and reports health or completion only from an explicit condition rule, naming the rule. Where no rule exists, health is "not reported" rather than inferred (W-14, W-15, S-10).
- **BF-5 Per-target without MWRS status.** Read child ManifestWorks, identified by the placement-name and owner-key-hash labels and the owner annotation (with the deprecated MWRS-name label present only when its value fits the 63-character label limit) rather than owner references, with pagination, tolerating missing or terminating children (W-18).
- **BF-6 Identity without a stable claim.** Composite key including creation timestamp; explicit duplicate detection (I-2, I-7).
- **BF-7 Read-only status.** Never write OCM-io controller-owned status; mirror and merge on the Fleet side (X-7, S-9).
- **BF-8 Path change without pause.** Ordered phase transitions and bounded dual-path windows for intent introduced after the transition, with conflict detection instead of prevention (P-5).
- **BF-9 Coverage reporting.** A registry of aspects, direction, authority, and mode, exposed next to health so "not mapped" cannot be mistaken for "healthy" (Z-6, Z-14, O-7).
- **BF-10 Target adoption through a per-cluster registration fulfillment.** Fleet has no target update or deregistration API, so adoption is built on the one path that can change and remove a target: a delivery output. A shim-owned Fleet delivery provider, registered through a delivery-only Fleet addon and run in the resource manager deployment, delivers a per-cluster registration manifest to a singleton registration target it registers at connect time and returns a `ProvisionedTarget` carrying the target ID and labels; Fleet records the delivery-owned `target:<id>` inventory item. Fleet has no runtime API for registering a new addon: addons are compiled into the resource manager and enabled at startup, so this provider is part of the Fleet build. It uses Fleet's implemented delivery capability and connect-time target registration; that build dependency is the increment's only Fleet-side prerequisite, and no OCM-io change is involved. The Fleet addon declares only a delivery capability: no managed-resource, inventory-resource, UI, or CLI capability, so it exposes no consumer-visible extension resource or plugin, and it is not a mapped Fleet addon (M-8). The manifest carries the cluster identity, labels, and projected claims. One registration fulfillment per cluster makes the fulfillment the unit of both refresh and un-adoption: a refresh is a successor fulfillment whose delivery upserts the same target ID, and deleting the deployment that owns the current fulfillment removes exactly that target (M-8, M-9, GAP-12). Fleet cannot update or invalidate a fulfillment in place, so the successor is created and delivered before the predecessor is deleted; reversed, the predecessor's cleanup would remove the target until the successor re-created it. If the successor fails or never completes, the predecessor is kept and the refresh is reported as failed, so the target retains its last delivered content instead of disappearing. The Fleet addon's record and descriptor, its registration target, and its fulfillments are shim infrastructure, carry no cluster workload, and are excluded from mapping and tenant inventory; they are not a mapped Fleet addon (scope row 16). Static registration of an adopted cluster target in a Fleet addon's connect input is rejected because its labels are frozen and it cannot be removed; the singleton registration target is exempt because it is never relabeled or removed; a shared registration fulfillment is rejected because deleting it would remove every target it produced; and the fleetlet `RegisterTarget` event is not a path until the resource manager handles it. The mechanism must be validated because it costs one fulfillment and one delivery per adopted cluster.
- **BF-11 Claim projection.** Project each `ClusterClaim` into a declared target label, preserving name and value and refreshing it through the cluster's successor registration fulfillment (BF-10). Fleet selects on exact label equality, so only `matchLabels`-style selection survives; declare what is lost: claim-selector semantics, `matchExpressions`, and the distinction between a claim and a label (M-6, Z-12). A claim name that collides with a label key resolves by a declared precedence and is reported, never left to map iteration order or allowed to alternate between refreshes. Taints are not projected: a taint permits or forbids selection through toleration, which a label cannot express, so taint- and toleration-based selection is declared unavailable (GAP-11).
- **BF-12 Complete-aware propagation.** Treat `Complete` as a freeze. Never set it on intent that can change, and do not read a later edit as applied while a work-level or per-manifest `Complete` still holds (W-27).
- **BF-13 Conflict surfacing.** Under `ServerSideApply`, read `Applied=False` with a conflict message as a conflict to surface, not as a transient error to retry with force (W-28, F-5). When a GitOps applier repeatedly reverts a shim write instead of producing a single conflict, the same rule applies: surface drift, name the competing manager where the API exposes it, and refuse to escalate (U-7, GO-6). Fleet's platform-side delivery agent applies with server-side apply without force, so a Fleet delivery that meets another manager's field fails with a conflict rather than an overwrite; the shim reports the failed delivery where a component receives the result first-hand and does not force the apply, naming the competing manager where the conflict detail reaches it. Fleet retries the failed delivery itself on its configured interval, so the same non-force attempt repeats until the intent or the ownership changes; the shim adds no retry, reports the repetition as the same drift, and declares the loop (GO-1, GO-2; GAP-21, GAP-22).
- **BF-14 Provenance downgrade on the OCM-io path.** When Fleet intent reaches a target through OCM-io's work agent, no Fleet verifier runs at the target. The shim records the verification locus as none, marks the delivery provenance-downgraded, and reports the OCM-io apply result as observed state with source and freshness. It does not synthesize an attestation from `Applied` or `Available`, and it does not inject an attestation reference into the work object. A general work-agent verification capability (E-8) is required to remove the downgrade; delivering through a fleetlet instead changes the path rather than verifying this one (PA-2, PA-3, PA-7, T-6; GAP-13).
- **BF-15 Declarative adoption records.** The shim's per-cluster registration input is a declarative record carrying the cluster identity, labels, and projected claims. The shim reconciles the record set to the set of registration fulfillments (BF-10), so applying a record adopts or refreshes one cluster and deleting it un-adopts exactly that cluster. The records are shim-owned and separate from the applier's OCM-io manifests, so adopting a cluster never edits that cluster's own objects. This is a candidate path, not a baseline guarantee: if only an imperative API exists, declarative adoption is a declared limitation (GO-3, GO-4; GAP-17).
- **BF-16 Fleet-origin registration through OCM-io's contract.** A Fleet target with no OCM-io counterpart is registered by driving OCM-io's own registration contract: the cluster owner installs klusterlet with a bootstrap credential; the shim completes whatever approval the installation does not automate — approving the bootstrap CSR and accepting the resulting `ManagedCluster` — and correlates the registered identity to the target (BF-1, BF-6). The shim never creates a `ManagedCluster` placeholder for a cluster that has not registered, and it unregisters only membership it created. A target whose cluster cannot run klusterlet stays visible as unregistered (F-1, M-3; GAP-23).

### 3.1 What the baseline cannot do

These are declared limitations, not defects to be hidden. They are recorded as gaps in [coverage.md](coverage.md) and are improved — never removed — by the enhancements in [enhancements.md](enhancements.md).

- A status without an `observedGeneration` cannot distinguish stale from fresh with certainty (S-3); source timestamp on the shim side is the best available.
- In hub-only constrained mode, spoke-local cleanup state is unobservable (W-17, S-7, R-9).
- `Available` reports that the object exists; it cannot stand in for health, which is "not reported" unless a condition rule is configured (W-14, W-15, S-10).
- `AppliedManifestWork` carries no apply timestamp and no component version; cleanup freshness is the reader's observation time (W-17).
- Fleet placement matches exact label values only, so claim-based selection is approximated by projection with `matchExpressions` lost, and taint- and toleration-based selection is unavailable (M-6, GAP-8, GAP-11).
- Fleet has no target update or deregistration API: a statically registered Fleet target keeps its registration labels and cannot be removed, while a delivery-produced target changes only when a delivery runs and is removed only with the fulfillment that still owns it. Adoption therefore costs one registration fulfillment per cluster (M-9, GAP-12).
- MWRS is alpha and off by default, so it cannot be a required fan-out path (W-25, W-26).
- Co-owned OCM-io status cannot be written safely; the baseline mirrors read-only (X-3, X-7, S-2).
- There is no operator-controlled pause primitive, so a path change for new intent is bounded and conflict-detected, not prevented (P-5, U-9).
- Cluster identity survives only as well as the composite key does (I-2, I-3, M-7).
- No Fleet provenance is verifiable by an OCM-io work agent; without a target-side verifier the delivery is downgraded, not authenticated (PA-3, GAP-13).
- Inventory observations are not attestations; source and freshness are the strongest claim available (PA-1, GAP-14).
- GitOps-owned fields cannot be declared in-object; the shim avoids them and surfaces repeated correction as drift (GO-1, GO-2, GAP-16).
- A failed delivery is retried by Fleet without an attempt cap and without a deployment-level failure state; the shim cannot stop the loop, only report the conflict and its repetition (GO-6, GAP-22).
- A Git commit is a source claim, not provenance, unless a configured profile binds it to the exact content (GO-5, GAP-15).
- The baseline exposes no shim-owned declarative API and Fleet's API has no update, patch, or apply surface, so declarative adoption records are a candidate path rather than a guarantee; C-3 permits an additive shim API (GO-3, GAP-17).
- Fleet's delete path produces no verifiable signed removal evidence, and Fleet has no evidence log; removal and historical verification are recorded, not attested (PA-5, PA-6, GAP-18, GAP-19).
- An attested deployment is bound to its full signed manifest set; a target that filters manifests by accepted type rejects the delivery instead of receiving a verified subset (PA-4, GAP-20).

---

## 4. Packaging and compatibility

### 4.1 Compatibility-state matrix

| State | OCM-io baseline | Enhancements | Shim behavior |
|---|---|---|---|
| **Baseline** | Supported minimum | none | Full declared scope, degraded fidelity where noted |
| **Enhanced** | Supported | one or more | Same scope, improved fidelity; no behavior change to OCM-io |
| **Mixed** | Multiple versions | per cluster | Per-cluster capability detection; coverage reported per cluster |
| **Unsupported** | Below minimum or unknown combination | — | Fail safe: no writes, explicit condition, no guessing (C-20) |

### 4.2 Constraints a candidate solution inherits

These restate requirements (C-16, Z-14, V-8, V-9) rather than adding new ones; they are listed here so a candidate design can be checked against them.

- **PK-1** Capability detection MUST be per cluster and per enhancement, not global.
- **PK-2** Each mapped aspect MUST have a baseline path and, if enhanced, an enhanced path. Neither may silently change authority.
- **PK-3** Coverage and fidelity MUST be reported per compatibility state (Z-14).
- **PK-4** An enhancement appearing or disappearing MUST be observable and MUST NOT silently alter writes or status (Z-16).
- **PK-5** The shim MUST NOT require an upgrade to deliver any approved scope (B-1, C-16).
- **PK-6** The test matrix MUST include baseline, enhanced, mixed, and unsupported combinations (V-8, V-9).

### 4.3 Addon packaging

OCM-io addon packaging is optional (H-1, G-12). When used, it follows OCM-io's addon framework exactly (H-2, C-13), and packaging does not change scope, authority, or tenancy (H-12). The expected shape is fleetlet as an OCM addon on delivery targets, with the resource manager serving Fleet from outside OCM-io's addon mechanism; the fleetlet connects outbound and multiplexes its channels over that connection, so the platform needs no inbound path to the spoke. A hub-only deployment has no fleetlet and declares the resulting gaps.

---

## 5. A possible first increment

This is a hypothesis about the smallest useful build, not an approved plan.

1. **No-enhancement baseline.** A first implementation would cover the fallbacks the confirmed aspects activate — BF-1, BF-2, BF-6, BF-9, BF-10, BF-11, and BF-16 — together with the scope registry and the inventory-only mode, assuming no enhancement. In particular it needs an adoption path that does not depend on workload delivery (BF-10), a claim projection (BF-11), and a Fleet-origin registration path (BF-16). The remaining fallbacks are exercised only when their aspects are admitted: BF-3 through BF-5, BF-7, BF-12, and BF-13 belong to delivery or status, BF-8 to path changes for new intent, and BF-14 to delivery provenance; BF-15 is the candidate declarative adoption path; where only an imperative surface is built, GAP-17 declares the limitation. The increment would have to be independently useful (AD-6) and pass its declared-scope verification suite (V-8).

Enhancement options are ordered separately in [enhancements.md](enhancements.md#10-sequencing). Any later increment would have to show a measurable benefit per AD-5 and add no prerequisite (C-16).
