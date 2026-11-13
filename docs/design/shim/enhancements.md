# Enhancements

**Status: options, not commitments.** This document lists changes that would make the introduction cheaper or safer. It has three parts: changes OCM-io could make (E), capabilities the resource manager needs (FC), and solutions that were considered and rejected (RS). No requirement in [requirements.md](requirements.md) depends on an OCM-io change (C-7), and none of the options in section 2 is needed to observe a hub, to enroll a cluster, or to deliver to it. The resource manager capabilities are different: requirements do depend on them, and [coverage.md](coverage.md) tracks each one as a gap until it exists.

## 1. Eligibility test for an OCM-io enhancement

An option belongs in section 2 only if all of these hold.

1. It is valuable to an OCM-io user who never installs the resource manager.
2. It is additive, opt-in, backward compatible, and versioned.
3. It is expressed in OCM-io's vocabulary, not the resource manager's.
4. It does not change who owns an existing OCM-io object.
5. The design works without it, and its absence is a declared mode.

## 2. OCM-io enhancements (E)

Each entry names what it would give OCM-io users in general and which requirement or hypothesis in [baseline-hypotheses.md](baseline-hypotheses.md) it would simplify. E-1 to E-5 and E-10 concern the optional path on which the management plane serves OCM-io agents.

| ID | Enhancement | Value to OCM-io users in general | Helps |
|---|---|---|---|
| **E-1** | **A persistence abstraction in the gRPC server.** OCM-io's gRPC registration proposal already asks for "an abstraction layer for persistence so that we can choose to use either k8s or db". Publishing the per-resource service interface as a versioned extension point would complete it. | Any store can back the agent protocol; hub storage stops being the only option. | BH-23, BH-25 |
| **E-2** | **A conformance suite for the agent protocols.** Tests that any implementation of the event-based work protocol and the gRPC registration flow can run. | Agents are protected from sources that diverge; alternative sources can prove themselves. | K-3, V-3 |
| **E-3** | **Independent work source selection.** The klusterlet operator derives the work agent's source from the registration driver. Letting the two be set separately would let a cluster keep its registration and follow another work source. | Hub migration and staged adoption of the event-based protocol. | BH-24, Z-4 |
| **E-4** | **More than one work source per agent.** A work agent that follows several sources, each owning its own applied work. | Work can move between hubs gradually, and add-on work can stay on one source while other work moves. | K-8, W-7 |
| **E-5** | **A stable source identity for applied work.** Applied work is keyed by a hash of the hub's address. A declared identifier that survives an address change and can be handed to a successor would replace reliance on an eviction grace period. | Deterministic hub restore and hub replacement. | W-7, BH-24 |
| **E-6** | **An implementation class on fan-out objects.** A field on `ManifestWorkReplicaSet` and `Placement` that names the controller implementing the object, in the way ingress and storage classes do. Without one, a kind has a single implementation per API server, and OCM-io's own controller for `ManifestWorkReplicaSet` is enabled by default after v1.4.0. | Alternative schedulers and rollout engines can coexist with OCM-io's own. | K-5, BH-20 |
| **E-7** | **A documented stable cluster identity.** OCM-io reserves the claim name `id.k8s.io` for a cluster's unique identifier. Documenting how it is populated, its uniqueness, and its survival across re-registration would make it usable. | Every inventory, cost, and policy consumer gets an identity that is not the cluster's name. | I-2, I-4, BH-4 |
| **E-8** | **Observed generation on status.** `ManagedCluster`, `Placement`, `ManifestWork`, and `ManifestWorkReplicaSet` have no status-level observed generation. Adding one lets a reader tell a stale status from a fresh one. | Every dashboard and GitOps tool that reads status. | M-3, IS-1 |
| **E-9** | **The requesting user on work objects.** An optional record, set by the hub at admission, of who created or last changed a `ManifestWork`. | Attribution for every OCM-io user, without reading audit logs. | IS-10 |
| **E-10** | **Lease renewal during re-registration.** A registration agent stops renewing its cluster's lease from the moment it starts to register again until the new registration is approved, and the hub then reports the cluster unreachable. Renewing with the credential the agent still holds would keep the cluster available through a planned change. | Hub migration and hub address changes without clusters being reported unreachable. | M-7, BH-24 |
| **E-11** | **A dependable record of each cluster's API address and certificate authority.** `ManagedCluster` can carry both. The registration agent fills them in only when the klusterlet is configured with the cluster's external address, and can change them only while it is bootstrapping. Documenting when the record is set, and keeping it current after an address or certificate change, would make it dependable. | Every tool that has to reach or verify a managed cluster from outside it. | EN-6, I-3, BH-9 |

## 3. Resource manager capabilities needed (FC)

These are not optional for the requirements they serve. Until each exists, the requirement is a declared gap.

| ID | Capability | Needed for | Until then |
|---|---|---|---|
| **FC-1** | A target lifecycle API: create, update labels and properties, remove one target | M-10, M-4 | The workaround in BH-5 |
| **FC-2** | Extensions added at run time: a server add-on and a console plugin registered without rebuilding the resource manager | Shipping and upgrading the integration separately from the resource manager (A-12, U-2, K-11) | The integration is built into the resource manager and its console |
| **FC-3** | Storage for integration-owned records in the management datastore | I-1, DR-11 | No place for correlation records |
| **FC-4** | Fine-grained authorization, tenants with their own role assignments, and workspaces, enforced in the API and in the datastore | T-1 to T-7, T-11, S-14 | Visibility is provider-only (T-7) |
| **FC-5** | Per-tenant limits on rate, concurrency, and stored records | T-5, T-10 | No protection between tenants |
| **FC-6** | Token passthrough with no persisted token | S-4 | The token is stored with the fulfillment |
| **FC-7** | History of change and of observation | IS-7 to IS-10, S-2 | Only current state |
| **FC-8** | Update and declarative apply for intent | GO-9, GO-2 | Delete and re-create |
| **FC-9** | Operation with more than one replica | A-9, R-6 | One replica |
| **FC-10** | Documented and tested backup, restore, point-in-time recovery, failover, and the reconciliation that follows a restore | R-1 to R-4, R-6, R-7, U-7 | The database's own tools, with no procedure |
| **FC-11** | An endpoint that serves OCM-io agents over their published protocols | K-3, W-4, M-7 | The work-agent path is not offered. A klusterlet follows its hub. |
| **FC-12** | Read APIs for targets, inventory, and deliveries, and queries that join them | IS-1, IS-3, IS-4 | The database is the only place to read them |
| **FC-13** | Rollout strategies beyond immediate | W-12 | Progressive fan-out is approximated or refused |
| **FC-14** | Verification of evidence at the target | S-6 | Verification on the platform side only |
| **FC-15** | A capacity model and validation at the declared profiles | N-1, N-3 | No published model; a stress test of orchestration only |
| **FC-16** | Export and import between backends | U-9 | None |
| **FC-17** | Target names unique per workspace instead of per installation | I-7, OQ-10 | Generated names must embed the hub connection |
| **FC-18** | A Kubernetes-facing driver and projector | GO-2, GO-4, GO-7, GO-8, K-5, K-6 | No Kubernetes-facing surface |
| **FC-19** | Delivery that needs no standing administrator credential for a target, and a report of the credentials and rights held per target | S-15, EN-7 | A stored platform credential with administrator rights for each cluster the resource manager creates |
| **FC-20** | Retention for the history of completed orchestration runs | N-3, IS-9 | Every run's history is kept and dominates the datastore |
| **FC-21** | A stable version of the management API, with a declared compatibility commitment | A-11 | An alpha API version |
| **FC-22** | Taking in a Kubernetes cluster that already exists: its address, trust anchor, and credential are verified, the credential is stored and renewed, a target is created, indexing starts, and all of it is undone by withdrawal | EN-13, EN-6, EN-8, EN-9, A-12 | Only a cluster the resource manager created can be a target |
| **FC-23** | An enrollment endpoint where a cluster, or a person acting for it, presents its credential, and which authenticates the caller | EN-4, EN-6 | No route by which a cluster can present itself |
| **FC-24** | A declared rule, per target, for delivery with the platform's credential, and trust settings on targets that were taken in | S-16, W-2 | The stored credential is used only for intent with a verified signature, on a target that carries trust settings. Other intent is applied with the caller's token. |
| **FC-25** | Console pages for clusters of any origin, observed, enrolled, or created, and a targets page that reads targets | EN-11, EN-12, F-13, U-10 | The cluster list and the cluster page show only the built-in provider's clusters. The targets page is derived from deployments. |
| **FC-26** | Watching applied state: noticing a delivered resource that another writer changed, took over, or removed | W-16 | A delivery stays reported as active |
| **FC-27** | Deleting intent with a choice to leave its resources in place, and deletion that spares a resource another owner still claims | W-17, W-6, W-7 | Deleting a deployment removes what it applied, even where an OCM-io work covers the same resource |
| **FC-28** | Inventory that follows the life of a cluster: records removed or marked as past when a cluster leaves, and a selectable set of what is indexed per target | IS-6, IS-2, N-12 | Rows of a deleted cluster stay and are returned as current. Every target is indexed with the default setting: every kind apart from a short deny list. |
| **FC-29** | The resource manager's own agent on a cluster: a served connection, a per-cluster identity, local apply, and local indexing | EN-3, S-12, S-15, N-12 | The direct form only |
| **FC-30** | Readable outcomes: the reason for each failed delivery per target, a declared retry policy, and refusal of intent that names a target that does not exist | W-10, W-14, EN-12 | A failed delivery shows as still being created, with no reason, and is retried without limit |

## 4. Rejected solutions (RS)

| ID | Solution | Why it is rejected |
|---|---|---|
| **RS-1** | Serve the management API as a Kubernetes API on SQL storage, by aggregation or by an etcd-compatible adapter | It keeps the Kubernetes tenancy, identity, and query model and must reproduce Kubernetes API semantics on a database. See analysis.md section 3. |
| **RS-2** | Keep correlation and integration state as custom resources on the hub | It adds hub objects per cluster, which MO-1 exists to remove, and puts management state outside database recovery (R-2). |
| **RS-3** | Track correlation with labels or annotations on OCM-io objects | A GitOps applier treats injected metadata as drift (C-3, GO-3). |
| **RS-4** | Store a user's token with the intent so that delivery can run unattended | It creates the token-storage problem the design avoids (S-4). |
| **RS-5** | Require an agent of the resource manager on every managed cluster | The direct form needs none, and an agent is opt-in per cluster (EN-1, EN-3, C-6). |
| **RS-6** | Create `ManagedCluster` objects on a hub for clusters only the resource manager knows | It fabricates membership (I-6). |
| **RS-7** | Force an apply to win a conflict with another writer | It takes a field from its owner and starts a fight (GO-5, W-14). |
| **RS-8** | Make OCM-io's core multitenant | It is a different project (G-6). |
| **RS-9** | Migrate a whole hub in one step | Every step must be small and reversible (AD-4, G-11). |
| **RS-10** | Use an aggregator above several hubs as the management plane | It adds visibility without one management API, one tenancy model, or one history. See analysis.md section 3. |
| **RS-11** | Deliver the resource manager's intent by writing one `ManifestWork` per target to a hub | The hub would store an object per target for every intent, which is the cost MO-1 exists to remove, and would be in the path of every delivery. The management plane would not be doing its own work (W-8, AD-10, EN-10). |
| **RS-12** | Convert existing `ManifestWork` objects into resource manager intent automatically | A work belongs to its writer. A workload moves only when its owner moves it (W-11, AD-1). |
| **RS-13** | Mirror the resource manager's deliveries back to a hub as per-cluster objects, so that hub tools can list them | It brings back the hub cost of RS-11 and leaves a second copy that looks authoritative (GO-7, K-6, X-5). |
| **RS-14** | Run the integration as a controller or an added API inside the hub | Management would again live inside a Kubernetes cluster and depend on the hub being up (A-1, A-2, R-11). |
| **RS-15** | Reach a cluster by borrowing the klusterlet's identity or its hub credentials | That identity is OCM-io's. It cannot be scoped, reported, or withdrawn for the management plane alone (EN-2, EN-7, EN-9, T-4). |

## 5. Sequencing

| For | Resource manager capabilities | OCM-io enhancements that would help |
|---|---|---|
| The first increment: hubs observed, clusters enrolled in the direct form, delivery and inventory by the resource manager | FC-22, FC-23, FC-24, FC-25, FC-28, FC-30; FC-1 or the BH-5 workaround; FC-3, FC-12, FC-19 | None needed. E-7, E-8, and E-11 improve identity, freshness, and verification. |
| Both systems on one cluster in full: takeovers noticed, and workloads moved in both directions | FC-26, FC-27 | None |
| History, tenancy, and attribution | FC-4, FC-5, FC-6, FC-7, FC-17 | E-9 |
| Clusters that accept no inbound connection, and the larger scale profiles | FC-29, FC-14, FC-15 | None |
| Production and service-provider claims | FC-2, FC-9, FC-10, FC-16, FC-20, FC-21 | None |
| Driving from Kubernetes and from pipelines | FC-8, FC-13, FC-18 | E-6 |
| Serving OCM-io agents, and membership without a hub for clusters that keep only the klusterlet | FC-11 | E-1 to E-5, E-10 |
