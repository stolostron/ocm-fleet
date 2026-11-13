# Decisions

**Status: record.** Section 1 lists the directions this design takes as given. Section 2 lists what is still open. Section 3 records where requirements pull against each other and how the pull is resolved. A direction changes the design only through [requirements.md](requirements.md); this document points at the normative text and does not replace it.

## 1. Recorded directions (DR)

| ID | Direction | Normative text |
|---|---|---|
| **DR-1** | The core management API is a standalone server API. It is not a CRD or an aggregated APIService. Kubernetes may drive it. | A-1, section 1.1 |
| **DR-2** | The resource manager replaces the essential need for Kubernetes controllers and Kubernetes-stored state in fleet management, and stays compatible with their supplemental use. | A-2, AD-6, K-8, K-9 |
| **DR-3** | The introduction is gradual and beside OCM-io: three phases, enrollment per cluster, a move per workload, a cutover per aspect and scope, and a way back for each. | AD-1 to AD-4, Z-1 to Z-8, EN-1, EN-9, X-3 to X-6 |
| **DR-4** | A relational database is the primary store for management state. | A-4 |
| **DR-5** | PostgreSQL is the production database, including an instance the operator provides. | A-5 |
| **DR-6** | SQLite is the database for evaluation, demonstration, and continuous integration. | A-6 |
| **DR-7** | Changes to management state are kept as history. | IS-7 to IS-9, S-2 |
| **DR-8** | The design target is 10,000 clusters in one management plane. Service-provider scale is claimed only after validation at 6,400 or more. The design is service-provider grade first and degrades gracefully for small deployments. | N-1, AD-5 |
| **DR-9** | The management plane has its own tenancy model, with hard isolation, per-tenant quality of service, and explicit sharing through the provider. | T-1 to T-11 |
| **DR-10** | Every change is attributable to an end user or a workload identity, and a user's token is passed through without being stored. Access is fine-grained, and the management plane holds as little standing privilege as a delivery path allows. | S-2 to S-5, S-14, S-15 |
| **DR-11** | The integration's own state lives in the management datastore, not in hub objects. | I-1, N-2, R-2 |
| **DR-12** | One management plane can connect to several hubs. | M-2, I-2 |
| **DR-13** | GitOps is an authority, and the integration is never a second writer. Provenance is a property of a delivery, not of an observed fact. | GO-3, GO-5, S-6 to S-8 |
| **DR-14** | Registration is OCM-io's while membership is in native mode. Enrollment is not registration. Membership moves only through an explicit cutover, and is never fabricated. | M-6, M-7, I-6 |
| **DR-15** | Existing agents stay as they are and keep serving their hub. Enrolling a cluster changes no agent. Serving OCM-io agents from the management plane is optional, and where it is done it uses OCM-io's published protocols. | K-2, K-3, C-6 |
| **DR-16** | No requirement depends on an OCM-io change. Upstream enhancements are optional and must stand on their own. Missing resource manager capabilities are tracked as gaps. | C-7, AD-9 |
| **DR-17** | The first increment brings a first existing fleet under the management plane: hubs are observed, the clusters that opt in are enrolled in the direct form, and the resource manager delivers to them and indexes them itself. | AD-7, Z-8. It is described in topologies.md section 9. |
| **DR-18** | The resource manager does its own work. No `ManifestWork`, and no other hub object, is created to deliver or to record its intent. | W-8, AD-10, GO-7 |
| **DR-19** | A cluster that already exists comes under the management plane by enrollment: opt-in per cluster, a small declared access piece, the cluster still registered with its hub, and a withdrawal that undoes it. | EN-1, EN-2, EN-9, F-2 |
| **DR-20** | There are two access forms. The direct form comes first. The resource manager's own agent serves clusters that accept no inbound connection, keeps the credential on the cluster, and is what the larger scale profiles rest on. | EN-3, S-15, N-12 |
| **DR-21** | OCM-io's own add-on mechanism is one route for rolling enrollment out, chosen by the hub's administrator. A route that uses no hub object always exists. | EN-4, EN-5, N-2 |
| **DR-22** | Both systems may deliver to one cluster. Each workload is held by one of them and moves only when its owner moves it. | W-15, X-2, W-7, W-11 |
| **DR-23** | The integration is an extension of the management plane, and taking in a cluster that already exists is a capability of the management plane itself. An enrolled cluster is a cluster like any other in the API, the console, and the command line. | A-12, EN-13, EN-11 |

Why these directions, what was considered instead, and what they cost are in [analysis.md](analysis.md) sections 2 to 5.

## 2. Open questions (OQ)

| ID | Question | What is fixed whatever the answer |
|---|---|---|
| **OQ-1** | Which datastore high-availability arrangements does the first production release validate? The candidates are in topologies.md section 3. The choice is deliberately deferred. | R-1 to R-7, A-5 |
| **OQ-2** | Where the management plane serves OCM-io agents, is that endpoint part of the resource manager, or a separate process beside it? | K-3, S-12 |
| **OQ-3** | Which kind is offered in driven mode first: `ManifestWorkReplicaSet` with `Placement`, or a kind the integration defines? | K-5, GO-2, GO-4, W-12 |
| **OQ-4** | How much of an enrolled cluster is indexed by default in the direct form, and is a hub indexed at all? | IS-2, N-12 |
| **OQ-5** | What are the default retention periods, and is observation history kept in the same database as management state? | IS-8, IS-9 |
| **OQ-6** | What does a connected hub map to by default once tenancy is enforced: a tenant, a workspace, or nothing? | T-3, T-8 |
| **OQ-7** | Where is the point of no return in a registration cutover? | X-6, M-7 |
| **OQ-8** | After a restore, how does an operator review and decide each divergence at fleet scale? | R-4 |
| **OQ-9** | How are 6,400 and 10,000 clusters validated: with real clusters, simulated agents, or both? | N-1, V-5 |
| **OQ-10** | What naming scheme keeps the targets of enrolled clusters collision-free, and should the resource manager scope name uniqueness to a workspace? | I-2, I-7 |
| **OQ-11** | When a workload is moved, what proves that ownership has transferred before the hub's work is retired: the field ownership recorded on each resource, a comparison of content, or both? | W-7, W-11, X-4 |
| **OQ-12** | Is the enrollment add-on kept enabled for a cluster after it is enrolled, so that a lapsed credential repairs itself at the price of hub objects, or disabled, so that the hub holds nothing? | EN-5, EN-8, N-2 |
| **OQ-13** | Which rights does the generated access piece ask for by default? | EN-7, S-15 |
| **OQ-14** | Under which rule does an unattended delivery use the credential issued at enrollment: only verified signed intent, or also an explicit grant? | S-16, S-3 |
| **OQ-15** | How does a cluster that enrolls prove which cluster it is: a one-time secret issued for it, the hub's record of it, or both? | EN-6, I-3 |
| **OQ-16** | At what number of enrolled clusters does the direct form stop being supportable for one management plane? | N-12, EN-3, N-1 |

OQ-13, OQ-14, and OQ-15 have to be answered inside the first increment (DR-17). Each has a conservative starting point: the narrowest role that still lets a workload be delivered, signed intent only, and both proofs. No other open question blocks it.

## 3. Tensions and how they are resolved

| Tension | Resolution |
|---|---|
| Compatibility costs hub objects, and scale wants none. | Nothing is written to a hub for a delivery. An enrollment route with no hub object always exists, and the cost of the others is bounded, counted, and can be ended once a cluster is enrolled (W-8, EN-5, K-12, N-2). |
| Existing agents are not touched, yet the resource manager has to reach the cluster. | Enrollment adds its own small access piece next to the klusterlet, opt-in per cluster and removable. It does not borrow the klusterlet or its credentials (EN-2, K-2, C-6). |
| Two systems deliver to one cluster, yet there is one system of record. | Each workload is held by exactly one system. Nothing is forced, each conflict is reported, and a move is an explicit operation (X-2, W-14, W-15, W-7). |
| Reaching a cluster directly is simple, yet it needs a route in and a credential held centrally. | The direct form is the first access form, with rights the cluster's side chooses and reports of what is held. The agent form removes both needs (EN-3, EN-7, S-15). |
| One system of record, yet Kubernetes objects drive intent. | A driving object is desired state that its writer owns. The system of record for orchestration, per-target records, and status is the datastore (GO-3, GO-7, X-2). |
| No stored user tokens, yet deliveries run unattended. | A delivery pauses when its credential is gone. Unattended work uses a workload identity, or signed intent applied with platform credentials (S-4, S-5, GO-10). |
| A standalone core, yet a Kubernetes ecosystem. | Supplemental controllers and add-ons keep working wherever a hub API remains, and the Kubernetes-facing surface is optional (AD-6, K-8, GO-1). |
| etcd fails over in seconds, a database in tens of seconds. | Clusters keep running while the management plane is unavailable, so the difference is a pause in management, not in workloads (R-5). |
| Gradual steps, yet no dead end. | Each phase is complete by itself and leads to the next without enrolling clusters again (AD-7, I-10). |
| Consolidating hubs, yet keeping tenants apart. | Separation that hubs provided is preserved unless an operator merges it (T-8). |
| Using OCM-io's seams, yet requiring no OCM-io change. | Only published protocols, public APIs, and OCM-io's own extension points are used, and every enhancement is optional (K-3, C-1, C-7). |
| History of everything, yet bounded storage. | History of change is complete. History of observation is bounded by declared retention (IS-7, IS-8, N-5). |
