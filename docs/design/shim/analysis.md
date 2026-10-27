# Analysis

**Status: rationale, not normative.** This document explains why the decision in [requirements.md](requirements.md) section 1.1 was taken, what was considered instead, what it costs, and what both systems do today. Where it disagrees with the requirements, the requirements win. Every statement about current behavior is listed with its source in section 9.

## 1. Two architectures at a glance

| | OCM-io v1.4.x | OCM Fleet resource manager today |
|---|---|---|
| Management API | The hub's Kubernetes API, extended with CRDs | A standalone gRPC and HTTP API |
| Store | The hub's etcd | A relational database: SQLite or PostgreSQL |
| Unit of intent | `ManifestWork`, one per cluster. `ManifestWorkReplicaSet` fans out by creating one `ManifestWork` per selected cluster. | A fulfillment: manifests, placement, and rollout in one record, with one delivery row per target |
| Orchestration | Controllers reconciling objects | Durable workflows persisted in the same database |
| Delivery | A work agent on each cluster reads its work and applies it | A delivery agent on the platform side applies to the cluster's API. An agent of its own on the cluster is designed. |
| What a delivery leaves behind | One `ManifestWork` on the hub and one `AppliedManifestWork` on the cluster, for each work | Rows in its database. No object on a hub and no custom resource on the cluster. |
| Membership | `ManagedCluster`, created by a registration agent over CSR or gRPC | Targets, created by an add-on's declaration or as the output of a delivery. There is no target API. |
| A cluster that already exists | Joins when a klusterlet on it registers | Cannot be taken in. Only clusters created through the built-in provider are targets. |
| Selection | `Placement`: label and claim selectors, expressions, taints and tolerations, prioritizers, producing `PlacementDecision` | A placement strategy: an explicit list, all targets, or an exact label match |
| Tenancy | Namespaces, `ManagedClusterSet`, and RBAC inside one hub, or separate hubs | A provider, tenant, and workspace model that is designed and not implemented |
| Who the target sees | The work agent's service account, or an executor service account named by the work | The caller's own token, or platform credentials after signed intent is verified |
| Search and history | None | A filtered query over indexed resources. No history. |
| Recovery | With the hub cluster and its etcd | Database tools. Nothing is packaged. |
| Runs without Kubernetes | No | Yes |

## 2. Why: the six motivations

A service provider that runs fleet management on a Kubernetes API usually wraps it in an API of its own, because it needs tenancy, attribution, and an endpoint it can expose to the internet. The six motivations are what that wrapper has to provide. This design makes the wrapper the management API.

### 2.1 Scale (MO-1)

**What a hub stores.** On an OCM-io v1.4.0 hub, a registered cluster with no add-on and no work costs nine objects: the `ManagedCluster`, its namespace, a `ClusterRole`, a `ClusterRoleBinding`, two `RoleBinding`s, a `Lease`, and the `ConfigMap` and `ServiceAccount` that Kubernetes places in every namespace. A test that registered 200 placeholder clusters grew etcd by 1,800 keys and about 13.7 KB per cluster.

Every `ManifestWork` is one more object. In the same test a work carrying two small manifests took about 2.0 KB in etcd before any status. A real work with the same payload, applied by a real agent, was 5.3 KB as stored JSON, of which the payload was 0.8 KB; status and metadata were the rest.

Membership is therefore cheap, and per-cluster work is what fills a hub. The table multiplies the measured base by two illustrative loads. The work sizes in the second row are an assumption, not a measurement.

| Load per cluster | Hub objects per cluster | 2,000 clusters | 6,400 clusters | 10,000 clusters |
|---|---|---|---|---|
| 10 works of 5.3 KB (the size measured for a small work) | 19 | 38,000 objects, 0.13 GB | 121,600 objects, 0.43 GB | 190,000 objects, 0.67 GB |
| 30 works of 20 KB (assumed) | 39 | 78,000 objects, 1.2 GB | 249,600 objects, 3.9 GB | 390,000 objects, 6.1 GB |

These figures count current objects only. They exclude add-on objects, events, the objects of the hub cluster itself, and the revision history etcd keeps until compaction.

**The ceiling is documented.** etcd's documentation sets the default storage quota at 2 GiB and states that "8 GiB is a suggested maximum size for normal environments". It describes etcd as "designed to hold small amounts of data that can fit entirely in memory", asks for at least as much memory as the quota, and limits a request to 1.5 MiB by default. A larger store also takes longer to snapshot, which lengthens recovery. All writes go through one leader, and a larger cluster does not add write capacity: "the write performance suffers because data must be replicated across more machines".

**Kubernetes publishes scale criteria for a different shape.** Its guidance for large clusters is stated in a cluster's own workload: no more than 5,000 nodes, 150,000 pods, and 300,000 containers. A hub is asked to hold something else: thousands of namespaces with tens of objects each, written by thousands of agents. The management state this design targets is also larger. The second row of the table is 390,000 objects at 10,000 clusters before any inventory. Resource inventory is larger again: the two small clusters of the test environment account for about 840 indexed items between them, a density at which 10,000 clusters would hold several million.

**Kubernetes says what a custom resource is for.** Its documentation advises: "Avoid using a Custom Resource as data storage for application, end user, or monitoring data". It lists signs that an API should stand alone, including "> a few kB per object, or > 1000s of objects" and "High bandwidth access (10s of requests per second sustained)". Per-cluster delivery records and status for thousands of clusters have that shape. The same page warns that an aggregated API server that shares the main storage inherits the same limit.

**OCM-io has named the problem.** Its event-based `ManifestWork` proposal states that "as manifestwork adoption and managed cluster numbers increase, we encounter scaling issues due to excessive manifestwork creation on the hub", and anticipates sources that are "non-Kubernetes restful services with databases". Its gRPC registration proposal notes that keeping the API server as the persistence layer "will not bring benefit in terms of scale" and lists "use a non-kube controlplane to store data" as a future possibility. Its provisional proposal 205 investigated two SQL-backed arrangements and measured neither.

**What users do today** is split a fleet across hubs. OCM-io's documentation says a hub can "accept and manage thousands of clusters" and publishes no validated limit, so an operator finds a hub's limit by measurement. Tests of a single hub have been reported at roughly 3,700 managed clusters, a figure this design did not reproduce. Splitting keeps each hub inside its limits and removes the single management API, which leads to the next motivation.

**What the resource manager changes.** Per-cluster records are rows in a database whose capacity is planned on its own terms, and a delivery leaves nothing on a hub. On the test environment a deployment of two objects through the resource manager created no hub object and no custom resource on the cluster. The same content as a `ManifestWork` is one object on the hub and one applied-work record on the cluster.

In the test environment's SQLite store a target with its inventory item is about 2.4 KB, a delivery record about 0.5 KB, and an indexed resource about 1.6 KB. The largest consumer is not a record. The workflow engine keeps about 37 KB for each orchestration run and never removes a completed run, and that history is two thirds of the 5.2 MB database. A capacity model therefore has to include orchestration history and its retention. These figures come from three targets and are indicative only.

The resource manager has not been validated at fleet scale: it has an orchestration stress test, and its own design assumptions stop at 2,000 targets. In the form that exists, it also holds every watch on every cluster it indexes inside its own process, and its design notes that the write path of its datastore bounds how large a cluster that form can sustain. That is why scale is stated per access form (N-12).

### 2.2 Multitenancy (MO-2)

In OCM-io, `ManagedCluster` and `ManagedClusterSet` are cluster-scoped kinds. Access is shaped with RBAC, with a namespace per cluster, and with set bindings. Kubernetes adds object-count quotas and request priority and fairness. What a hub cannot offer is a tenant hierarchy over its cluster-scoped kinds, or isolation of one tenant's storage and watch load from another's: everything shares one API server and one etcd. The strong boundary is a separate hub per tenant, and then no management plane spans the tenants.

Several projects have layered tenancy onto the Kubernetes API, for example with hierarchical namespaces or with separate logical control planes. None of them is part of Kubernetes itself. Expressing tenants in a resource hierarchy and RBAC is also not sufficient: multitenancy needs quality-of-service safeguards through the whole API implementation, which is why providers build their own.

A standalone API can have its own resource hierarchy and enforce isolation and limits through every layer. The resource manager's design has a provider, tenants, and a workspace tree in which each target belongs to one workspace. PostgreSQL's row-level security can back that isolation in the datastore. None of it is implemented: the schema has no tenant or workspace, and both demonstration users see the same records.

### 2.3 Security and attribution (MO-3)

Kubernetes controllers act with their own identities, independently of whoever wrote the desired state. That is a virtue for reliability and a problem for attribution: an investigation that asks who changed a cluster ends at a service account.

On the test cluster, a `Deployment` applied through a `ManifestWork` records two field managers, `work-agent` and `kube-controller-manager`. The `ManifestWork` on the hub records `kubectl-client-side-apply` and `work`. Neither object names a user. The only record of the user is the hub's API audit log, where one is enabled, and that is outside OCM-io's API. A work can name an executor service account for the apply, which is still a workload identity and not the end user.

Storing the user's token with the object would let the target see the user, at the price of storing user tokens. The resource manager's design avoids that. A delivery either presents the caller's token to the target or applies with platform credentials after verifying intent the user signed. When a credential is missing or expired, the delivery pauses until an authorized user resumes it. The design keeps tokens in memory only. The current prototype does not: it persists the caller's token with the fulfillment.

Two further consequences follow. A management plane that applies everything with its own credentials has to hold standing authority over every cluster, which makes it the most valuable thing to compromise. Presenting the user's credential lets it hold less (S-15). The prototype is not there yet: for each cluster it creates, it stores the token of a platform service account with administrator rights. And an API with fine-grained permissions lets more people use it directly, without a wrapper service in front of it (S-14). The prototype does not authorize at all.

A cluster that already exists adds one more consideration. It accepts a user's token only if its API server trusts the management plane's identity provider. That is a setting of the cluster's control plane, which an operator cannot always change, and on the test environment a cluster created outside the resource manager refused the caller's token. The management plane therefore needs a credential the cluster itself issued (EN-3). In the direct form it holds that credential, with the rights the cluster's administrator chose. In the agent form the credential stays on the cluster with the resource manager's own agent, and the management plane holds none (S-15).

### 2.4 Inventory, search, and history (MO-4)

OCM-io exposes cluster facts on `ManagedCluster` and per-work status through feedback rules. It has no search across clusters or resources and no history: a Kubernetes API serves current state, and etcd's revision history is compacted. Deployments that need search or history add separate services and databases beside the hub, so management state ends up split between Kubernetes objects and other stores, and those stores share neither etcd's availability nor its backup.

The resource manager keeps inventory in its database. For each Kubernetes cluster it holds a credential for, it indexes every kind of object apart from a short deny list, and it serves a filtered query over the result, which its console's search uses. It keeps no history of change, and it has no API that lists targets. A relational store is what makes both joins and history tables ordinary work.

Where one of those clusters is an OCM-io hub, the hub's `ManagedCluster` objects are indexed like any other object, with their labels and conditions, and the console's search finds them. The test environment shows two limits. Such a row is a generic object: it is not shown as a cluster, and nothing ties it to a target. And the rows of a cluster that was deleted are never removed, so more than half of the indexed rows there belong to clusters that no longer exist (IS-6).

### 2.5 Reliability and recovery (MO-5)

A hub's management state is in etcd, so it is highly available the way etcd is: three members tolerate one failure, and a new leader is elected within seconds with no external tool. It is backed up the way a cluster is: a snapshot restores the whole cluster's state, not management state alone. Running etcd across regions costs consensus latency on every write. Upgrading the hub's API server is an event for the management API, because they are the same thing. Management load and the hub cluster's own load also share one API server and one etcd, so exhausting either affects both.

A relational database brings its own established procedures: streaming replication, point-in-time recovery, and failover tooling. The cost is real. Failover is run by external tooling and commonly takes ten to thirty seconds. The options are compared in [topologies.md](topologies.md), and the choice among them is open (OQ-1).

The resource manager today runs as one replica, with durable workflows and no packaged backup or failover.

### 2.6 Independence from Kubernetes (MO-6)

An OCM-io hub is a Kubernetes cluster, so a cluster must exist before a cluster can be managed. The resource manager is an ordinary server. In this design's test environment it ran in a container on a workstation and created the two local clusters that later became the OCM-io hub and its managed cluster. Its target model is not limited to Kubernetes clusters.

## 3. Alternatives considered

| Alternative | What it helps | Why it is not the direction |
|---|---|---|
| A larger etcd quota and faster disks | Raises the storage ceiling somewhat | etcd documents 8 GiB as its suggested maximum and writes through one leader. It does nothing for MO-2 to MO-6. |
| More hubs with an aggregator above them | Scale, by partitioning | This is the status quo. Each hub remains a separate API and tenancy boundary, and the aggregator is a second system that adds visibility without one management API, one tenancy model, or one history. |
| An aggregated API server with SQL storage | Moves selected OCM-io kinds out of etcd | OCM-io's proposal 205 found that an APIService owns a whole group and version, that CRD validation, defaulting, conversion, and admission are not reproduced, and that Kubernetes-native resources stay in etcd. It keeps the Kubernetes tenancy and identity model and still needs a cluster. |
| A hosted Kubernetes API on Kine and PostgreSQL | Puts all hub state in SQL | The same proposal reported slow queries and duplicate-key conflicts and measured nothing. It is still the Kubernetes API, with its tenancy, identity, and query model. |
| Event-based `ManifestWork` alone | Removes work objects from a hub when the source is not the hub | It is a delivery protocol, not a management API. It brings no tenancy, attribution, search, or history. This design keeps it as an optional path for serving OCM-io agents (BH-23). |
| A key-value or document store behind a standalone API | Scale | See section 5. |
| **A standalone server API on a relational database** | All six motivations | Chosen. Its costs are in section 4. |

How the chosen direction meets a fleet that already exists is a second choice.

| Alternative | What it helps | Why it is not the direction |
|---|---|---|
| Deliver through the hub: the resource manager writes a `ManifestWork` for each target, and OCM-io delivers it | Reaches every registered cluster with nothing added to it | The hub stores an object per target for every intent and is in the path of every delivery, so MO-1 and MO-5 are not served. The resource manager's own delivery and inventory are bypassed, and what the cluster sees is still the work agent (W-8, RS-11). |
| Serve OCM-io's agents from the resource manager from the start | Nothing is added to a cluster | A klusterlet follows one source, so a cluster would have to leave its hub before the resource manager could do anything on it, and its hub-delivered add-ons could not follow (K-8). It also needs OCM-io's protocols implemented on a database first. It remains an optional later path (section 7.8). |
| **Beside OCM-io: a cluster stays registered with its hub and is also enrolled with the resource manager, which reaches it itself** | Existing clusters come under the management API one at a time, with OCM-io untouched and the hub out of the path | Chosen. It adds a small access piece to each enrolled cluster, and both systems can then deliver to one cluster (sections 7.2 and 7.3). |

## 4. Tradeoffs accepted

- **One more service and one more database.** They must be deployed, patched, monitored, backed up, and recovered.
- **Kubernetes tooling does not apply by default.** `kubectl`, RBAC, admission, and watch do not reach the management API. A Kubernetes-facing surface gives them back where they are wanted, at a cost that is counted (K-12).
- **Slower failover.** A database fails over through external tooling in tens of seconds. etcd re-elects in seconds by itself.
- **Two systems on the same clusters.** For as long as both are used, a cluster can receive workloads from both. Each workload has one owner, and people have to know which. Section 7.3 describes what was observed.
- **Something is added to each enrolled cluster.** The direct form adds a service account and a binding. The agent form adds an agent. Neither replaces the klusterlet, which stays for as long as the cluster is registered with a hub.
- **A credential and a route per cluster, at first.** In the direct form the management plane holds a credential for every enrolled cluster and has to reach its API. The agent form removes both needs and is not built (EN-3, S-15).
- **An ecosystem to carry.** Controllers and add-ons written for a hub keep working only where a hub API remains. Moving one is work for its owner.
- **Maturity.** The resource manager is experimental. Many required capabilities are not implemented; [coverage.md](coverage.md) lists them.
- **An open choice.** The high-availability arrangement for the datastore is not selected (OQ-1).

### 4.1 Questions this raises

- **Does a hub deliver anything for the resource manager?** No. The resource manager applies to enrolled clusters itself and keeps every record in its database. It creates no `ManifestWork` and no other hub object for what it delivers (W-8).
- **What is added to my clusters and to my hub?** Nothing, until a cluster is enrolled. An enrolled cluster carries a small access piece. The hub carries either nothing, or the objects of an add-on that its administrator chose to roll enrollment out with (EN-2, EN-5).
- **Are existing agents changed?** No. A klusterlet keeps serving its hub, and enrolling its cluster does not touch it (K-2).
- **What about `kubectl` and Kubernetes tooling?** They keep working against a hub in native mode and against a Kubernetes-facing surface where one is deployed (K-4, GO-1). What changes is where management state lives.
- **What about controllers and add-ons written for a hub?** They keep working wherever a hub API remains, and nothing forces them to move (K-8, K-9). Their use is supplemental and still encouraged.
- **What about GitOps?** It remains an authority. Pipelines keep applying OCM-io objects, can drive the management API through driving objects, and can apply to it directly (GO-3, GO-2, GO-9).
- **Why not raise etcd's quota?** A larger quota moves one ceiling. Tenancy, attribution, search, history, and recovery stay where they are (section 3).
- **Is a database a step down in availability?** In failover time, yes: tens of seconds with external tooling, against seconds for etcd. The effect is bounded. Clusters keep running while the management plane fails over (R-5), and recovery objectives are declared and drilled (R-1, V-6).
- **Is it acceptable not to choose a high-availability arrangement now?** Yes. The requirements fix the outcomes (R-1 to R-7) and forbid depending on one arrangement (A-5), so an operator can use a database they already run. What each arrangement needs is in [topologies.md](topologies.md) section 3. The choice of what a first production release validates is OQ-1, and it blocks no earlier step.
- **What about small deployments?** The same software runs as one process with an embedded database, with declared limits (AD-5, A-6).
- **Are there two sources of truth during the transition?** No. Each aspect of each scope has one system of record, each workload is held by one system, and either changes hands only by an explicit operation (X-1, X-2, X-3).
- **What if a deployment wants to stop?** Until the operator accepts a point of no return, every step has a way back, and removing the integration leaves OCM-io working (AD-4, F-12). A cluster is withdrawn by removing its access piece (EN-9).

## 5. Why a relational database

- **The questions are relational.** Tenants, workspaces, clusters, intent, placement, deliveries, permissions, and history refer to each other. "Which clusters in this tenant are affected by this intent" is a join.
- **Changes span records.** A target and its inventory item, or an intent and its new version, must change together. That is a transaction.
- **Integrity belongs in the store.** Uniqueness and references are enforced by the database, not re-checked in application code.
- **Durable workflow engines run on it.** The resource manager's orchestration already persists its steps in the same database.
- **Its operation is well known.** Backup, restore, point-in-time recovery, replication, and failover have established tools and practice. A server and a database is a deliberately ordinary stack.
- **A key-value store** leaves joins and secondary indexes to the application. **A document store** weakens transactions and integrity across documents. Either would move the work listed above into the resource manager.

**PostgreSQL for production.** It offers B-tree, composite, partial, and expression indexes; JSON columns with inverted indexes for resource attributes; trigram and full-text search; block-range indexes for large history tables; transactions with savepoints, selectable isolation levels, and constraint checks that can be deferred to commit; row locking that suits worker queues; row-level security; point-in-time recovery; and streaming replication. It is open source, and an operator can provide and run their own instance, which opens the scaling and availability arrangements in [topologies.md](topologies.md) section 3.

**SQLite for evaluation.** It runs inside the server process with no daemon, socket, or container, speaks enough standard SQL to share a schema, and makes continuous integration fast. Its source is in the public domain.

## 6. What exists today

### 6.1 The resource manager

| Capability | State today |
|---|---|
| Standalone server and API | Implemented. gRPC with an HTTP gateway, at an alpha API version. |
| Runs without Kubernetes | Implemented. An all-in-one container runs it with a bundled identity provider. |
| SQLite and PostgreSQL | Implemented behind one persistence interface, selected at start. |
| Durable orchestration | Implemented with a workflow engine on the same database. |
| Authentication | Implemented with OIDC. |
| Authorization | Not implemented. Any authenticated caller can read and act on everything. |
| Tenants and workspaces | Designed. Absent from the schema. |
| Per-tenant limits | Not designed in detail. |
| Deployment API | Create, get, list, resume, delete. No update or apply. Delete removes what was applied; no option leaves it in place. |
| Placement | Explicit list, all targets, exact label match. |
| Rollout | Immediate only. |
| Kubernetes delivery | Implemented on the platform side with server-side apply and no force. It leaves no object on a hub and no custom resource on the cluster. |
| Applied state | Not watched. A deployment stays reported as active after another writer changes or removes what it applied. |
| Failure reporting | A failed delivery is retried without a cap. Its reason is not available through the API. |
| Token passthrough | Implemented. The token is also persisted, contrary to the design. |
| Standing credentials | For each cluster it creates, the resource manager stores the token of a platform service account with administrator rights. |
| Signed intent | Verified on the platform side before applying with platform credentials. This is the only case in which a target's stored credential is used, and the target has to carry trust settings. |
| The resource manager's own agent on a cluster | Designed. Its service is defined and not served. |
| Target lifecycle API | None. Targets come from an add-on's declaration or a delivery's output. Target names are unique across the installation. |
| Taking in an existing cluster | None. Only clusters created through the built-in provider become targets. |
| Inventory and query | Implemented for resources its Kubernetes add-on indexes: every kind apart from a short deny list, watched from inside the resource manager's process. The indexer accepts a narrower setting, and nothing sets one. Rows of a deleted cluster are kept. No list of targets. |
| Console | Implemented, with pages for clusters, pods, nodes, namespaces, and targets, a search, and extension points. Its cluster pages show only the built-in provider's clusters. |
| Extensions | A server add-on and a console plugin. Both are built in today; registration at run time is designed. |
| History of change | None. Earlier strategy versions are kept only while a fulfillment exists. |
| More than one replica | Designed, not exercised. Add-on registrations and delivery routing are held in one process's memory. |
| Backup, restore, failover | Nothing packaged or documented. |
| Service to OCM-io agents | None. A proof of concept explores the `ManifestWork` seam. |
| Kubernetes-facing surface | None. A proof of concept runs standard reconcilers against delivery targets. |

### 6.2 OCM-io seams

| Seam | What it allows | Where the hub stores state today |
|---|---|---|
| Add-on templates | A hub's administrator describes manifests and an agent in one object, and OCM-io installs them on each cluster where the add-on is enabled, by name or through a `Placement` | Two objects for the add-on, and two more for each cluster while it is enabled there |
| The annotation that leaves an add-on's manifest in place | A manifest that carries it stays on the cluster when the add-on is disabled or removed | Nothing afterwards |
| The client configuration on `ManagedCluster` | A record of the cluster's API address and certificate authority, filled in where the klusterlet is configured with the cluster's external address | The `ManagedCluster` object |
| A hub registered with itself | The hub is a managed cluster like any other, so what is rolled out to clusters can be rolled out to it | The objects of one more managed cluster |
| The orphan delete option of a work | Deleting the work leaves what it applied in place | Nothing afterwards |
| gRPC registration driver | An agent registers and reports through a gRPC server and never talks to the hub's API server | The hub's Kubernetes API |
| Work source drivers (`kube`, `mqtt`, `grpc`) | A work agent follows a source other than the hub's API server | Wherever the source keeps it. OCM-io's own source is the hub's Kubernetes API. |
| The server-side `Service` interface in the SDK | One implementation per resource type lists resources for an agent and handles its status updates | OCM-io's implementations call the Kubernetes API. Another implementation can use a database. |
| Applied-work eviction rules | Applied work from a source the agent no longer follows survives for a grace period, and a resource that work from the current source also owns survives the eviction | On the managed cluster |
| `ManifestWorkReplicaSet` API | A fan-out object | The CRD is served on a v1.4.0 hub. The feature gate that starts its controller is off by default at v1.4.0 and on by default in OCM-io's API repository after that release. |
| Hosted deployment mode | Hub controllers run outside the cluster whose API they use | The Kubernetes API they point at |

## 7. Compatibility analysis

Sections 7.1 to 7.6 concern every deployment. Sections 7.7 to 7.9 concern the optional parts: a Kubernetes-facing surface, and a klusterlet that follows the management plane instead of a hub.

### 7.1 How each system delivers work

In OCM-io, desired state is a `ManifestWork` in the cluster's namespace on the hub. The work agent on the cluster reads it, applies each manifest, writes conditions and feedback back to the work's status, and keeps a local ledger, the `AppliedManifestWork`, of what it applied so that it can clean up. Controllers converge by replaying, not by transaction.

Anything that emits or serves work has to keep these semantics (W-4 to W-6):

- **Update strategies** decide how the agent treats an existing object: update it, create it only, apply it server-side, or read it only. Treating one as another changes drift repair.
- **Delete options** decide what happens to applied resources when the work is deleted.
- **`Applied` and `Available`** mean that the last apply produced no error and that the object exists. Neither is health. Health or completion is reported only by a rule the work configures.
- **Size.** The hub rejects a work whose manifests exceed 500 KiB by default.

The resource manager delivers differently. Desired state is a fulfillment in its database. For each selected target, its delivery applies every manifest to the cluster's API with server-side apply, under one field manager name and without force, and records the outcome as a row. It adds no owner reference and no label to what it applies, keeps no record on the cluster, and creates nothing on a hub. On the test environment a deployment of a `ConfigMap` and a `Deployment` was active 14 seconds after it was created, and a deployment of a single `ConfigMap` after about two seconds. What it lacks follows from the same simplicity: it does not watch what it applied, it cannot leave resources in place when intent is deleted, and it has no update (section 6.1).

### 7.2 Enrolling a cluster

The resource manager can deliver to a cluster and index it once it has three things: the address of the cluster's API, the certificate authority behind it, and a credential the cluster accepts. For a cluster it creates, the built-in provider supplies all three. For a cluster that already exists they have to come from the cluster, which is what enrollment arranges (EN-2).

The checks below were made on the test environment with unmodified OCM-io v1.4.0. The receiving side was a stand-in, because the resource manager cannot take in a cluster yet (GAP-32).

- **The piece is small.** A service account and a binding were enough. With a token for that account, calls made from where the resource manager runs could list, watch, read the API's discovery documents, and apply with server-side apply, on a cluster the resource manager had not created and whose API had refused the caller's own token.
- **OCM-io can roll it out.** One add-on template and its registration, applied to the hub, were enabled for two clusters. Within 12 seconds each cluster had the service account, the binding, and a small agent that requested a one-hour token and handed it over together with the cluster's certificate authority. The hub held two objects for the add-on in total, and two more for each cluster: the add-on's enablement for that cluster, and the work with which OCM-io's own add-on manager installs it. That work carries the access piece and nothing of the resource manager's.
- **The hub's cost can end.** The service account and the binding carried OCM-io's annotation that leaves a manifest in place when its add-on goes. When the add-on was disabled for a cluster, the agent and the hub's objects for that cluster were gone within a second. The service account and the binding remained, and the token went on working. Its holder then obtained a fresh token from the cluster's API, with nothing running on the cluster. Where the add-on was left enabled, the agent handed over a fresh token after 25 minutes.
- **The hub's record can verify it.** For all three clusters, the API address and certificate authority on the hub's `ManagedCluster` were the ones the cluster answered with, and the certificate authority the agent presented was the same.
- **The cluster's side can end it.** When the service account and the binding were removed, the next call was refused. The hub went on reporting the cluster as available throughout, and the klusterlet was never touched.
- **A hub can be enrolled the same way.** The hub was registered with itself using unmodified OCM-io and received the same piece through the same add-on.

Not exercised: selection by `Placement`, a role narrower than cluster administrator, a receiving endpoint that authenticates its callers, and any step performed by the resource manager itself.

### 7.3 Both systems on one cluster

Enrollment leaves a cluster with two systems that can deliver to it. Both apply through the cluster's API, and Kubernetes records which manager owns each field. Four cases were observed on one cluster of the test environment.

| Case | What happened |
|---|---|
| Each system delivers resources of its own | Neither affected the other. OCM-io's resources carried its work agent as field manager and an owner reference to its applied-work record. The resource manager's carried its own field manager and no owner reference. |
| The resource manager applies different content onto a resource OCM-io delivered | The apply was refused, and the resource was untouched. The resource manager retried about every seven seconds without a limit, its deployment stayed in the creating state, and no reason was available through its API. Deleting that deployment left the resource alone. |
| An OCM-io work that asks for server-side apply without force applies onto a resource the resource manager delivered | The work reported that it was not applied and named the resource manager as the conflicting manager. The resource was untouched. |
| An OCM-io work with the default update strategy applies onto a resource the resource manager delivered | The resource was overwritten without warning and gained OCM-io's owner reference. Deleting the work then deleted the resource. The resource manager went on reporting its deployment as active. |

Two rules follow. The resource manager never takes a resource from another writer, because it never forces (W-14). Whether OCM-io takes one from the resource manager depends on how the work is written, so a release has to say which settings do (W-15), and the resource manager has to notice when it happens (W-16). Today it does not (GAP-35).

### 7.4 Moving a workload

Server-side apply lets two managers hold the same fields when they agree on the content, so a workload can change hands without being touched. A `ConfigMap` delivered by a `ManifestWork` was also delivered, with identical content, by a deployment in the resource manager. The apply succeeded, and the object, with the same identity, then listed both managers. The work was deleted with OCM-io's orphan delete option. The object stayed, lost OCM-io's owner reference, and was managed by the resource manager alone. At no point did it cease to exist.

The way back was tried the same way and failed at its last step. With the work created again, deleting the resource manager's deployment removed the object within two seconds, although the work also covered it, and OCM-io's agent restored it four and a half minutes later as a new object. The resource manager has no delete that leaves resources in place (GAP-36). Until it has one, a move is safe in one direction only (W-7, W-17).

One object of one kind was moved. A workload of several objects, an object with fields that a controller on the cluster also writes, and a work that a `ManifestWorkReplicaSet` owns were not tried.

### 7.5 Placement

OCM-io `Placement` selects with label and claim selectors, expressions, taints and tolerations, and prioritizers, and publishes `PlacementDecision` objects of at most 100 decisions each. The resource manager selects by an explicit list, all targets, or exact label match. Copying a hub's labels, cluster sets, and claims onto targets preserves exact-match selection and loses the rest, and each loss has to be declared (M-4). A deployment that needs the rest can leave the decision with OCM-io and select by its result, which the integration copies onto the decided targets as a label (BH-14). Tools that read `PlacementDecision` are served in native mode while a hub decides, and by a projection where no hub remains (GO-8).

### 7.6 Identity

A `ManagedCluster` name is unique in one hub, and the resource manager requires target names to be unique across the installation. Identity therefore has to include the hub connection (I-2), and a generated target name has to be collision-free (I-7).

The test environment shows what a declared key can rest on. For each of the two clusters that the resource manager created and OCM-io then registered, both systems record the same API address and the same certificate authority, so an exact-match key on the two links the pair (I-3). One of them carries a different name in each system, so the name would not have linked it. The key cannot be assumed everywhere. A hub records an address only where the klusterlet was configured with one, and on an earlier build of the environment the two systems recorded different addresses for one API server. The targets have no labels and the `ManagedCluster` objects have no claims, so nothing else is there to compare, and the name remains the last resort.

### 7.7 Fan-out from a Kubernetes object

This section concerns the optional Kubernetes-facing surface.

A `ManifestWorkReplicaSet` is a work template, references to placements, and a rollout strategy. A fulfillment is manifests, placement, and rollout. The shapes correspond, which makes the replica set the natural driving object (BH-20). They are not equivalent (W-12): the replica set selects only through OCM-io `Placement`, reports an aggregate status, and creates per-cluster works that other tools may read.

One kind on one API server can have one reconciler (K-5). A v1.4.0 hub does not run OCM-io's controller for this kind unless its feature gate is turned on, and OCM-io has since made that gate on by default. Where that controller runs, the kind stays in native mode on that hub. Driving it then needs a surface without that controller, or a way for two implementations to share the kind (E-6).

### 7.8 Changing a cluster's work source

This section concerns the optional path on which a klusterlet follows the management plane instead of a hub (K-3). Enrolling a cluster does not involve it.

A work agent identifies its source by a SHA-256 hash of the source's address, and names every `AppliedManifestWork` with that hash as a prefix. It applies four rules to the applied work it holds.

- **Unmanaged.** If an applied work carries another source's hash, or its work is missing at the current source, the agent records an eviction start time.
- **Evicted.** When the grace period ends, the agent deletes the applied work. The period defaults to 60 minutes and is a public klusterlet setting. A period of 100 years or more disables eviction.
- **Shared resources are kept.** On deletion the agent deletes each resource the applied work recorded, unless another applied work also owns that resource. In that case it only removes its own owner reference.
- **Cancelled.** If the work is delivered again before the period ends, the agent clears the eviction start time.

These rules exist so that a hub can be restored or replaced. In a cutover they are both the risk and the mechanism. A resource that the new source does not deliver is deleted when the period ends, which W-7 forbids. A resource that the new source delivers first gains a second owner and survives, which is what the candidate procedure in BH-24 relies on.

The rules were observed on the test cluster's unmodified v1.4.0 klusterlet.

- **A real change of source address.** With eviction held off, the klusterlet was pointed at the same hub under another address. It registered again, and the work agent created a second applied work under the new source's hash. The `Deployment` and `ConfigMap` it had applied gained that applied work as a second owner. When the grace period was then set to two minutes, the old applied work was evicted on time. Both objects kept their identity, and the pod kept running without a restart. Changing back to the first address gave the same result.
- **Constructed cases.** With applied-work records built to stand for a previous source, a `ConfigMap` that only the previous source owned was deleted at eviction, an eviction that had started was cancelled when its work was delivered again, and a 100-year period suppressed an eviction whose time had already passed.

Two things were not exercised: a source that is a gRPC server, and a new source that holds different content for the same work.

### 7.9 Registration

Enrollment is not registration and changes none of what follows (M-6). This section matters only where a deployment later has a klusterlet register with the management plane.

A registration agent joins with a bootstrap credential, and the hub approves it and creates the cluster's namespace and RBAC. With the gRPC driver the agent talks only to a gRPC server, which today still writes every object to the hub's API server. The same driver against a server backed by the management datastore is the candidate for registration cutover (BH-25).

Registering again has a time limit. An agent registers again when its bootstrap configuration names another server, proxy, CA, or cluster name, and from that moment it stops renewing the cluster's lease until the new registration is approved. On the test hub approval was not automatic. When one approval was left for ten minutes, the hub reported the cluster's availability as unknown and tainted it unreachable after about five minutes. The workload on the cluster was untouched, and both signals cleared as soon as the registration was approved. A cutover therefore has to arrange acceptance before the agent switches (M-7).

## 8. GitOps and provenance

GitOps is an authority, not another delivery path. An applier owns every field in its desired state and reasserts it continuously, so any other writer to those fields starts a conflict it cannot win. The integration therefore observes what an applier wrote, writes only status, keeps its own records in the management datastore, and reports divergence as drift (GO-3 to GO-5). Injected tracking metadata would itself be drift to the applier, which is why correlation lives in the management datastore (I-1, C-3). A GitOps agent on an enrolled cluster is one more writer there. The resource manager never forces, so what that agent applies stays the agent's (C-5, W-14).

Provenance is a property of a delivery: evidence that a principal authorized exact content. Its strength depends on where it is checked. At the target, a compromised resource manager can withhold or replay but cannot make the target accept different content. On the platform side, it protects against forgery from outside and not against the platform itself. Where OCM-io's work agent applies, nothing is checked, so the management plane can record who asked and cannot make the target enforce it (S-6). A fact read from a hub is an observation with a source and a time, never evidence (S-8). A commit signature authenticates a commit, not the content that reached a cluster, unless a configured profile binds the two (GO-11).

## 9. Current-state claims and their sources

`source` means read from the named files. `live` means observed on the test environment: an OCM-io v1.4.0 hub with managed clusters, on Kubernetes v1.36 and v1.37, and a resource manager in a container. The environment was rebuilt between measurements, so figures in different rows were not all taken at one moment. The etcd figures were taken on Kubernetes v1.37 with etcd 3.7. `reported` means a figure that came with the motivation for this design and was not reproduced here. Paths under `server/`, `proto/`, `poc/`, `extensions/`, `sdk/`, and `docs/` are in the OCM Fleet repository. Paths under `ocm/`, `api/`, `sdk-go/`, `addon-framework/`, and `enhancements/` are in the OCM-io repositories of those names, and paths under `content/` are in the OCM-io website repository.

| Claim | Source | Basis |
|---|---|---|
| Nine hub objects per registered cluster; about 13.7 KB of etcd each | 200 placeholder clusters, etcd compacted before each reading | live |
| A two-manifest work takes about 2.0 KB before status and 5.3 KB as JSON once applied | 1,000 placeholder works; one real work applied by the agent | live |
| Applied objects record `work-agent` and no user | Field managers of an applied `Deployment` and of its `ManifestWork` | live |
| A v1.4.0 hub serves the `ManifestWorkReplicaSet` CRD and runs no controller for it by default. The gate that starts the controller is on by default after v1.4.0. | CRD list and hub deployments; `api/feature/feature.go` at the v1.4.0 tag and on the main branch | source and live |
| Registration drivers `csr`, `awsirsa`, `grpc`; hub work drivers `kube`, `mqtt`, `grpc` | `api/operator/v1/types_klusterlet.go`, `api/operator/v1/types_clustermanager.go` | source |
| The work agent accepts a source driver, and the klusterlet operator sets it from the registration driver | `ocm/pkg/work/spoke/options.go`, `ocm/manifests/klusterlet/management/klusterlet-work-deployment.yaml` | source |
| OCM-io's gRPC services persist through the Kubernetes API | `ocm/pkg/server/services/`; `enhancements/.../141-grpc-based-registration` | source |
| The gRPC server checks each agent event against the agent's cluster | `sdk-go/pkg/cloudevents/server/grpc/authz/kube/sar.go` | source |
| A server-side `Service` interface lists resources and handles status | `sdk-go/pkg/cloudevents/server/store.go` | source |
| A source may be a service with a database | `enhancements/.../224-event-based-manifestwork` | source |
| A work agent's source identity is a SHA-256 hash of the source's address | `ocm/pkg/work/helper/helpers.go`, `ocm/pkg/work/spoke/spokeagent.go`; the hash on the test cluster | source and live |
| Unmanaged applied work is evicted after a grace period, 60 minutes by default. A shared resource is kept. An eviction is cancelled when the work returns. A period of 100 years or more disables eviction. | `ocm/pkg/work/helper/helpers.go`, `ocm/pkg/work/spoke/options.go`, `ocm/pkg/work/spoke/controllers/finalizercontroller/`, `api/operator/v1/types_klusterlet.go`; a real change of the hub's address on the test cluster, and a test with constructed applied-work records | source and live |
| An agent registers again when its bootstrap configuration names another server, proxy, CA, or cluster name. It stops renewing the cluster lease until it is approved, and the hub then reports the cluster unknown and taints it. | `ocm/pkg/registration/register/common.go`, `ocm/pkg/registration/spoke/event_handler.go`; the same change of address, with one approval left for ten minutes | source and live |
| Manifest limit of 500 KiB by default | `ocm/pkg/work/webhook/option.go` | source |
| At most 100 decisions per `PlacementDecision` | `ocm/pkg/placement/controllers/scheduling/scheduling_controller.go` | source |
| `ManagedCluster` and `ManagedClusterSet` are cluster-scoped; `Placement` selects by labels, claims, expressions, tolerations, and prioritizers | `api/cluster/v1/types.go`, `api/cluster/v1beta2/types_managedclusterset.go`, `api/cluster/v1beta1/types_placement.go` | source |
| A work can name an executor service account | `api/work/v1/types.go` | source |
| `id.k8s.io` is a reserved claim name; a registered v1.4.0 cluster reports no claims by default | `api/cluster/v1alpha1/types.go`; the test cluster | source and live |
| No status-level observed generation on `ManagedCluster`, `Placement`, `ManifestWork`, `ManifestWorkReplicaSet` | `api/cluster/v1/types.go`, `api/cluster/v1beta1/types_placement.go`, `api/work/v1/types.go`, `api/work/v1alpha1/types_manifestworkreplicaset.go` | source |
| Proposal 205 investigated two SQL-backed arrangements and measured neither | OCM-io enhancements, proposal 205 | source |
| OCM-io says a hub can manage "thousands of clusters" and publishes no validated limit | `content/en/docs/concepts/architecture.md` | source |
| A single hub has been tested at roughly 3,700 managed clusters | Not reproduced on the test environment | reported |
| The resource manager selects SQLite or PostgreSQL at start | `server/internal/bootstrap/config.go`; the running container uses SQLite | source and live |
| It runs in one container with no Kubernetes | Process list of the running container | live |
| It created the hub cluster and one managed cluster of the test environment. Another managed cluster was created outside it. | Target and inventory records in its database | live |
| Authentication is enforced and authorization is not | Two demonstration users list the same records; an unauthenticated request is refused | live |
| The schema has no tenant or workspace | `server/internal/infrastructure/sqlite/migrations/00001_initial.sql` | source and live |
| The tenancy design has one provider tenant, consumer tenants, and access granted at workspace level | `docs/design/architecture/tenancy_and_permissions.md` | source |
| A platform service account with administrator rights is created on each cluster the resource manager creates, and its token, valid for 30 days, is stored | `server/internal/addon/kind/platform_sa.go`, `server/internal/addon/kubernetes/cluster_connection.go` | source |
| The caller's token is persisted with the fulfillment | `server/internal/domain/delivery.go`, `server/internal/infrastructure/sqlite/fulfillment_repo.go`; the stored record | source and live |
| Signed intent is verified on the platform side | `server/internal/addon/kubernetes/delivery_agent.go`, `server/internal/attestation/` | source |
| Server-side apply with a fixed field manager and no force | `server/internal/addon/kubernetes/delivery_applier.go` | source |
| Deployment API has no update; target, inventory, and delivery records have no API; the API is at an alpha version | `proto/ocm/v1alpha/`; requests to the running server | source and live |
| Placement is a list, all, or exact labels; rollout is immediate | `server/internal/domain/strategy_spec.go` | source |
| A delivery that fails with a retryable error is retried after a fixed delay, with no cap | `server/internal/domain/orchestration.go` | source |
| Target names are unique; a delivery output conflicts only on the identifier | `server/internal/infrastructure/*/migrations/00001_initial.sql`, `server/internal/infrastructure/sqlite/target_repo.go` | source |
| One delivery record per fulfillment and target; strategy versions deleted with the fulfillment | The same migration | source |
| Query over indexed resources works; the test environment held 764 indexed items when the byte figures below were taken | Requests to the running server; its database | live |
| In SQLite a target with its inventory item is about 2.4 KB, a delivery record about 0.5 KB, and an indexed resource about 1.6 KB, or 1.9 KB with indexes | Page and payload statistics of the running server's database: 3 targets, 3 delivery records, 764 indexed resources | live |
| Workflow tables hold about 37 KB for each of 95 orchestration runs, 93 of them completed, and are 67 percent of the database. No retention is configured. | The same statistics; `server/internal/infrastructure/goworkflows/` | source and live |
| The service for the resource manager's own agent is defined and not served | `proto/ocm/v1alpha/fleetlet_service.proto`, `server/internal/bootstrap/` | source |
| Add-on registrations and delivery routing are held in process memory | `server/internal/application/addon_manager.go` | source |
| Proofs of concept for the `ManifestWork` seam and for reconcilers on delivery targets | `poc/ocm-work-agent-adapter`, `poc/resource-manager-controller-runtime` | source |
| A deployment through the resource manager creates no hub object and no custom resource on the cluster. What it applies carries its field manager and no owner reference. A `ManifestWork` is one hub object and one `AppliedManifestWork` on the cluster. | `server/internal/addon/kubernetes/delivery_applier.go`; a deployment and a work on one cluster of the test environment | source and live |
| A deployment of two objects was active 14 seconds after it was created, and one of a single object after about two seconds | The deployments' timestamps | live |
| A cluster created outside the resource manager refuses the caller's token, and a cluster the resource manager created accepts it | The same request to both clusters from the resource manager's container | live |
| A target's stored credential is used only for intent with a verified signature, on a target that carries trust settings. Otherwise the caller's token is presented. The cluster targets of the test environment carry no trust settings. | `server/internal/addon/kubernetes/delivery_agent.go` and its unit tests; target properties in the database | source and live |
| A target enters only through an add-on's declaration or a delivery's output, and an add-on is given no handle on targets | `server/internal/domain/orchestration.go`, `server/internal/application/addon_manager.go`, `server/internal/bootstrap/addons.go` | source |
| An OCM-io add-on template, enabled for two clusters, put a service account, its binding, and an agent on each within 12 seconds. The hub held two objects in total and two for each cluster. | The hub and clusters of the test environment, with a stand-in receiver | live |
| A manifest that carries the annotation `addon.open-cluster-management.io/deletion-orphan` stays when its add-on is disabled, and the hub then holds nothing for that cluster | `api/addon/v1alpha1/types_managedclusteraddon.go`, `addon-framework/pkg/addonmanager/controllers/agentdeploy/utils.go`; the same add-on, disabled on two clusters in turn | source and live |
| With a token for the enrolled service account, list, watch, discovery, and server-side apply succeed from the resource manager's container, and the holder can request a fresh token | Requests from the container to a cluster the resource manager did not create | live |
| Removing the service account and its binding ends access at once, and the hub still reports the cluster as available | The same cluster | live |
| The registration agent fills the API address and certificate authority of a `ManagedCluster` from the klusterlet's external server addresses, and can change them only while it is bootstrapping. They matched all three clusters. | `api/cluster/v1/types.go`, `api/operator/v1/types_klusterlet.go`, `ocm/pkg/registration/spoke/registration/creating_controller.go`; fingerprints compared on the test environment | source and live |
| The resource manager and the hub record the same API address and certificate authority for the clusters the resource manager created | Target properties in the database; the `ManagedCluster` objects | live |
| The four cases of two systems delivering to one cluster (section 7.3) | One cluster of the test environment; each case was run twice | live |
| A workload moved from OCM-io to the resource manager kept its identity. Moved back, it was deleted and restored four and a half minutes later (section 7.4). | One `ConfigMap` on the test environment | live |
| OCM-io's work agent holds the built-in `admin` role across a managed cluster | Role bindings on a cluster of the test environment | live |
| The indexer watches every kind apart from a deny list, from inside the resource manager's process. It can be narrowed by kind and by namespace, and every target is given the default setting. Its design says the datastore's write path bounds how large a cluster that can sustain. | `server/internal/addon/kubernetes/inventory_discovery.go`, `server/internal/addon/kubernetes/inventory_indexer.go`, `server/internal/addon/kubernetes/inventory_indexing_runtime.go`, `server/internal/addon/kind/agent.go`, `docs/design/addons/kubernetes-inventory-and-delivery.md` | source |
| A hub's `ManagedCluster` objects are indexed and found by the console's search. Rows of deleted clusters are kept: 840 current rows for two clusters, and 1,138 rows of clusters that no longer exist. | Requests to the running server and its console; its database | live |
| The console lists as clusters only those of the built-in provider. Its extension points cover routes, set-up steps, cluster providers, onboarding actions, cluster detail tabs, and search result renderers. | `extensions/core/client/src/plugins/core-plugin/ClustersPage.tsx`, `sdk/build-utils/src/extensions/`; the running console | source and live |
| Server add-ons are compiled into the resource manager, and the console's plugins are listed in a registry generated when it is built. Registration at run time is designed. | `server/internal/bootstrap/config.go`, `docs/design/architecture/addon_integration.md`, `docs/ui/plugin-system.md` | source |
| A hub registered with itself is a managed cluster like any other | The hub of the test environment, joined to itself with unmodified `clusteradm` | live |

External references:

- etcd system limits: <https://etcd.io/docs/v3.6/dev-guide/limit/>
- etcd frequently asked questions: <https://etcd.io/docs/v3.6/faq/>
- Kubernetes custom resources: <https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/>
- Kubernetes considerations for large clusters: <https://kubernetes.io/docs/setup/best-practices/cluster-large/>
- OCM-io enhancement proposals: <https://github.com/open-cluster-management-io/enhancements>
- PostgreSQL point-in-time recovery: <https://www.postgresql.org/docs/current/continuous-archiving.html>
- PostgreSQL row security: <https://www.postgresql.org/docs/current/ddl-rowsecurity.html>
- PostgreSQL streaming replication: <https://www.postgresql.org/docs/current/warm-standby.html>
