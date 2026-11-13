# Requirements

**Status: normative, design only.** This document states what introducing OCM Fleet's resource manager architecture to OCM-io must achieve. Nothing in this directory is implemented. Candidate mechanisms are in [baseline-hypotheses.md](baseline-hypotheses.md); what exists today and what is missing is in [coverage.md](coverage.md).

**How to read it.** Section 1 is the premise: the decision and the six motivations every requirement traces to. Sections 2 to 4 say how the introduction proceeds. Sections 5 to 18 are the requirement families. Section 19 covers observability and verification, section 20 the constraints on how OCM-io is touched, and section 21 the non-goals.

**MUST** and **MUST NOT** are non-negotiable. **SHOULD** is expected unless a documented reason prevents it. **MAY** is optional. A requirement written as a plain statement of behavior binds like MUST. Identifiers are stable and other documents cite them.

---

## 1. Premise

### 1.1 The decision

The core management API for a fleet is a **standalone server API**: OCM Fleet's resource manager, with its own relational datastore. It is not an extension of the Kubernetes API, whether a CustomResourceDefinition or an aggregated APIService. Kubernetes can drive that API; it is not that API.

This steps away from how OCM-io works today, where a hub's Kubernetes API and etcd are both the management API and its store. It moves toward what is conventional in managed, multitenant cluster services: a dedicated management API with its own datastore.

The step is taken gradually, and beside OCM-io, not through it. A cluster stays registered with its hub. A cluster that opts in is also *enrolled* with the resource manager, which then delivers to it, indexes it, and shows it with its own machinery. OCM-io APIs, agents, controllers, and GitOps pipelines keep working, on the same clusters, while management moves to the resource manager one cluster and one workload at a time. The resource manager does its own work: it never asks a hub to deliver or to store anything on its behalf. It replaces the *essential* need for Kubernetes controllers and Kubernetes-stored state in fleet management. It stays compatible with their *supplemental* use, which remains encouraged.

The trade is explicit: one more service and one more database to operate, in exchange for scale, tenancy, attribution, search, history, and recovery properties that an extension of the Kubernetes API does not provide efficiently. The evidence, the alternatives considered, and the costs accepted are in [analysis.md](analysis.md).

### 1.2 Motivations (MO)

Every requirement traces to at least one motivation, or to the decision in section 1.1 where it exists to keep the introduction gradual and compatible. "Today" means unmodified OCM-io v1.4.x.

| ID | Motivation | Today | Required |
|---|---|---|---|
| **MO-1** | Scale | One hub's etcd and API server hold every per-cluster record. Fleets are split across hubs to stay inside their limits. | Management state grows in a datastore that scales independently of any Kubernetes control plane. The design target is 10,000 clusters in one management plane. |
| **MO-2** | Multitenancy | Tenants are separated by running separate hubs. Inside one hub, separation is namespaces and RBAC over kinds that are cluster-scoped. | One management plane with its own tenancy model, hard isolation between tenants, and per-tenant quality of service. |
| **MO-3** | Security and attribution | Controllers and agents act with their own service identities. The objects they apply record no end user. | Every management change is attributable to an end user or a declared workload identity, and a user's credential can be presented to a target without being stored. Access is fine-grained, and the management plane holds as little standing privilege as a delivery path allows. |
| **MO-4** | Inventory, search, and history | There is no fleet-wide search, and the API holds current state only. | Durable fleet-wide inventory and search, and a retained record of who changed what, and when. |
| **MO-5** | Reliability and recovery | Management state is protected as Kubernetes objects in the hub's etcd, together with the hub cluster. | Management state is protected by database procedures (backup, point-in-time recovery, replication, failover) against declared recovery objectives, independently of any cluster. |
| **MO-6** | Independence from Kubernetes | A Kubernetes cluster must exist before any cluster can be managed. | The management plane runs without Kubernetes, from one process on a laptop to a replicated production deployment, and can manage targets that are not Kubernetes clusters. |

### 1.3 Terms

- **Management plane**: the resource manager, its datastore, and the integration, taken together.
- **Resource manager**: the OCM Fleet server that owns the management API, authorization, persistence, and orchestration.
- **Integration**: everything that connects OCM-io and the resource manager. It observes hubs, enrolls the clusters that opt in, and carries out the procedures that move workloads and authority. Where a deployment wants them, it also provides a Kubernetes-facing surface and serves OCM-io agents.
- **Extension**: a server add-on and a console plugin built on the resource manager's extension interfaces. The integration is one (A-12).
- **Hub**: an OCM-io control plane, which is a Kubernetes API carrying OCM-io's APIs and controllers. A **hub connection** is one hub that the management plane is configured to read.
- **Observed cluster**: a cluster the management plane knows through a hub connection. It is listed and searchable. Nothing has been added to it, and nothing can be delivered to it.
- **Enrollment**: the opt-in step that gives the management plane access to one cluster. It adds a declared **access piece** to the cluster and makes the cluster a target. The cluster's registration with its hub is not touched. **Withdrawal** reverses it.
- **Access form**: how the management plane reaches an enrolled cluster. *Direct*: the resource manager calls the cluster's API with a credential the cluster issued at enrollment. *Agent*: the resource manager's own agent on the cluster connects out to the management plane and acts locally.
- **Target**: the resource manager's record of something it can deliver to. An enrolled cluster is one.
- **Delivery path**: how intent held by the resource manager reaches a cluster: *direct* or *agent*, following the access form, or optionally *work agent*, where the management plane serves OCM-io's work agent. Writing a `ManifestWork` to a hub is not a delivery path (section 12).
- **Aspect**: a coherent set of management facts with one owner at a time. The aspects are listed in section 4.
- **Scope**: what a deployment has selected: the hub connections, the clusters within them that are observed and those that are enrolled, and the workloads and aspects that have been moved. Nothing outside the scope is touched.
- **System of record**: the one place whose value for an aspect is authoritative.
- **Cutover**: the explicit operation that moves the system of record for one aspect, for one scope, from a hub to the resource manager. The smallest cutover is the **move** of one unit of workload.
- **Compatibility surface**: an OCM-io API or agent protocol that keeps working. Each API kind is offered in one **mode**: *native* (OCM-io's own controllers act on it, unchanged), *driven* (its objects are inputs that drive the management API), *projected* (read-only objects the integration writes from management-plane state), or *not offered*. On a hub every kind is in native mode unless a deployment selects otherwise.
- **Driving object**: a Kubernetes object in driven mode. Whoever writes it owns its desired-state fields.
- **Projection**: a Kubernetes object in projected mode. It is never read back as authority.
- **Kubernetes-facing surface**: an optional Kubernetes API that holds driving objects and projections (section 7).

### 1.4 Baseline

The compatibility baseline is unmodified OCM-io v1.4.x: its public APIs and documented agent protocols, with hub and agents within one minor version of each other. For the resource manager, requirements target interfaces that are implemented at the declared release, not ones that are only designed. A release declares the versions and combinations it validated and fails safe everywhere else (C-8).

---

## 2. Adoption principles (AD)

- **AD-1 Opt-in and inert by default.** Installing the management plane or connecting a hub MUST change nothing on that hub or its clusters. Every effect follows an explicit selection: a scope to observe, a cluster to enroll, a workload to move, or an aspect to cut over.
- **AD-2 Compatible at every phase.** Existing OCM-io users, manifests, pipelines, controllers, and agents inside the declared compatibility set (K-1) MUST keep working at every phase. Nobody is required to migrate in order to receive a benefit that an earlier phase claims.
- **AD-3 One system of record.** Each aspect of each scope has exactly one system of record at a time (X-1).
- **AD-4 Reversible until declared otherwise.** Every step toward the resource manager MUST have a tested way back, up to a point of no return that the operator explicitly accepts (X-6).
- **AD-5 Service-provider grade first, graceful when small.** The design MUST be sized for a managed service provider: many tenants, 10,000 clusters, an internet-facing API. The same software MUST run correctly as one process with an embedded database for the smallest deployment.
- **AD-6 Kubernetes is optional for the core and supported as a driver.** A core management function MUST NOT require a Kubernetes API server (A-2). Kubernetes objects, controllers, and GitOps remain supported ways to drive and extend the management plane (section 7).
- **AD-7 Each phase stands on its own.** A phase MUST be complete, supportable, and useful without the next one. It MUST NOT be a dead end: the next phase is reachable without enrolling clusters again or reinstalling agents.
- **AD-8 Declared, never implied.** Every limitation MUST be a declared gap that is observable at run time. Partial coverage MUST NOT be presented as success, and a designed capability MUST NOT be presented as implemented.
- **AD-9 Upstream and vendor neutral.** The integration uses public OCM-io APIs and documented seams only (section 20) and presumes no single product shape.
- **AD-10 Its own work, and a small footprint.** The management plane delivers, observes, and records with its own machinery. It MUST NOT hand that work to a hub (W-8). What it adds to a hub or to a cluster MUST be the declared minimum, counted and removable (N-2, N-11, C-6).

---

## 3. Outcomes (F)

These are what a deployment gets. Each depends on the phase and scope that provide it (section 4).

- **F-1 One management API for the fleet.** Membership, inventory, placement, delivery, and status for every cluster, across all connected hubs and for clusters that have no hub, are available from one standalone API (A-1).
- **F-2 Existing clusters, as they are.** Clusters already registered with an OCM-io hub become visible in the management plane when the hub is connected, and manageable through it when they are enrolled. Neither step re-registers a cluster, replaces or reconfigures its klusterlet, or changes its hub (M-1, EN-1, K-2).
- **F-3 OCM-io API users keep working.** Tools, controllers, and people that use OCM-io APIs continue to work in the declared compatibility mode (section 6).
- **F-4 Kubernetes and GitOps can drive it.** Desired state expressed as Kubernetes objects, including objects applied from Git, drives the management API (section 7).
- **F-5 Fleet growth does not grow the hub.** Per-cluster management records are held in the management datastore, and its capacity is planned independently of any hub's etcd (N-2, N-3). A workload the resource manager delivers adds no object to any hub (W-8).
- **F-6 One multitenant plane.** Tenants share one management plane with hard isolation and per-tenant quality of service, instead of one hub per tenant (section 13).
- **F-7 Attributable change.** For every management change the plane can answer who made it, what it changed, when, and on which clusters (S-2, IS-7).
- **F-8 Inventory, search, and history.** Clusters and selected resources across the fleet are searchable from one place, and past states and changes are retained (section 15).
- **F-9 Database-grade recovery.** Management state is backed up, restored, and failed over with database procedures against declared recovery objectives (section 16).
- **F-10 Runs anywhere.** The same management plane runs on a laptop with no Kubernetes and no database service, and as a replicated production deployment (A-3, A-6, A-9).
- **F-11 Declared fidelity.** For every aspect and compatibility mode, what is preserved, approximated, or unavailable is declared and reported per cluster (K-1, O-1).
- **F-12 A way back.** Removing the integration leaves OCM-io working for every scope still in native mode, and every enrollment, move, and cutover has a documented return path (AD-4, EN-9, U-3).
- **F-13 One way of working.** An enrolled cluster is deployed to, placed on, indexed, searched, and shown through the same API, console, and command line as a cluster the management plane created (EN-11).
- **F-14 Both systems on one cluster.** OCM-io and the resource manager can deliver to the same cluster at the same time without interfering. A workload moves from one to the other when its owner chooses, one at a time and without interruption (W-15, W-7).

---

## 4. Phases and scope (Z)

The introduction has three phases. They describe who leads, not a schedule.

| Phase | Who leads | What the management plane does | What OCM-io users notice |
|---|---|---|---|
| **1. Alongside** | OCM-io, for everything | Connects to one or more hubs and shows every cluster in scope, with its labels, cluster sets, and state, in its inventory, its search, and its console. It changes nothing on a hub or on a cluster. | Nothing. |
| **2. Leading** | The resource manager, for the clusters that enroll and for the intent it holds. OCM-io for everything else, on the same clusters. | Delivers intent created through its own API to enrolled clusters, indexes them, and holds every record of what it delivers. A workload that OCM-io delivers stays with OCM-io until its owner moves it. | Their hub, agents, objects, and pipelines work as before. An enrolled cluster carries its declared access piece (EN-2). A hub stores nothing for a workload the resource manager delivers (W-8). |
| **3. Standalone** | The resource manager | Manages enrolled clusters with no hub required. A Kubernetes-facing surface remains available to those who want one. | A hub is optional. They keep one only for what they still choose to run on it. |

A cluster passes from observed to enrolled by enrollment (section 11), not by a change to its registration. Aspects then move independently. The table shows the system of record for each aspect.

| Aspect | Alongside | Leading | Standalone |
|---|---|---|---|
| Cluster membership | Hub. The resource manager holds an observed copy. | Hub, for clusters registered with it, until membership is cut over (M-7). The resource manager holds the observed copy and each cluster's enrollment. | Resource manager |
| Access to a cluster | None | Resource manager, for enrolled clusters | Resource manager |
| Workload intent | Hub | One system for each unit of intent: the hub for intent created there, the resource manager for intent created through its API or moved to it (X-2) | Resource manager |
| Per-cluster delivery records and status | Hub (`ManifestWork`) | Hub, for the hub's own works. Resource manager for its own deliveries, never as a hub object (W-8). | Resource manager |
| Placement decisions | Hub (`PlacementDecision`) | Hub, for OCM-io placements. Resource manager for its own placement, which can select by facts the hub holds (M-4). | Resource manager |
| Resource inventory | Not collected. Hub-held facts only. | Resource manager, for enrolled clusters | Resource manager |
| Add-on enablement | Hub | Hub for add-ons that stay on OCM-io's addon-framework. Resource manager for add-ons moved to its API. | Resource manager. An add-on that needs a hub API is a supplemental controller. |
| Tenancy and access | Hub RBAC for the hub API. Resource manager for its own API. | The same. Hub RBAC still governs the hub and any Kubernetes-facing surface. | Resource manager |
| History | Resource manager, for what it observes and does | Resource manager | Resource manager |

- **Z-1 Default scope is empty.** Nothing is observed, enrolled, moved, mapped, driven, projected, or cut over unless it is individually selected and recorded.
- **Z-2 Scope units.** Scope MUST be selectable per hub connection, per set of clusters within it, per aspect, and, for a move, per unit of workload.
- **Z-3 Phase is declared and reported.** A deployment MUST declare, and the management plane MUST report, the phase and system of record of each aspect for each scope, and the enrollment state and access form of each cluster (O-1).
- **Z-4 Independent aspects.** Aspects are cut over independently. Where one cutover depends on another, the dependency MUST be declared before either is started.
- **Z-5 Expansion is additive.** Adding scope, an enrolled cluster, a moved workload, an aspect, or a phase MUST NOT require enrolling other clusters again, reinstalling agents, rewriting correlations, or downtime. Where an aspect's cutover changes how an agent connects, that change is part of the cutover (M-7, W-7).
- **Z-6 Narrowing deletes nothing.** Narrowing or disabling scope stops new effects. It MUST NOT implicitly delete data or objects on either side and MUST leave both sides valid.
- **Z-7 Not mapped is a state.** An aspect that is not mapped MUST be reported as "not mapped", distinguishable from empty, unknown, and healthy on every surface the release declares.
- **Z-8 A phase is claimed with evidence.** A release MAY claim support for a phase, aspect, access form, or scale profile only with the verification evidence that section 19 requires for it.

**What binds when.** Every requirement binds from the first phase, with these exceptions. Section 11 binds when a first cluster is enrolled, except EN-1 and EN-12, which bind from the first phase. Section 12 binds when the management plane first delivers workload to a cluster, except W-5, W-8, and W-11, which bind from the first phase. M-7 binds at a registration cutover. K-3 and W-4 bind only where the management plane serves OCM-io agents. K-5 and K-6 bind when a kind is first offered in driven or projected mode. GO-2, GO-4, and GO-7 bind when a Kubernetes-facing surface is deployed, and GO-9 binds when the management API first accepts workload intent for a scope. The other GO requirements bind from the first phase, because GitOps pipelines already exist. X-3 to X-6 bind when a cutover is performed.

---

## 5. Architecture and datastore (A)

Traces to MO-1, MO-5, MO-6.

- **A-1 Standalone API.** The core management API MUST be served by the resource manager at its own endpoints. It MUST NOT be a CustomResourceDefinition or an aggregated APIService, and a client MUST NOT need a kubeconfig or Kubernetes credentials to use it.
- **A-2 No Kubernetes in the core path.** Registration and membership, inventory, placement, delivery orchestration, status, tenancy, authorization, and history MUST function with no Kubernetes API server, Kubernetes controller, or Kubernetes-stored state in their path, other than the API servers of the clusters being managed.
- **A-3 No recursion.** Starting a management plane MUST NOT require an existing Kubernetes cluster. A management plane running on a workstation or a virtual machine can create or enroll the first cluster.
- **A-4 Relational primary store.** Management state MUST live in a relational datastore that is its only system of record. Related records change in one transaction, and uniqueness and referential integrity are enforced by the database, not by convention.
- **A-5 PostgreSQL in production.** Production deployments MUST be supported on PostgreSQL, including an instance that the operator provides and operates. The management plane MUST NOT depend on one particular PostgreSQL distribution, operator, or high-availability tool (OQ-1).
- **A-6 Embedded database for evaluation.** Evaluation, demonstration, and continuous-integration deployments MUST be supported on an embedded SQLite database: no database service, the same API and behavior, declared limits. It is not a production topology (R-14).
- **A-7 One behavior across backends.** A feature MUST behave the same on every supported backend or declare the difference. A capability that only one backend has, such as row-level security or point-in-time recovery, MUST be reported as present or absent.
- **A-8 Durable orchestration.** Accepted intent MUST survive restart. Orchestration steps are persisted before they run, resume after failure, and are safe to repeat.
- **A-9 Replicas.** A production deployment MUST be able to run more than one resource manager replica against the same datastore without double delivery, lost updates, or state that exists only in one replica's memory. A single replica MUST remain a correct deployment (AD-5).
- **A-10 Not only Kubernetes.** Core records MUST NOT assume that a target is a Kubernetes cluster. The integration maps OCM-io clusters onto the general target model and does not narrow that model.
- **A-11 Extensions get the same guarantees.** Add-on services that extend the management plane MUST integrate at the standalone API, where they can define their own resource types and use its delivery, placement, rollout, authorization, and inventory. They inherit its tenancy, attribution, history, and recovery properties, instead of adding state to a hub. The API they build on is versioned, and a release declares which versions are stable.
- **A-12 The integration is an extension.** The integration with OCM-io MUST be built on the extension interfaces of A-11. Its resources are defined at the standalone API and its views in the console, and it has no private path into the resource manager. Taking in a cluster that already exists is a capability of the management plane itself (EN-13), which the integration uses and does not own.

---

## 6. Compatibility with OCM-io (K)

Traces to the decision in section 1.1. Four groups depend on OCM-io today: agents on managed clusters, writers of OCM-io objects (people, GitOps appliers, controllers), readers of status and decisions, and add-ons. Each API kind they use is offered in one of the modes defined in section 1.3. On a hub everything stays in native mode unless a deployment selects otherwise, so all four groups keep what they have.

- **K-1 Declared compatibility set.** A release MUST declare, for each OCM-io API kind and agent protocol: the versions supported, the mode offered in each phase, and the fidelity (preserved, approximated, or unavailable). Anything outside the set MUST fail safe, not be guessed.
- **K-2 Agents are left alone.** A klusterlet already on a cluster, meaning its registration agent and work agent at a version in the compatibility set, keeps serving its hub exactly as before, in every phase and whether or not the cluster is enrolled. Enrollment MUST NOT replace, reconfigure, restart, or remove it. Having a klusterlet follow the management plane instead of a hub is a separate, declared operation (M-7, W-7), on a path the management plane MAY offer (K-3). It is never a precondition for enrolling a cluster or for delivering to it.
- **K-3 Published protocols only.** Where the management plane serves OCM-io agents itself, it MUST use OCM-io's published agent protocols, which are the event-based `ManifestWork` protocol and the gRPC registration flow. It MUST NOT depend on a private dialect or an agent patch.
- **K-4 Native mode is untouched.** An object in native mode is acted on by OCM-io's own controllers exactly as it would be without the integration. The integration MUST NOT write it, annotate it, or change how it is delivered or reported.
- **K-5 Driven mode has one reconciler.** A kind MAY be offered in driven mode only where no OCM-io controller reconciles the same objects. Its semantics MUST be preserved, or declared approximated field by field (F-11). A field the driver does not honor MUST be rejected or reported, never silently ignored.
- **K-6 Projections are read-only, marked, and selectable.** A projection MUST be written only by the integration, MUST be identifiable as a projection, and MUST NOT be read back as authority. A projection MUST NOT be placed where an OCM-io controller would act on it as a native object. Projection MUST be selectable per scope, so its cost is paid only where a consumer needs it. A per-target record of workload delivery is never projected (GO-7).
- **K-7 Readers keep their signals.** The status and decisions that OCM-io tools read on a hub stay there, in native mode, for everything OCM-io still does: `ManagedCluster` conditions, labels, and claims; `PlacementDecision`; `ManifestWork` conditions and status feedback; and `ManagedClusterAddOn` status. For a workload that has been moved to the resource manager, status is read from the management API, and the loss of the hub object is declared before the move (U-4). Where no hub remains, `ManagedCluster` and `PlacementDecision` MAY be offered as projections with declared freshness (K-6).
- **K-8 Add-ons keep working.** OCM-io add-ons built on the addon-framework MUST keep registering, renewing leases, and receiving their manifests wherever a hub API remains and the cluster's klusterlet still follows it. Moving an add-on to the standalone API is opt-in per add-on and is never a precondition for a phase.
- **K-9 Supplemental controllers.** A controller that watches or writes OCM-io objects MUST keep working against objects in native mode. For driven and projected kinds, every difference in behavior that a controller could observe MUST be declared.
- **K-10 Operational procedures.** `clusteradm` and documented OCM-io procedures MUST keep working for scopes in native mode. The release MUST document which OCM-io operations are available, degraded, or unavailable in each phase and mode.
- **K-11 Version skew.** The supported skew between the management plane, hubs, and agents MUST be declared. Upgrades within it are rolling (R-8).
- **K-12 Compatibility cost is counted.** The hub objects that exist because of the integration (the objects of an enrollment that is rolled out through OCM-io's add-on mechanism, driving objects, and projections) MUST be countable per managed cluster and reported, next to the native per-cluster objects that OCM-io itself keeps (N-2, O-6).

---

## 7. Kubernetes-driven operation and GitOps (GO)

Traces to the decision in section 1.1. A **Kubernetes-facing surface** is a Kubernetes API that holds driving objects and projections. It may be an existing hub or a small API server kept for this purpose. Nothing in this section is needed to enroll a cluster or to deliver to it.

- **GO-1 The surface is optional.** The management plane MUST work without a Kubernetes-facing surface (A-2). Deploying one is an operator's choice.
- **GO-2 Declarative in, idempotent.** A driving object is desired state. Applying the same object twice is safe, and deleting it removes exactly the unit it describes, with no out-of-band step in normal operation.
- **GO-3 The writer owns its fields.** Where a GitOps applier or any other writer manages an object, its desired state is authoritative for every field it contains. The integration MUST NOT write, correct, or revert those fields, MUST NOT inject metadata into the object (C-3), and MUST NOT write to the Git source.
- **GO-4 One status writer.** On a driving object the integration writes only the status. Everything else it records goes to the management datastore (I-1). Each object class has exactly one status writer (K-5).
- **GO-5 No second reconciler.** The integration MUST NOT fight another writer. Divergence between what a writer applies and what the management plane intends MUST be reported as drift or conflict, with the competing manager named where the API exposes it, and never converged by repeated writes.
- **GO-6 Pipelines never block.** GitOps pipelines MUST keep applying objects when the integration or the management plane is down, degraded, or removed. The integration MUST NOT add a webhook, finalizer, required field, or admission dependency that blocks them. Effects resume when the management plane returns.
- **GO-7 Fan-out stays out of Kubernetes.** For intent expressed by a driving object, per-target records are held in the management datastore. They MUST NOT be materialized as per-cluster Kubernetes objects, on a hub or on any other surface (W-8).
- **GO-8 Selection results for GitOps tools.** Where a GitOps tool consumes placement results in OCM-io's form, a `PlacementDecision` MUST be available for the selected scopes in native or projected mode (K-7).
- **GO-9 Direct declarative API.** The management API MUST offer an idempotent declarative operation (create or update, dry run with a difference report, and delete) so that a pipeline can manage intent without any Kubernetes API.
- **GO-10 Pipeline identity.** A non-interactive pipeline authenticates as a workload identity. That identity is what attribution records (S-2). A user is not impersonated.
- **GO-11 A commit is a source claim.** A Git repository, revision, author, or commit signature MUST NOT be reported as the authorization for a change unless a configured profile binds it to the exact content delivered. Otherwise it is recorded as a source claim. Source metadata is never injected into OCM-io objects.
- **GO-12 Rollback is a change.** A revert in Git is observed and reconciled as an ordinary desired-state change, not as an error or a conflict.

---

## 8. Authority and cutover (X)

Traces to the decision in section 1.1.

- **X-1 Exactly one system of record.** For every aspect of every scope there is exactly one system of record at a given time. It MUST be explicit, reported, and stable across restarts.
- **X-2 Authority is per aspect, and per unit of intent.** Authority is defined per aspect (section 4), not per object. For workload intent the scope can be as small as one unit of intent: each unit is held by exactly one system, a hub or the resource manager. Inside an object, each field has at most one writer.
- **X-3 Cutover is an operation.** A cutover MUST be an explicit operation with stated preconditions, a dry run, a verification step, and a rollback. It is never a side effect of installation, upgrade, enrollment, or a configuration reload. Moving one unit of workload is a cutover of the smallest scope.
- **X-4 No dual authority.** At no time during a cutover do both systems accept writes as the authority for the same aspect of the same scope. Any window in which writes are refused MUST be declared and bounded. While a workload is being moved, both systems hold it only with identical content (W-7).
- **X-5 Nothing ambiguous is left behind.** After a cutover, objects that the former system of record holds for that aspect are driving objects, projections, or removed, as declared. A second copy that looks authoritative MUST NOT remain.
- **X-6 Rollback.** Rolling back a cutover MUST return authority without losing intent that was accepted before the rollback. The point after which rollback is no longer possible MUST be explicit and accepted by the operator.
- **X-7 No echo.** The integration MUST NOT treat its own writes as external change, and MUST NOT rely on metadata injected into objects it does not own to tell the difference (C-3).
- **X-8 Controller-owned fields.** Where an OCM-io controller owns a field, the integration MUST NOT write it, and MUST preserve values and fields it does not understand.
- **X-9 A hub's control plane is not a workload.** On a hub that is itself registered as a managed cluster, and whether or not that cluster is enrolled (EN-14), OCM-io's control-plane namespaces and the management plane's own components are excluded from delivery, deletion, and cleanup.
- **X-10 A new aspect needs an authority decision.** An aspect with no declared system of record MUST NOT be mapped.

---

## 9. Identity and correlation (I)

Traces to the decision in section 1.1 and to MO-4.

- **I-1 Durable correlation.** Every pair of records that represents the same thing in both systems MUST have a durable, bidirectional correlation. It is stored in the management datastore, never in OCM-io object metadata (C-3), and is recoverable after restart.
- **I-2 Identity is scoped by hub.** A cluster observed through a hub is identified by the hub connection together with its `ManagedCluster` name. The same name on two hubs denotes two clusters unless a declared stable identity proves otherwise.
- **I-3 Declared match.** Whether a `ManagedCluster` and a cluster the management plane already knows are the same cluster is decided by a declared rule applied in order: an exact-match key that the deployment declares, such as the API address and certificate authority that both sides record; then recorded aliases; then a name match that applies only when exactly one unlinked record on each side carries the name. One candidate links. Several candidates are reported as a conflict and nothing is linked. The basis of every link is recorded.
- **I-4 Incarnations.** A Kubernetes UID is never the sole or primary key. Recorded markers such as the UID and creation time detect that a name now denotes a different object. A new incarnation inherits nothing from its predecessor: not identity, cleanup obligations, or history.
- **I-5 Corrections are durable.** A recorded correlation can be cleared or reassigned by an explicit operation. The correction is durable and is not silently undone by a later name match.
- **I-6 No fabricated membership.** The integration MUST NOT create a `ManagedCluster` to stand for a cluster that OCM-io did not register. A projected `ManagedCluster` exists only for a cluster whose membership the resource manager holds, and it is marked as a projection (K-6).
- **I-7 Generated names.** Any name the integration generates MUST be deterministic, valid in the system that receives it, stable across retries, and unique wherever the receiving system requires uniqueness.
- **I-8 Discoverable both ways.** An operator can find the counterpart of any record from either side through the management API, without reading OCM-io object metadata.
- **I-9 Orphans without history.** Orphan detection MUST work without a complete event history. If one side deleted something while the other was down, the integration still converges.
- **I-10 Identity survives phases.** A cluster keeps its management-plane identity, and its history, through observation, enrollment, a change of access form, withdrawal followed by a later enrollment, and a change of registration. No phase re-creates it. A cutover updates the recorded markers itself, so that it is not taken for a new incarnation (I-4).

---

## 10. Membership and inventory (M)

Traces to MO-1, MO-2, MO-4.

- **M-1 Observation.** Every cluster in the declared scope of a hub connection is held as a cluster record that carries its identity, membership state, labels, cluster sets, and claims. Observation needs a read-only credential that the hub's administrator creates (S-10), and nothing else: the integration creates no object on the hub, and nothing changes on any cluster. An observed cluster is listed and searchable. Placement does not select it until it is enrolled (EN-12).
- **M-2 Many hubs.** One management plane MUST support several hub connections. Each has its own scope, credentials, status, and failure isolation: an unreachable hub affects only its own clusters, which are reported stale (M-3).
- **M-3 Freshness.** Join, leave, acceptance, availability, label, and claim changes MUST be reflected within the declared budget (N-6). During an outage the last known values are kept and marked stale with the time they were last confirmed. Stale is never shown as empty.
- **M-4 Selectable facts.** The labels, cluster sets, and claims that a hub holds for a cluster MUST be usable by the resource manager's placement once the cluster is enrolled, and MUST follow changes on the hub within the freshness budget (M-3). The integration never writes them back to the hub. A deployment MAY also select by the result of an OCM-io `Placement` that a hub has already decided. Where the resource manager's selection is less expressive than OCM-io `Placement`, each loss MUST be declared.
- **M-5 Leaving is explicit and durable.** Excluding a cluster from observation and withdrawing its enrollment (EN-9) are explicit operations that affect exactly the named clusters. Each is durable: a cluster that was excluded or withdrawn is not silently observed or enrolled again while it remains in scope.
- **M-6 Registration in native mode is OCM-io's.** While membership is in native mode, the integration MUST NOT issue bootstrap tokens, approve certificate signing requests, accept a `ManagedCluster`, or require a particular registration tool. Enrollment is not registration and does none of these.
- **M-7 Registration cutover.** Membership for a scope is cut over only by an explicit operation. For an enrolled cluster it makes the resource manager the system of record for the cluster's membership, after which the cluster's registration with a hub is no longer needed. Where a deployment keeps OCM-io's klusterlet as the cluster's agent, the klusterlet registers with the management plane through a published protocol (K-3); acceptance is then a management API operation with attribution (S-2), arranged before the agent switches, so that no cluster is reported unreachable because of the cutover. OCM-io registration objects for those clusters are projections or absent (X-5).
- **M-8 Clusters without a hub.** A cluster that no hub knows can be enrolled and managed in any phase, with the same identity model and the same access forms (EN-13).
- **M-9 Known clusters are linked, not duplicated.** A cluster that the management plane already knows and that is later observed through a hub connection is linked to its existing record when the declared match identifies it (I-3). An unmatched record stays visible and is reported as unregistered.
- **M-10 Target lifecycle.** Enrollment creates a target with its inventory and keeps its labels current, and withdrawal removes exactly that target, independently of workload records. Where the resource manager offers no such surface, enrollment is unavailable or uses a declared workaround with its cost stated.
- **M-11 Membership is not health.** Presence of a target says the cluster is a member. It MUST NOT be presented as reachability or health.

---

## 11. Enrollment and access (EN)

Traces to the decision in section 1.1 and to MO-1, MO-3. **Enrollment** is how a cluster that already exists becomes a target of the resource manager. It is separate from registration with a hub, which it neither needs nor changes. Two **access forms** are recognized. In the *direct* form the resource manager calls the cluster's API with a credential the cluster issued. In the *agent* form the resource manager's own agent runs on the cluster, connects out to the management plane, and acts with a credential that stays on the cluster.

- **EN-1 Opt-in per cluster.** A cluster is enrolled only by an explicit act of someone entitled to administer it: the cluster's own administrator, or the hub's administrator acting through the hub. Observing a cluster never enrolls it (AD-1).
- **EN-2 What is added is declared and small.** Enrollment adds to a cluster only the access piece of the chosen form. For the direct form that is a service account and its role binding, which are built-in Kubernetes kinds. For the agent form it is the resource manager's agent with what it needs to run. The direct form MUST NOT need a custom resource definition, and MUST NOT need anything that keeps running on the cluster once the cluster is enrolled. Whatever a roll-out route runs on the cluster to hand the credential over is declared with that route and can be removed without ending the enrollment (EN-5). No form changes the klusterlet, its configuration, or any object OCM-io owns (K-2, C-3). What was added is listed per cluster (N-11).
- **EN-3 Access forms.** A release declares which access forms it offers and reports the form of each enrolled cluster. The direct form needs the resource manager to reach the cluster's API. A cluster that accepts no inbound connection MUST be enrollable through the agent form; until that form is offered, such a cluster is observed only and is reported as not enrollable. Changing a cluster from one form to the other MUST NOT interrupt its workloads or change its identity (I-10).
- **EN-4 Roll-out without a step per cluster.** Enrolling many clusters MUST NOT need a person to act on each one. The access piece MUST be deliverable through OCM-io's own add-on mechanism, to the clusters a hub administrator names or a `Placement` selects, with OCM-io unmodified (C-1). It MUST also be deliverable with no hub object at all, as a manifest applied to the cluster by any means. The management plane MUST NOT need write access to a hub for either (S-10).
- **EN-5 The hub cost of enrollment is bounded and can be zero.** Enrollment through OCM-io's add-on mechanism costs a declared number of hub objects, in total and per cluster, for as long as the add-on stays enabled for that cluster (N-2). They are OCM-io objects that the hub's administrator and OCM-io's own add-on manager create, including the work that installs the access piece. By default the management plane creates none of them (S-10), and none carries intent of the resource manager (W-8). It MUST be possible to disable the add-on for a cluster once the cluster is enrolled, leaving the access piece in place and no hub object for that cluster.
- **EN-6 An enrollment is verified.** Before a target is created, the management plane MUST check that the address, trust anchor, and credential presented for a cluster work against that cluster. For a cluster in the scope of a hub connection it MUST also check them against what the hub records for that cluster (I-3). An enrollment that fails a check creates nothing and is reported. Enrollment requests are authenticated: nobody can create or replace a target without proving the right to do so.
- **EN-7 The cluster's side decides the rights.** The rights the management plane holds on a cluster are those granted by what the cluster's or the hub's administrator applied. The management plane MUST work with less than cluster-administrator rights, MUST report the rights it holds on each cluster (S-15), and MUST report a delivery that is refused for lack of rights as exactly that, per target (W-10).
- **EN-8 Credentials are short-lived and renewed.** A credential held for the direct form is time-limited. It is renewed before it expires, without a person and, where the cluster's API allows it, without any component on the cluster. The management plane keeps it only in its secret store, never in a record that a query can return (S-11). A cluster whose credential has lapsed is reported as needing enrollment, never as empty or as healthy (M-11).
- **EN-9 Withdrawal.** Withdrawing a cluster is an explicit operation that removes its target, its credential, and its indexing, and by default leaves delivered workloads in place. Removing the access piece from the cluster ends the management plane's access at once, whatever the management plane does, and the cluster is then reported as withdrawn, not as failing. Withdrawal is durable (M-5) and leaves the cluster's registration with its hub untouched.
- **EN-10 Independent of the hub afterwards.** Once a cluster is enrolled, delivery to it, its inventory, and its status MUST NOT depend on its hub being reachable or on the cluster remaining registered there. What still comes from the hub, such as labels, is kept at its last known value and marked stale (M-3). A deployment MAY select, for a cluster it cannot reach otherwise, an access route that passes through the cluster's hub. Such a cluster is reported as depending on that hub and is outside this guarantee.
- **EN-11 No second-class cluster.** An enrolled cluster is delivered to, placed on, indexed, searched, and shown through the same API, console, and command-line paths as a cluster the management plane created. Its origin is recorded and shown. It MUST NOT select a different way of working.
- **EN-12 Observed is not enrolled.** A cluster that is observed and not enrolled MUST be reported in that state on every surface, distinguishable from an enrolled cluster that cannot be reached. Placement MUST NOT select it, and intent that names it is refused with that reason instead of waiting.
- **EN-13 A capability of the management plane.** Enrolling a cluster that already exists is offered for any Kubernetes cluster, with or without a hub (M-8). The integration adds a hub's knowledge to it and MUST NOT be the only way in (A-12).
- **EN-14 A hub can be enrolled.** A hub that is registered with itself as a managed cluster can be enrolled like any other cluster. By default the access piece on a hub grants reading only (X-9, T-9).

---

## 12. Work delivery (W)

Traces to MO-1, MO-3. A **delivery path** is how intent held by the resource manager reaches a cluster. Two paths follow from the access forms of section 11: *direct*, where the resource manager applies through the cluster's API, and *agent*, where the resource manager's own agent applies on the cluster. A third path, *work agent*, is optional: the management plane serves OCM-io's work agent through the event-based protocol, for a cluster whose klusterlet follows the management plane (K-3). Writing a `ManifestWork` to a hub is not a delivery path of the management plane (W-8).

- **W-1 One active path.** For each cluster and each unit of workload there is exactly one active delivery path in steady state. Any period with two paths MUST be explicit, bounded, and reported.
- **W-2 Paths declare their properties.** Each path a deployment uses MUST declare what a delivery adds to the cluster besides the workload, the identity that applies at the target, where evidence is verified (S-6), and its status fidelity.
- **W-3 Newer intent always wins.** A stale delivery attempt MUST NOT overwrite newer intent, whichever path carried it.
- **W-4 Work semantics are preserved.** On the work-agent path, where the management plane serves `ManifestWork` to OCM-io's work agent, it MUST preserve update strategies, delete options, the executor, feedback rules, condition rules, and completion semantics exactly. It MUST NOT add or rewrite work configuration to gain observation, drift repair, or ordering.
- **W-5 Apply status is not health.** `Applied` and `Available` MUST NOT be presented as health. Existence, health, and completion are reported distinctly, and "not reported" is shown where no rule produces a value.
- **W-6 Cleanup is ownership-aware.** Removing intent deletes only what that intent owns. A resource still claimed by another intent, by OCM-io's work agent, or by another writer is not deleted. Where OCM-io's work agent applies, OCM-io's cleanup contract MUST hold: finalizers complete, `AppliedManifestWork` is not orphaned, and delete options are honored.
- **W-7 Moving is safe.** Moving a unit of workload between OCM-io and the resource manager, in either direction, MUST NOT delete, recreate, or interrupt what is applied. Ownership of each applied resource is verified as transferred before the previous owner's record is retired, and that record is retired in a way that leaves the resources in place. A work on a hub is retired by its owner, not by the integration (W-11). The same holds for changing which source a cluster's work agent follows; that change MUST NOT be made while the previous source still holds work for the cluster that the new source would not deliver, such as the works of OCM-io add-ons (K-8).
- **W-8 Nothing is handed to a hub.** For intent held by the resource manager, the management plane MUST NOT create a `ManifestWork`, or any other object on a hub, to deliver it, to track it, or to report it. Per-target delivery records live in the management datastore only.
- **W-9 Limits are respected.** Size and count limits of the receiving side MUST be validated before delivery. Intent that exceeds them is rejected or split, never delivered in part without saying so.
- **W-10 Per-target outcome.** The outcome of each delivery is observable per target, with the reason for a failure. One failing cluster MUST NOT block or distort what is reported for others.
- **W-11 Other writers' work is theirs.** A `ManifestWork` or `ManifestWorkReplicaSet` that the integration did not create is externally owned. The integration MUST NOT edit it, delete it, or take it over. Its workload comes to the resource manager only when its owner moves it (W-7).
- **W-12 A fan-out object is not a fulfillment.** Where a `ManifestWorkReplicaSet` is offered in driven mode, each difference between its semantics and the resource manager's rollout MUST be declared (K-5). Equivalence is never implied.
- **W-13 No conflicting instructions.** Two intents MUST NOT instruct different content for the same resource on the same cluster. The conflict is detected before apply.
- **W-14 Conflicts are surfaced, not forced.** An apply that another field manager would lose MUST fail visibly and name that manager where the API exposes it. The integration MUST NOT force it. Retry behavior MUST be declared, including a retry that has no cap.
- **W-15 Both systems on one cluster.** OCM-io and the resource manager MAY deliver to the same cluster at the same time. Each applied resource belongs to one of them. The resource manager MUST NOT take over a resource that OCM-io's work agent, a GitOps agent, or anyone else applied (W-14). A release MUST state which settings of an OCM-io work let that work take over a resource the resource manager applied, and which make it fail visibly instead.
- **W-16 Applied state is watched.** The management plane MUST notice when a resource it delivered is changed, taken over, or removed by another writer, MUST report it per target, and MUST NOT go on reporting that delivery as in place. Whether it restores the resource is a declared policy of the intent, and restoring never forces (W-14).
- **W-17 Withdrawing intent offers a choice.** Deleting intent MUST offer both removing what it applied and leaving it in place. Leaving it in place is what a move away from the resource manager uses (W-7).

---

## 13. Tenancy and quality of service (T)

Traces to MO-2. These requirements bind from the first phase. A deployment whose resource manager does not yet enforce tenant isolation meets this section only by T-7: it is provider-only, and it says so.

- **T-1 A tenancy model of its own.** The management API MUST have its own tenancy model: a provider, tenants, and a workspace hierarchy within each tenant. A target belongs to exactly one workspace. Each tenant manages its own role assignments and MAY have its own identity configuration.
- **T-2 Hard isolation.** One tenant MUST NOT observe or affect another tenant's intent, targets, inventory, names, errors, or history, through any API, projection, or status surface.
- **T-3 Declared mapping.** How OCM-io's scoping maps to workspaces MUST be declared, for example a hub connection to a tenant or a `ManagedClusterSet` to a workspace. The default is no mapping. A mapping MUST NOT give a principal more access than the source granted.
- **T-4 Independent authorization.** Management-plane authorization and OCM-io authorization are evaluated independently. Passing one MUST NOT imply passing the other, and the integration MUST NOT become a confused deputy between them.
- **T-5 Quality of service.** Per-tenant limits on request rate, concurrent operations, and stored records MUST prevent one tenant from exhausting API, orchestration, or datastore capacity for others. Behavior under overload MUST be declared (N-8).
- **T-6 Isolation in depth.** Tenant isolation MUST be enforced in the API layer. It SHOULD also be enforced in the datastore where the backend supports it, for example with row-level security.
- **T-7 No tenant claim before enforcement.** Until tenant isolation is enforced, visibility is provider-only and reported as such. Tenant-scoped visibility MUST NOT be claimed.
- **T-8 Consolidation keeps separation.** Where separate hubs separated tenants, connecting those hubs to one management plane MUST preserve that separation unless an operator explicitly merges them.
- **T-9 A hub is not offered to tenants by default.** A hub that is itself a managed cluster is offered to a consumer tenant only by an explicit provider decision (X-9).
- **T-10 Internet-facing by design.** The management API MUST be operable as an internet-facing, multitenant endpoint: authenticated before any work is done, tenant-scoped, rate-limited, and audited.
- **T-11 Sharing is explicit.** A resource that more than one tenant uses is owned by the provider and offered to each consumer tenant by an explicit grant. Nothing is shared between tenants by default.

---

## 14. Security, credentials, and provenance (S)

Traces to MO-3.

- **S-1 Standard authentication.** The standalone API authenticates people and workloads with standard identity providers. Using it MUST NOT require a Kubernetes service account.
- **S-2 Attribution.** Every accepted management change MUST record the authenticated principal, the time, the identity of the content, and the affected scope. The record is durable and queryable (IS-7).
- **S-3 Credential presentation is declared.** Each delivery declares whose credential applies at the target: the requesting user's, a delegated workload identity's, or the platform's. The identity the target sees is reported.
- **S-4 No stored user tokens.** A user token used for passthrough MUST NOT be persisted in the datastore, in workflow history, in logs, or in any Kubernetes object. When the token is missing or expired, the delivery pauses and waits for re-authorization.
- **S-5 Pause is not bypass.** Resuming a paused delivery MUST NOT override the target's policy or let older work overtake newer intent (W-3).
- **S-6 Verification locus is declared.** Each delivery path declares where authorization evidence is verified: at the target, by the platform before apply, or nowhere. Verification by the resource manager MUST NOT be described as protection from a compromised resource manager. Where OCM-io's work agent applies, the end user is recorded by the management plane and not enforced at the target, and the path MUST be reported that way.
- **S-7 Evidence is bound and never synthesized.** Evidence binds the exact content delivered to the principal that authorized it. Where the integration re-expresses intent in another form, it keeps the original evidence identity. It MUST NOT re-sign, and MUST NOT synthesize evidence from OCM-io status.
- **S-8 Observed facts are not attested.** A fact read from OCM-io carries its source and read time. It MUST NOT be presented as verified, and it MUST NOT inherit the evidence of a delivery that happened to carry it.
- **S-9 Trust state stays in the management plane.** Trust anchors, signer enrollments, and verification policy MUST NOT be projected into OCM-io objects, and an OCM-io object MUST NOT be treated as a trust root.
- **S-10 Least privilege on hubs.** The integration's credential on a hub is read-only, limited to the kinds in scope, and created by the hub's administrator. By default the integration writes nothing to a hub. A deployment MAY grant it the right to enable enrollment for selected clusters (EN-4) and, where a Kubernetes-facing surface is on that hub, to write the status of driving objects and the selected projections, each only in the selected scopes. Hub cluster-admin MUST NOT be required.
- **S-11 No secrets in compatibility objects.** Credentials MUST NOT be placed in OCM-io custom resources, annotations, driving objects, or projections. A credential for an enrolled cluster rests on a hub only where an operator selects an enrollment route that works that way, and the release declares it.
- **S-12 Per-cluster agent identity.** An agent that connects to the management plane authenticates as its own cluster and can read and write only that cluster's records.
- **S-13 Threat model.** Each released component MUST have a threat model that covers its assets, trust boundaries, and credentials.
- **S-14 Fine-grained authorization.** Every request to the management API MUST be authorized against permissions scoped by workspace, resource type, and operation, so that a principal holds exactly what its role needs. A refusal MUST NOT reveal the existence of anything the caller cannot see (T-2). Grants, and changes to them, are recorded in history (IS-7).
- **S-15 Reduced standing privilege.** The management plane SHOULD NOT need a standing administrative credential for a target in order to deliver to it. In the direct form it holds one credential for each enrolled cluster, with the rights that cluster granted (EN-7). In the agent form it holds none, and the credential stays on the cluster. Where a delivery presents the requesting user's credential or a delegated identity (S-3), the platform's own rights on that target MUST be limited to what the delivery path declares (W-2). The standing credentials that the management plane holds MUST be reported per target.
- **S-16 Use of the platform's credential is a declared rule.** Where a delivery applies with the management plane's own credential for a cluster, the rule that permits it MUST be declared per target: verified signed intent, or an explicit grant recorded with attribution (S-2). A delivery MUST NOT fall back to the platform's credential silently when the requester's own credential is refused.

---

## 15. Inventory, search, and history (IS)

Traces to MO-4.

- **IS-1 Cluster inventory.** For every cluster in scope the management plane holds identity, membership state, enrollment state, labels, cluster sets, claims, and the facts later aspects add. Each value carries its source and the time it was last confirmed.
- **IS-2 Resource inventory.** Resources on enrolled clusters are indexed into the management plane under declared schemas, through the cluster's access form. What is indexed is selectable per cluster, so that a large cluster, or a hub, need not be indexed whole. A cluster that is observed and not enrolled has hub-held facts only, and says so.
- **IS-3 One search.** One query API covers every observed and every enrolled cluster, whichever hub it came from or none. It filters by type, labels, fields, and relationships, and paginates with a deterministic order.
- **IS-4 Relational questions.** Questions that join records, such as which clusters in a tenant are affected by an intent, MUST be answerable from the management API.
- **IS-5 Tenant-scoped results.** A query returns only what the caller is authorized to see (T-2).
- **IS-6 Durable inventory.** Inventory survives restart. After an outage it catches up incrementally under a declared resynchronization behavior. It is not an ephemeral cache that must be rebuilt from nothing. Records of a cluster that was withdrawn, replaced, or deleted are removed or marked as past, and MUST NOT be returned as current (I-4).
- **IS-7 History of change.** Every change to management state (intent, placement, membership, tenancy and access, cutover) is kept as an append-only record of who changed what, when, from which source, and with what outcome.
- **IS-8 History of observation.** Changes in observed state are retained under a declared retention so that trends can be shown. High-churn data is bounded.
- **IS-9 Retention.** Retention and erasure policies MUST be declared. History outlives the deletion of its subject for the retention period.
- **IS-10 Honest about the actor.** For a change that originated on a hub, the record names the OCM-io object, its version, and the time it was observed. The actor is recorded only where OCM-io exposes one. Otherwise the record states that no actor is available on that path.

---

## 16. Reliability, availability, and recovery (R)

Traces to MO-5.

- **R-1 Declared recovery objectives.** Each supported topology MUST declare a recovery point objective and a recovery time objective for the management plane, and both MUST be verified (V-6).
- **R-2 Restore from the database.** Management-plane state MUST be fully restorable from a datastore backup and the deployment's configuration, by a documented and tested procedure. Restoring it MUST NOT depend on backing up Kubernetes objects.
- **R-3 Point-in-time recovery.** Topologies on PostgreSQL MUST support recovery to a chosen point in time.
- **R-4 Restore is safe.** After a restore to an earlier point, the management plane reconciles with hubs, enrolled clusters, and agents. Older restored intent MUST NOT silently overwrite newer applied state: each divergence is reported and an operator decides. An enrolled cluster whose restored credential no longer works is reported as needing enrollment (EN-8).
- **R-5 Static stability.** While the management plane or its datastore is unavailable, managed clusters keep running and agents keep the last applied state. No workload is removed because the management plane is unreachable.
- **R-6 No single point of failure.** In a high-availability topology, the loss of one node or one zone of the API tier or the datastore leaves the service running or fails over within the declared time.
- **R-7 Regional recovery.** A production release MUST support a topology that recovers in another region from replicated or backed-up data within its declared objectives. Enrolled clusters stay enrolled, and agents re-attach without re-registration.
- **R-8 Upgrades are independent.** Upgrading the management plane MUST NOT require upgrading hubs or managed clusters, and the reverse, within the declared skew (K-11). Upgrading a Kubernetes cluster that hosts the management plane MUST NOT interrupt management for longer than the declared failover time.
- **R-9 Convergence.** The management plane converges after restart, partition, or a long outage, without relying on an event history.
- **R-10 Idempotency.** Every operation across the boundary is safe under at-least-once delivery, duplicates, and replay.
- **R-11 Fault isolation.** A failure is contained to the target, hub connection, or tenant where it occurs. An unreachable hub does not stop delivery to the clusters enrolled from it (EN-10).
- **R-12 Bounded behavior.** Queues and memory are bounded. Recovery MUST NOT flood a hub API, the resource manager, or the datastore.
- **R-13 Neither system depends on the integration.** A failure or removal of the integration MUST NOT break OCM-io for scopes in native mode, or the resource manager's other functions.
- **R-14 Honest small topologies.** A single process with an embedded database is a single point of failure. A release MUST say so and MUST NOT attach recovery objectives to it beyond restoring from a backup.

---

## 17. Scale and performance (N)

Traces to MO-1.

| Profile | Managed clusters | Purpose |
|---|---|---|
| Evaluation | up to 50 | One process with an embedded database |
| Standard | 2,000 | The largest profile in the resource manager's own design assumptions |
| Service provider | 6,400 | The minimum a production release validates before it claims service-provider scale |
| Design target | 10,000 | What the data model and protocols are sized for |

- **N-1 Scale is validated, not asserted.** The design target is 10,000 managed clusters in one management plane. A release MUST state the profile it validated, and it MAY claim service-provider scale only after validating at 6,400 or more. A figure beyond the validated profile is an extrapolation and MUST be labeled as one.
- **N-2 Hub object budget.** The integration's steady-state cost on a hub MUST be declared in objects in total, per managed cluster, and per intent. Per intent it MUST be zero in every phase (W-8). Per cluster it MUST be zero for observation. For enrollment it is the declared cost of the route the hub's administrator chose: none for a route that uses no hub object, and for a route through OCM-io's add-on mechanism the objects that mechanism keeps while the add-on is enabled for that cluster (EN-5). Driving objects that users create and projections that an operator selects are counted separately (K-12).
- **N-3 Capacity model.** A release MUST publish records and bytes per cluster, per intent, per delivery, and per inventory item, so that datastore capacity can be planned without reference to any hub's etcd.
- **N-4 Bounded hub load.** Load on a hub API MUST be bounded and declared. Per-object polling and repeated full lists MUST NOT be required for correctness.
- **N-5 Bounded write amplification.** Status churn MUST NOT create intent versions or history of change, and one change on a hub produces a bounded number of datastore writes.
- **N-6 Latency budgets.** Each profile declares its budgets. The initial budgets for the Alongside phase at the Standard profile are: a change on a hub is visible in the management plane within 60 seconds at the 95th percentile, and full convergence after an outage completes within 10 minutes.
- **N-7 Fan-out.** One intent that selects every cluster in the profile, and one cluster that is selected by many intents, MUST both remain correct.
- **N-8 Overload.** Under overload the management plane sheds observation before intent and deletion, and reports that it is shedding.
- **N-9 Small footprint.** The Evaluation profile declares its resource ceiling and needs no service other than the one process (A-6).
- **N-10 Room to scale out.** The data model MUST carry tenant and hub-connection partition keys so that reads can be served from replicas and writes can later be partitioned. The Standard profile MUST NOT depend on either.
- **N-11 Cluster object budget.** What enrollment adds to a managed cluster MUST be declared and reported per cluster (EN-2). On the direct and agent paths a delivery MUST add nothing to the cluster beyond the workload's own objects: no custom resource records the delivery there.
- **N-12 Scale is stated per access form.** A release states which access form each validated profile used, and what the management plane holds open for each enrolled cluster in that form. The design target MUST NOT depend on the management plane itself holding every watch on every cluster (EN-3).

---

## 18. Lifecycle and operations (U)

Traces to the decision in section 1.1 and to MO-5.

- **U-1 Install is inert.** Installation creates only the management plane's own records and contacts no hub until a hub connection is configured (AD-1).
- **U-2 Rolling upgrades.** Upgrades of the management plane and the integration are rolling. Operations in flight across the boundary survive the upgrade or are safely retried.
- **U-3 Removal.** Removing the integration stops its effects and leaves both sides valid, with no OCM-io object left invalid, blocked on a finalizer, or orphaned. Workloads the resource manager delivered stay on their clusters. Leaving records in place is the default. Deleting the integration's own records, and removing the access piece from each cluster (EN-9), are separate, explicit operations.
- **U-4 Explain before acting.** Enrollment, every move, and every cutover MUST offer a dry run that shows what would be added, what would change, and what would be approximated or lost.
- **U-5 Backfill.** Observing and enrolling existing clusters MUST NOT rewrite OCM-io objects or need a maintenance window.
- **U-6 Hub retirement.** A documented procedure retires a hub. It verifies first that every cluster that is to stay managed is enrolled, that every workload that is to stay has been moved, and that nothing else still depends on the hub.
- **U-7 Runbooks.** Backup, restore, failover, enrollment, withdrawal, moving a workload, cutover, rollback, and hub retirement each have a documented procedure that the release tests (V-6, V-7, V-14).
- **U-8 Configuration is declarative.** The integration's configuration, scopes, and mappings are declarative records. Changing them is an operation with a defined order, not a flag whose effect depends on restart order.
- **U-9 Data portability.** A deployment SHOULD be able to move its management state from the embedded database to PostgreSQL by a documented export and import.
- **U-10 Set-up is a short sequence.** Connecting a hub, seeing its clusters, enrolling the chosen ones, and withdrawing them are operations of the management API, offered in the console and on the command line with the same result. Whatever a hub's or a cluster's administrator has to apply on their side is generated for them and shown before it is used.

---

## 19. Observability and verification (O, V)

Traces to every motivation: none of them may be claimed without evidence (AD-8, Z-8).

- **O-1 Status surface.** For each hub connection, cluster, and aspect the management plane reports the phase, system of record, compatibility mode, enrollment state, access form, freshness, and errors.
- **O-2 Metrics.** Metrics cover lag, error rate, conflicts, queue depth, write rate, and shed work, labeled by hub connection and scope.
- **O-3 Correlated logs.** Logs and events carry the identifiers of the OCM-io object and the management-plane record they concern.
- **O-4 Health is not coverage.** Reports distinguish "integration healthy" from "converged" and from "covered".
- **O-5 Audit of the integration.** Every write the integration makes is auditable with its actor, its cause, and the record it changed.
- **O-6 Capacity signals.** The management plane reports datastore size and growth against the capacity model (N-3), and the hub and cluster objects that the integration accounts for (K-12, N-11).

- **V-1 Traceability.** Every requirement has at least one automated test or an explicitly accepted manual check, traceable by identifier.
- **V-2 Real systems.** Anything that touches the boundary is tested against a real OCM-io hub and managed cluster and a real resource manager, not mocks alone.
- **V-3 Compatibility conformance.** Tests prove that OCM-io is unmodified, that objects in native mode are never written by the integration, that the klusterlet on an enrolled cluster is unchanged and keeps working with its hub (K-2), and that an unmodified klusterlet works against every agent protocol the release serves (K-3).
- **V-4 Chaos.** Tests cover the integration, a hub, the resource manager, and the datastore each being down; an unreachable cluster; duplicates; clock skew; throttling; admission rejection; and conflict.
- **V-5 Scale.** Load tests run at the declared profile and in the declared access form (N-12), record the latency budgets (N-6), and count hub and cluster objects against the budgets (N-2, N-11).
- **V-6 Recovery drills.** Backup and restore, point-in-time recovery, datastore failover, and regional recovery are exercised and measured against the declared objectives (R-1 to R-7).
- **V-7 Moves, cutover, and rollback.** Moving a workload in each direction, and each aspect's cutover and rollback, are tested, including the proof that no workload is deleted or interrupted and that no cluster is reported unreachable (W-7, X-6, M-7).
- **V-8 Tenancy.** Tests prove isolation between tenants on every surface, including resources shared through the provider, and prove that per-tenant limits hold under load (T-2, T-5, T-11).
- **V-9 Credentials and attribution.** Tests scan the datastore, workflow history, logs, and Kubernetes objects for stored user tokens (S-4), check that attribution is complete (S-2), and check each path's declared verification locus (S-6). They exercise permissions at every declared scope (S-14) and compare the standing credentials reported for each target with those actually held (S-15). They check that no cluster credential can be read back through a query (EN-8) and that the platform's credential is used only under its declared rule (S-16).
- **V-10 GitOps.** Tests prove that the integration writes no field a GitOps applier owns, that drift is reported and not fought, that a revert reconciles normally, and that pipelines keep applying while the management plane is down (GO-3, GO-5, GO-6, GO-12).
- **V-11 Evaluation.** On a clean machine with no Kubernetes and no database service, the documented steps produce a running management plane (A-3, A-6).
- **V-12 Unsupported inputs.** An unsupported OCM-io version, an unknown version string, or an undeclared combination produces no write and a clear report (C-8).
- **V-13 Lifecycle.** Install, upgrade, and removal are tested from every supported state, including a partially converged one (U-1 to U-3).
- **V-14 Enrollment.** Tests enroll clusters through each route the release offers and count what each adds to the hub and to the cluster (N-2, N-11). They prove that disabling the add-on leaves the cluster enrolled and the hub clean (EN-5), that a false enrollment creates nothing (EN-6), that a lapsed credential is reported and renewed (EN-8), and that withdrawal and removal of the access piece each end access at once (EN-9).
- **V-15 Both systems on one cluster.** Tests deliver to one cluster from OCM-io and from the resource manager at the same time, including onto each other's resources. They prove that nothing is forced, that each conflict and takeover is reported, and that no hub object exists for anything the resource manager delivers (W-8, W-14, W-15, W-16).

---

## 20. Constraints on OCM-io (C)

Traces to the decision in section 1.1.

- **C-1 No fork.** The integration MUST NOT require a fork or private patch of OCM-io, or a change to an existing installation inside the declared baseline. Objects that a hub's administrator chooses to apply through OCM-io's own extension points, such as an add-on that rolls out enrollment (EN-4), are use of OCM-io and not a change to it. The one exception is the agent settings that an explicit registration cutover changes and its rollback restores (M-7, X-3).
- **C-2 No schema change.** The integration MUST NOT change the schema of an existing OCM-io CRD. Any API it adds is additive, separately named, and removable without altering OCM-io objects.
- **C-3 No injected metadata.** The integration MUST NOT add labels, annotations, finalizers, owner references, or fields to objects it does not own, for tracking, correlation, or loop suppression.
- **C-4 No change to what users already have.** For every scope in native mode, the integration MUST NOT require changes to existing manifests, pipelines, or operational procedures, and MUST NOT invalidate them. What a cutover changes for its scope is declared before it is performed (K-10, U-4).
- **C-5 Not the only writer.** The integration MUST NOT assume it is the only writer to a cluster, a namespace, a work object, or a workload. GitOps appliers, OCM-io controllers, add-on agents, and operators may all write.
- **C-6 Nothing is added unasked.** A cluster that is not enrolled has nothing added to it. An enrolled cluster has only its access piece (EN-2), which is opt-in per cluster, declared, and removable. The direct form adds no agent beside the klusterlet, and the agent form is used only where it is chosen (EN-3).
- **C-7 Upstream changes are optional.** No requirement depends on an OCM-io change. An enhancement the integration can use MUST be general, additive, opt-in, and valuable to OCM-io users who never install the resource manager. Its presence is detected and its absence is a declared mode, never a silent difference.
- **C-8 Declared support.** A release declares the OCM-io and resource manager versions, the combinations, and the skew it supports, and fails safe on anything else without corrupting state.

---

## 21. Non-goals (G)

- **G-1** Changing, forking, or patching OCM-io, its CRDs, controllers, klusterlet, or addon-framework.
- **G-2** Forcing existing OCM-io users to migrate, or withdrawing an OCM-io API from users who still depend on it.
- **G-3** Re-implementing the Kubernetes API on a database. The Kubernetes-facing surface is a Kubernetes API, and the management API is not one.
- **G-4** Translating arbitrary CRDs. Only declared kinds and mappings are in scope.
- **G-5** Transactions that span both systems, or a single global serialization point.
- **G-6** Making OCM-io itself multitenant.
- **G-7** Selecting one PostgreSQL high-availability product. The requirement is the outcome; the choice is deferred (OQ-1).
- **G-8** Parity for every concept in both directions. Coverage is chosen, not maximal.
- **G-9** Mapping the resource manager's add-on model one-to-one onto OCM-io's addon-framework.
- **G-10** Specifying policy, application lifecycle, or observability add-ons. They integrate through A-11.
- **G-11** A migration that must happen all at once.
- **G-12** Redesigning OCM-io, or changing the resource manager's architecture. The capabilities the resource manager still has to gain are tracked as gaps in [coverage.md](coverage.md).
- **G-13** Using a hub as the delivery service for the resource manager's intent, or mirroring that intent back to a hub (W-8, GO-7).
- **G-14** Converting existing `ManifestWork` objects into resource manager intent automatically. A workload moves when its owner moves it (W-7, W-11).
