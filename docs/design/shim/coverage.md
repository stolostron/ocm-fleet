# Coverage

**Status: ledger.** This document maps what [requirements.md](requirements.md) asks for to what exists, and names every gap. It does not decide whether a requirement is satisfied; verification does that (V-1). Statements about what exists rest on the sources in [analysis.md](analysis.md) section 9.

## 1. How to read it

Each requirement or story is in one of three states.

- **Exists**: unmodified OCM-io v1.4.x or the current resource manager already behaves this way.
- **Hypothesis**: a candidate mechanism in [baseline-hypotheses.md](baseline-hypotheses.md) would provide it. Nothing is built.
- **Gap**: it is not available. Section 4 gives each gap an identifier, what would close it, and what holds until then.

The integration itself does not exist (GAP-1). Every "hypothesis" below is therefore also unbuilt.

## 2. What each phase delivers for each motivation

| Motivation | Alongside | Leading | Standalone |
|---|---|---|---|
| **MO-1** Scale | Nothing is added to a hub. Many hubs appear in one datastore. The hubs themselves are unchanged. | A workload the resource manager delivers adds nothing to a hub, and its per-cluster records are rows in the datastore. What OCM-io still delivers stays on its hub until it is moved. | There is no hub to grow. |
| **MO-2** Multitenancy | Hub connections can be mapped to tenants. Visibility is provider-only until isolation is enforced. | Tenants share one plane for intent and delivery to enrolled clusters. | The same, with no hub boundary left. |
| **MO-3** Security and attribution | The management plane's own actions and the changes it observes are recorded. The actor of a change made on a hub is not available. | Every change made through the management API is attributed. The rights held on each enrolled cluster are the ones its administrator granted, and are reported. Token passthrough is available where a cluster trusts the identity provider. | Every change is attributed. |
| **MO-4** Inventory, search, and history | Clusters and hub-held facts across all hubs, with a history of observed change. | Resources on enrolled clusters, deliveries, and questions that join them. | The same. |
| **MO-5** Reliability and recovery | The management plane's state is recovered by database procedures. Hub state is still recovered with the hub. | Enrollments, and everything the resource manager delivers, are recovered by database procedures. Delivery to an enrolled cluster does not depend on its hub being up. | Everything is. |
| **MO-6** Independence from Kubernetes | The management plane runs without Kubernetes. Managing clusters still goes through hubs. | Enrolled clusters are managed with no hub in the path. Hubs are needed for what OCM-io still does, and for an optional Kubernetes-facing surface. | None is required. |

## 3. Requirement coverage by family

| Family | Exists today | Hypotheses | Gaps |
|---|---|---|---|
| **A** Architecture and datastore | A-1 to A-4, A-6, A-8, A-10: a standalone API, no Kubernetes needed, a relational store, SQLite, durable orchestration. A-5: PostgreSQL through a connection URL. | BH-19, BH-31 | A-9: GAP-10. A-7: backend differences are not reported. A-11: extension APIs exist without the guarantees (GAP-5, GAP-8, GAP-31). A-12: GAP-27, GAP-32. |
| **K** Compatibility | K-2, K-4, K-7 to K-10 hold today, because nothing touches OCM-io or its agents. | BH-20, BH-21, BH-23, BH-25 | K-3: GAP-12. K-5, K-6: GAP-15, GAP-16. K-8 after a source change: GAP-14. K-1, K-11, K-12: GAP-1. |
| **GO** Kubernetes and GitOps | GO-1: the resource manager works with no Kubernetes-facing surface. GO-3, GO-5, GO-6, GO-12 hold today, because nothing writes. | BH-20, BH-21, BH-22 | GO-2, GO-4, GO-7, GO-8: GAP-15. GO-9: GAP-9. GO-10: GAP-8. GO-11: GAP-1. |
| **X** Authority and cutover | X-1, X-2, X-7 to X-10 hold by default, because nothing is mapped or written. | BH-17, BH-24, BH-25 | X-3 to X-6: no move and no cutover exists as an operation (GAP-1, GAP-36, GAP-13). |
| **I** Identity | None. | BH-2, BH-4 | I-7: GAP-3. I-2, I-4: GAP-23. I-3 where a hub holds no address for a cluster: GAP-40. |
| **M** Membership and inventory | M-6 holds. M-8 for clusters the resource manager creates. | BH-1, BH-3, BH-4, BH-5, BH-14, BH-25 | M-1, M-2, M-5, M-9: GAP-1. M-4, M-10: GAP-2. M-3: GAP-28. M-7: GAP-1, GAP-12. M-8 for clusters that already exist: GAP-32. |
| **EN** Enrollment and access | None as a capability. Its parts were observed by hand (section 6). | BH-5 to BH-13 | EN-1, EN-2, EN-4, EN-6, EN-8 to EN-10, EN-13, EN-14: GAP-32. EN-3: GAP-38. EN-5: GAP-1. EN-6 where a hub holds no record: GAP-40. EN-7: GAP-29, GAP-39. EN-9 and EN-14 for inventory: GAP-37. EN-11: GAP-34. EN-12: GAP-34, GAP-39. |
| **W** Work delivery | The direct path, for clusters the resource manager created. W-3: deliveries carry a generation. W-8: a delivery creates no hub object. W-14: applies are not forced. | BH-12, BH-13, BH-16 to BH-18, BH-23, BH-24 | W-2 for enrolled clusters: GAP-33. W-4: GAP-12. W-6, W-17: GAP-36. W-7: GAP-36, GAP-13. W-10: GAP-39. W-12: GAP-17. W-14 retries: GAP-24. W-16: GAP-35. W-9, W-13, W-15: GAP-1. |
| **T** Tenancy and quality of service | None. | BH-26, BH-27 | T-1 to T-4, T-6 to T-9, T-11: GAP-4, GAP-5. T-5, T-10: GAP-6. |
| **S** Security and provenance | S-1: OIDC. S-3: the caller's token or platform credentials. S-5: pause and resume. S-7: signed intent is bound to content. | BH-6, BH-12, BH-22, BH-28 | S-2: GAP-8, GAP-20. S-4: GAP-7. S-6: GAP-21. S-12: GAP-38, GAP-12. S-14: GAP-4. S-15: GAP-29. S-16: GAP-33. S-9 to S-11, S-13: GAP-1. |
| **IS** Inventory, search, and history | IS-6: inventory is in the database. IS-2, IS-3 for clusters the resource manager created. | BH-3, BH-15, BH-29 | IS-1, IS-3, IS-4: GAP-18. IS-2 for clusters that already exist: GAP-19. IS-2 selection, and IS-6 for a cluster that has left: GAP-37. IS-5: GAP-5. IS-7 to IS-10: GAP-8, GAP-20. |
| **R** Reliability and recovery | R-5: clusters keep running in both systems. R-9, R-10 for the resource manager's own orchestration. R-13 holds, because no integration exists. | BH-2, BH-30 | R-1 to R-4, R-7: GAP-11. R-6: GAP-10, GAP-26. R-8, R-11, R-12: GAP-1. |
| **N** Scale | None validated. Observed by hand: a delivery by the resource manager adds no hub object and no custom resource on the cluster (N-2, N-11). | BH-7, BH-31 | N-1, N-3, N-6, N-12: GAP-22. N-12 in the agent form: GAP-38. Orchestration history: GAP-30. |
| **U**, **O**, **V** | None. | All of them | GAP-1. U-9: GAP-25. U-10: GAP-34. |
| **AD**, **F**, **Z**, **C**, **G** | Principles, outcomes, scope rules, constraints, and non-goals. They are met or broken by the rest. | | |

## 4. Declared gaps (GAP)

| ID | Gap | Affects | Closed by | Until then |
|---|---|---|---|---|
| **GAP-1** | The integration does not exist. Every mechanism is a hypothesis. | Everything | Implementation, with FC-3 for the integration's own records | This directory is design only. |
| **GAP-2** | The resource manager has no target lifecycle API. A target cannot be created, relabeled, or removed through the API. | M-4, M-10 | FC-1 | The BH-5 workaround: an enrollment is a resource whose delivery reports the target, and a label changes only when that delivery runs again. |
| **GAP-3** | Target names are unique across the installation. | I-2, I-7 | FC-17 | A generated name embeds the hub connection. |
| **GAP-4** | The resource manager does not authorize. Every authenticated caller can read and act on everything. | T-2, T-4, T-10, S-14 | FC-4 | Every caller is in effect the provider. |
| **GAP-5** | Tenants and workspaces are not implemented. | T-1 to T-9, T-11, IS-5, F-6 | FC-4 | Visibility is provider-only and is reported that way (T-7). |
| **GAP-6** | There are no per-tenant limits. | T-5, T-10 | FC-5 | One caller can exhaust the plane. |
| **GAP-7** | The caller's token is persisted with the fulfillment. | S-4 | FC-6 | Token passthrough cannot be described as storing no token. Signed intent with platform credentials avoids the token. |
| **GAP-8** | There is no history of change or of observation. | IS-7 to IS-10, S-2, F-7, F-8 | FC-7 | Current state only. Attribution is the caller of the current fulfillment. |
| **GAP-9** | Intent has no update and no declarative apply. | GO-9, GO-2 | FC-8 | Delete and re-create. |
| **GAP-10** | Only one replica is exercised. | A-9, R-6 | FC-9 | Single-instance topologies only. |
| **GAP-11** | There is no backup, restore, point-in-time, or failover procedure, and no reconciliation after a restore. | R-1 to R-4, R-7, U-7 | FC-10 | The database's own tools, with no tested procedure. No recovery objective can be claimed. |
| **GAP-12** | The management plane does not serve OCM-io agents. | K-3, W-4, M-7 | FC-11; E-1 and E-2 help | The work-agent path is not offered. A klusterlet follows its hub. |
| **GAP-13** | Changing a cluster's work source without disturbing workloads is proven only for a change of hub address. It is unproven with the management plane as the source. | W-7, X-6 | Live validation of BH-24 with the management plane as the source; E-5 and E-10 help | No source change is offered. |
| **GAP-14** | A klusterlet follows one work source, and the operator ties that source to the registration driver. | K-8, Z-4 | E-3, E-4 | A cluster with hub-delivered add-on work stays on its hub. |
| **GAP-15** | There is no Kubernetes-facing surface. | GO-2, GO-4, GO-7, GO-8, K-5, K-6 | FC-18 | Native mode only. |
| **GAP-16** | A kind can be driven only where OCM-io's own controller for it does not run. | K-5 | E-6 | Driven mode is limited to surfaces where that controller does not run. For `ManifestWorkReplicaSet` that is a v1.4.0 hub by default; OCM-io has since turned the controller on by default. |
| **GAP-17** | The resource manager selects by exact label only and rolls out immediately only. | M-4, W-12 | FC-13 | Losses are declared. Selection can stay with OCM-io (BH-14, BH-20). |
| **GAP-18** | Targets, inventory items, and deliveries have no read API, and no query joins them. | IS-1, IS-3, IS-4 | FC-12 | They can be read only in the database. |
| **GAP-19** | Resource inventory reaches only the clusters the resource manager created, because only they have a target and a credential. | IS-2 | FC-22; FC-29 for a cluster that accepts no inbound connection | Hub-held facts only, for every other cluster. |
| **GAP-20** | The actor of a change made on a hub is not available to the management plane. | IS-10, S-2 | E-9 | The record says no actor is available on that path. |
| **GAP-21** | Evidence is verified on the platform side only, and not at all where OCM-io's work agent applies. | S-6 | FC-14 | Each path reports its verification locus. |
| **GAP-22** | No scale profile is validated in any access form, and no capacity model is published. | N-1, N-3, N-6, N-12 | FC-15, OQ-9, OQ-16 | No scale claim is made. |
| **GAP-23** | OCM-io publishes no stable cluster identity by default. | I-2, I-4 | E-7 | Name and incarnation markers. A name match is a heuristic. |
| **GAP-24** | A delivery that fails with a retryable error is retried after a fixed delay, with no cap. | W-14 | FC-30 | Declared. The integration adds no retry of its own. |
| **GAP-25** | State cannot be moved between backends. | U-9 | FC-16 | An evaluation cannot be carried into production. |
| **GAP-26** | No datastore high-availability arrangement is selected. | R-6, A-5 | OQ-1 | The topology is the operator's choice, unvalidated. |
| **GAP-27** | The integration cannot be shipped or upgraded separately from the resource manager. Server add-ons and console plugins are built in. | A-12, U-2, K-11 | FC-2 | It is built into the resource manager and its console and released with them. |
| **GAP-28** | Several OCM-io kinds carry no status-level observed generation. | M-3, IS-1 | E-8 | The management plane records when it read each value. |
| **GAP-29** | The management plane holds standing administrator credentials and does not report them. For each cluster it creates, the resource manager stores the token of a platform service account with administrator rights. OCM-io's work agent applies with its own rights whoever asked. | S-15, EN-7 | FC-19 | Each path declares the identity that applies at the target (W-2). |
| **GAP-30** | The workflow engine never removes the history of a completed orchestration run. In the test environment that history is two thirds of the datastore. | N-3, IS-9 | FC-20 | Datastore growth includes about 37 KB for every orchestration run. |
| **GAP-31** | The management API is at an alpha version and carries no compatibility commitment. | A-11 | FC-21 | Extensions and clients build against an API that can change. |
| **GAP-32** | The resource manager cannot take in a cluster it did not create. Nothing accepts an address, a trust anchor, and a credential for an existing cluster, verifies them, renews the credential, or lets a cluster present itself. | EN-1, EN-2, EN-4, EN-6, EN-8 to EN-10, EN-13, EN-14, M-8, A-12 | FC-22, FC-23 | Only clusters the resource manager creates are targets. Enrollment is not offered. |
| **GAP-33** | The stored credential of a target is used only for intent that carries a verified signature, and only on a target that carries trust settings. Other intent is applied with the caller's token, which a cluster that already exists refuses unless its API server trusts the management plane's identity provider. | S-16, W-2, S-3 | FC-24, OQ-14 | Delivery to an enrolled cluster needs signed intent and trust settings on its target. |
| **GAP-34** | The console lists and shows as clusters only those of the built-in provider, and derives its targets page from deployments. | EN-11, EN-12, F-13, U-10 | FC-25 | An observed or enrolled cluster is found through search only. |
| **GAP-35** | The resource manager does not notice when a resource it delivered is changed, taken over, or removed by another writer. | W-16 | FC-26 | A deployment stays reported as active. |
| **GAP-36** | Deleting intent always removes what it applied, including a resource that an OCM-io work also covers. No delete leaves resources in place. | W-17, W-6, W-7 | FC-27 | A workload can be moved to the resource manager without interruption, and not back. |
| **GAP-37** | Inventory rows of a cluster that has left are kept and returned as current, and what is indexed cannot be chosen per cluster: every target is given the indexer's default setting. | IS-6, IS-2, EN-9, EN-14 | FC-28 | A reader has to recognize stale rows. A cluster, or a hub, is indexed whole. |
| **GAP-38** | The resource manager's own agent is not built. Its service is defined and not served. | EN-3, S-12, N-12 | FC-29 | The direct form only. A cluster that accepts no inbound connection is observed and cannot be enrolled. |
| **GAP-39** | The reason for a failed delivery cannot be read through the API, and intent that names a target that does not exist is accepted and waits. | W-10, EN-7, EN-12 | FC-30 | The reason is in the resource manager's log and database. |
| **GAP-40** | A hub records a cluster's API address and certificate authority only where the klusterlet was configured with them. | EN-6, I-3 | E-11 | Where the record is absent, an enrollment is verified against the cluster itself and by a one-time secret. |

A gap limits what may be claimed. It is reported at run time wherever it applies (AD-8).

## 5. Use-case coverage

Stories are in [use-cases.md](use-cases.md). The entries UC-N1 to UC-N20 in its section 13 are guards, not stories. Each names the requirement that enforces it, and that requirement's coverage is in section 3.

| State | Stories |
|---|---|
| The behavior exists today, in OCM-io or the resource manager as they are | UC-S5, UC-R5, UC-E1, UC-E6, UC-I5, UC-C2, UC-C4, UC-G1, UC-G6 |
| Need only the observing part of the integration (BH-1 to BH-4) | UC-S4, UC-R8, UC-E2, UC-C6, UC-C8, UC-C9, UC-C10, UC-M1, UC-M5, UC-M6 |
| Need the observing part and a resource manager capability | UC-O1 (GAP-34), UC-C7 (GAP-3), UC-I1 (GAP-18), UC-T4 (GAP-5) |
| Need enrollment (BH-5 to BH-15), which rests on resource manager capabilities | UC-O2, UC-O3, UC-O4, UC-O13, UC-C1 (GAP-32), UC-O5 (GAP-32, GAP-33, GAP-39), UC-S3 (GAP-32, GAP-39), UC-O6 and UC-O11 (GAP-32, GAP-37), UC-O7 (GAP-32, GAP-34), UC-O8 (GAP-32, GAP-29), UC-I2 (GAP-19), UC-G7 (GAP-2) |
| Need a move in both directions (BH-17) | UC-O10, UC-M2 (GAP-36) |
| Need the resource manager's own agent (BH-13) | UC-O12, UC-A5 (GAP-38) |
| Blocked by another resource manager gap | UC-S1 and UC-S2 (GAP-22), UC-R1, UC-R3, UC-R6 (GAP-11), UC-R2 (GAP-10, GAP-26), UC-E5 (GAP-5, GAP-8, GAP-31), UC-E7 (GAP-25), UC-T1, UC-T2 (GAP-4, GAP-5), UC-T3 (GAP-6), UC-T5, UC-I4 (GAP-8), UC-T6 (GAP-5), UC-A1 (GAP-8, GAP-20), UC-A2 (GAP-7), UC-A4 (GAP-21), UC-A6 (GAP-4), UC-A7 (GAP-29), UC-I3 (GAP-18), UC-G4 (GAP-9) |
| Need an optional part: a Kubernetes-facing surface, or service to OCM-io agents (BH-20 to BH-25) | UC-G2 (GAP-15, GAP-16), UC-C3, UC-M3 (GAP-12, GAP-13) |
| Partly available | UC-R4 and UC-R7 (separate servers today; no declared failover or skew), UC-E3 (creating a first cluster works; enrolling one is GAP-32), UC-E4 (OCM-io add-ons work; the rest is a proof of concept), UC-A3 (signed intent is verified; workload identity is designed), UC-O9 (neither system forces, and each conflict is refused; a takeover is not noticed, GAP-35), UC-G8 (the resource manager never forces; enrolling the cluster is GAP-32), UC-C5 and UC-G3 (on a hub this is OCM-io as it is; projection is GAP-15), UC-G5 (a revert reconciles; its history record is GAP-8), UC-M4 (needs every earlier step) |

## 6. Verification coverage

No verification in section 19 of the requirements has been run as a test suite. The checks below were made by hand while writing this design and are recorded in analysis.md section 9. They are observations, not release evidence.

| Requirement | What was observed |
|---|---|
| V-11 | The resource manager runs in one container with an embedded database and no Kubernetes. |
| V-5, in part | Hub objects and etcd growth per registered cluster and per work, at 200 placeholder clusters. A deployment through the resource manager created no object on the hub and no custom resource on the cluster. |
| V-9, in part | The caller's token is present in the stored fulfillment. This is a failure of S-4 and is recorded as GAP-7. |
| V-8, in part | Two users see the same records. This is a failure of T-2 and is recorded as GAP-4. |
| V-3, in part | The hub returned to its original objects after each measurement. Through every check below, the klusterlet on each cluster was left as it was, and the hub went on reporting the clusters as available. |
| V-7, in part | On an unmodified klusterlet, a real change of the hub's address, in both directions, left applied resources with the same identity and the same running pod. With constructed records, a resource the new source does not deliver was deleted. One registration left unapproved for ten minutes made the hub report the cluster unreachable until it was approved. One workload was moved from OCM-io to the resource manager with its resource keeping its identity. Moved back, the resource was deleted and restored four and a half minutes later (GAP-36). |
| V-14, in part | An OCM-io add-on, applied to an unmodified v1.4.0 hub and enabled for two clusters, put a service account and its binding on each and handed a token to a stand-in receiver within 12 seconds. The hub held two objects in total and two for each cluster. After the add-on was disabled for a cluster, the hub held nothing for it, the access piece remained, and the token still worked. After the access piece was removed, the token was refused at once. The hub's record of address and certificate authority matched all three clusters. No step was performed by the resource manager (GAP-32). |
| V-15, in part | One cluster received deliveries from OCM-io and from the resource manager. Resources that only one of them delivered were unaffected by the other. The resource manager's apply onto an OCM-io resource was refused. An OCM-io work with server-side apply and no force onto a resource of the resource manager failed and named it. One with the default strategy overwrote it, and the resource manager did not notice (GAP-35). No hub object existed for anything the resource manager delivered. |
| EN-11, in part | The console lists the two clusters of the built-in provider. A cluster the hub knows and the resource manager did not create appears only in search, as the hub's `ManagedCluster` object (GAP-34). |
| IS-6, in part | More than half of the indexed rows belong to clusters that no longer exist (GAP-37). |
| N-3, in part | Bytes per target, delivery record, indexed resource, and orchestration run in the SQLite store of the test environment. |

## 7. Keeping this ledger

- A change to a requirement, a hypothesis, or a gap is not complete until this document reflects it.
- A gap closes only with evidence: a test, or an observation recorded in analysis.md section 9.
- A capability moves from "hypothesis" to "exists" only when it is observed on real systems (V-2).
