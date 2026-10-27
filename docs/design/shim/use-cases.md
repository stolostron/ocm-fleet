# Use cases

**Status: illustrative, not normative.** Each story shows what a requirement means for a person. Where a story and [requirements.md](requirements.md) disagree, the requirements win. Nothing here is implemented by this directory; the **Today** line in each story says what unmodified OCM-io v1.4.x and the current resource manager already do. Sources for those statements are in [analysis.md](analysis.md).

## 1. How to read a story

Every story has the same parts.

- **Story**: who wants what, and why.
- **Acceptance**: what must be true for the story to be satisfied.
- **Today**: what exists now.
- **Phase**: the earliest phase that can satisfy it (requirements section 4).
- **Requirements**: the identifiers that make it binding.

Section 3 follows one person from a working OCM-io fleet to using the management plane on it. Sections 4 to 12 are grouped by concern. Section 13 lists what must never happen.

## 2. Personas

| Persona | Who they are |
|---|---|
| Fleet operator | Runs management for many clusters, often for many tenants. A managed service provider is the demanding case. |
| Platform operator | Runs the management plane itself: its processes, its database, its upgrades, and its recovery. |
| Tenant administrator | Manages one tenant's workspaces, clusters, and access inside a shared management plane. |
| Security auditor | Investigates who authorized a change and what each delivery path can prove. |
| GitOps engineer | Owns pipelines that apply desired state from Git. |
| OCM-io user | Uses OCM-io APIs, `kubectl`, and `clusteradm` today and wants to keep doing so. |
| Hub administrator | Administers an OCM-io hub and decides what is installed on its managed clusters. |
| Add-on developer | Builds controllers and add-ons, on OCM-io's addon-framework or on the management API. |
| Evaluator | Wants to try the system with as little setup as possible. |

---

## 3. Onboarding an existing fleet

These stories are one journey. Someone already runs a hub with many managed clusters, and it works. They install the management plane, connect the hub, see their clusters, enroll the ones they choose, and then deploy to them, search them, and look at them the management plane's way, while OCM-io carries on beside it.

### UC-O1 Connect my hub and see my clusters

**Story.** As an OCM-io user with a working hub and many managed clusters, I want to install the management plane, connect my hub, and see my clusters in it, so that I gain a second view of my fleet without changing anything I run.

**Acceptance.**
- Connecting needs one read-only credential that the hub's administrator creates. Nothing else is added to the hub, and nothing is added to any cluster.
- Every cluster the hub has accepted appears with its labels, its cluster set, and its state, marked as observed and not enrolled.
- A cluster that leaves the hub leaves the list, and a stale value is shown as stale.

**Today.** The integration does not exist yet. The resource manager's own indexer, pointed at a hub it can reach, already copies the hub's `ManagedCluster` objects into its inventory, with their labels and conditions, and the console's search finds them. They are generic objects there: they are not shown as clusters, nothing ties them to a target, and the copy from a hub that is gone is still returned.

**Phase.** Alongside. **Requirements.** M-1, M-3, EN-12, S-10, AD-1, U-10.

### UC-O2 Enroll the clusters I choose

**Story.** As a hub administrator, I want to choose which of my clusters the management plane may act on, so that it gains access one cluster at a time and only where I allow it.

**Acceptance.**
- A cluster is enrolled only after I act. Seeing a cluster never enrolls it.
- Enrollment adds only the declared access piece. The cluster's klusterlet, its registration, and its add-ons are untouched.
- The management plane checks what an enrolling cluster presents against what my hub records for that cluster, and creates nothing if they disagree.
- An enrolled cluster becomes a target that carries the labels and cluster set it has on my hub.

**Today.** Not available: the resource manager has no way to take in a cluster it did not create. On the test environment, a service account and its binding were applied to a cluster the resource manager had not created, and a token for that account worked from the resource manager's position for listing, watching, and server-side apply. The hub's record of each cluster's API address and certificate authority matched the clusters.

**Phase.** Leading. **Requirements.** EN-1, EN-2, EN-6, M-4, M-10, F-2.

### UC-O3 Roll enrollment out with the tools my hub already has

**Story.** As a hub administrator with hundreds of clusters, I want to enroll them with OCM-io's own add-on mechanism and a `Placement`, so that I do not visit each cluster and I use tools I already operate.

**Acceptance.**
- I apply the add-on once and name clusters or a `Placement`. OCM-io installs the access piece on each selected cluster. OCM-io itself is not changed.
- The hub objects this costs are declared, in total and per cluster.
- I can disable the add-on for a cluster after it is enrolled. The cluster stays enrolled, and the hub then holds nothing for it.
- The management plane needs no write access to my hub.

**Today.** Observed on the test environment with unmodified OCM-io v1.4.0 and a stand-in for the management plane's receiving side. One add-on template and its registration on the hub, enabled for two clusters, enrolled both within 12 seconds. The hub held two objects in total and two for each cluster. After the add-on was disabled for a cluster, the hub held nothing for it, the service account and its binding remained on the cluster, and the credential still worked. Selection by `Placement` was not exercised.

**Phase.** Leading. **Requirements.** EN-4, EN-5, N-2, K-12, C-1, S-10.

### UC-O4 Enroll without adding anything to my hub

**Story.** As a platform operator whose clusters are built from templates or managed from Git, I want to enroll a cluster by applying a manifest to it, so that my hub gains no object at all.

**Acceptance.**
- The manifest is generated for me and shown before I use it.
- It can be applied by any means: by hand, by the pipeline that builds the cluster, or by a GitOps agent on the cluster.
- No hub object exists for the enrollment at any time.

**Today.** Not available. The access piece for the direct form is two built-in Kubernetes objects, so any tool that applies manifests can deliver it.

**Phase.** Leading. **Requirements.** EN-4, N-2, U-10, M-8.

### UC-O5 Deploy to my existing clusters with nothing created on my hub

**Story.** As a fleet operator, I want to deploy a workload to enrolled clusters through the management plane, by name or by label, so that the hub neither delivers it nor stores a record of it.

**Acceptance.**
- One intent is stated once. No `ManifestWork` and no other hub object is created for it.
- Nothing is added to a cluster beyond the workload's own objects.
- The outcome is visible per cluster, with the reason for a failure.
- Placement can use the labels and cluster sets the clusters have on the hub.

**Today.** On a cluster that the resource manager created and that is also registered with the hub, a deployment through the resource manager applied a `ConfigMap` and a `Deployment` with no object created on the hub and no custom resource on the cluster. On a cluster it did not create, the same apply call was made by hand with an enrolled credential and succeeded. The resource manager cannot yet make that call itself, because it has no target for such a cluster. The reason for a failed delivery cannot be read through its API.

**Phase.** Leading. **Requirements.** W-8, N-11, W-10, M-4, F-5, F-13.

### UC-O6 See what runs on the clusters I enrolled

**Story.** As a fleet operator, I want the pods, nodes, namespaces, and other resources of my enrolled clusters in the management plane's inventory, so that I can search my existing fleet from one place.

**Acceptance.**
- Indexing starts when a cluster is enrolled and stops when it is withdrawn.
- I can narrow what is indexed for a large cluster.
- A cluster that is only observed shows the facts the hub holds, and says that it is not indexed.
- Records of a cluster that has left are not returned as current.

**Today.** The resource manager indexes every kind of object on the clusters it created, apart from a short deny list, and its console and query API search them. It keeps the rows of a cluster after the cluster is deleted: on the test environment more than half of the indexed rows belong to clusters that no longer exist, under the names of clusters that do. Its indexer can be narrowed by kind and by namespace, but every target is given the default setting and no API changes it.

**Phase.** Leading. **Requirements.** IS-2, IS-3, IS-6, EN-12.

### UC-O7 Do all of it from one console

**Story.** As an OCM-io user, I want to connect my hub, see my clusters, enroll them, deploy to them, and search them in the management plane's console, so that my existing fleet is used the same way as any other.

**Acceptance.**
- The console offers connecting a hub and enrolling clusters as guided steps, and the same steps exist in the API and on the command line.
- Observed, enrolled, and created clusters appear in the same cluster list, each with its origin and state.
- An enrolled cluster has the same pages as a created one, and also shows what its hub knows about it.

**Today.** The resource manager has a console with pages for clusters, pods, nodes, namespaces, and targets, a search, and a page for extensions that has a place for exactly this kind of step. Its cluster list and cluster page show only clusters of its one built-in provider. Console extensions are built into the console.

**Phase.** Alongside for connecting and seeing; Leading for the rest. **Requirements.** U-10, EN-11, F-13, A-12.

### UC-O8 Decide how much the management plane may do on a cluster

**Story.** As a hub administrator, I want to choose the rights the management plane gets on each cluster, so that enrolling a cluster is not handing over its administration.

**Acceptance.**
- The rights are those of the role I bind in what I apply. The management plane works with less than cluster administrator.
- The management plane reports the rights it holds on each cluster.
- A delivery that needs more is refused and reported as that, for that cluster.

**Today.** For each cluster it creates, the resource manager binds a service account of its own to the cluster-administrator role. OCM-io's work agent holds the built-in `admin` role across a managed cluster.

**Phase.** Leading. **Requirements.** EN-7, S-15, W-10.

### UC-O9 Run both systems on one cluster

**Story.** As an OCM-io user, I want OCM-io to go on delivering what it delivers to a cluster while the management plane delivers other things to the same cluster, so that I can start using the management plane without moving anything.

**Acceptance.**
- Each system delivers its own workloads, and neither disturbs the other's.
- The management plane never takes over a resource that OCM-io, a GitOps agent, or anyone else applied. A conflict is reported and names the other owner.
- If another writer takes over or removes something the management plane delivered, the management plane reports it and does not go on showing the delivery as in place.
- The release says which settings of an OCM-io work would take such a resource over without a conflict.

**Today.** Observed on the test environment. Each system's own deliveries were unaffected by the other. The resource manager's apply onto a resource OCM-io had delivered was refused; it then retried without limit and gave no reason through its API. An OCM-io work that asked for server-side apply without force onto a resource the resource manager had delivered failed and named the resource manager. An OCM-io work with the default update strategy overwrote that resource without warning, and deleting the work then deleted the resource, while the resource manager went on reporting its deployment as active.

**Phase.** Leading. **Requirements.** W-15, W-14, W-16, C-5, F-14.

### UC-O10 Move one workload without interrupting it

**Story.** As the owner of a workload that OCM-io delivers, I want to hand it to the management plane when I choose, and to hand it back, so that I move at my own pace and never bet more than one workload.

**Acceptance.**
- Nothing moves unless I move it.
- During the move both systems hold identical content, and the applied resources keep their identity and keep running.
- After the move the hub holds no object for that workload.
- Moving back is the same operation in the other direction, with the same guarantee.

**Today.** Observed on the test environment. A resource delivered by a `ManifestWork` was also delivered, with the same content, through the resource manager. The work was then deleted with OCM-io's orphan option, and the resource stayed in place, with the same identity, owned by the resource manager alone. The way back did not hold: deleting the resource manager's deployment removed the resource although OCM-io's work also covered it, and OCM-io put it back four and a half minutes later. The resource manager has no delete that leaves resources in place.

**Phase.** Leading. **Requirements.** W-7, X-3, X-4, W-17, W-11.

### UC-O11 Withdraw a cluster

**Story.** As a hub administrator, I want to take a cluster back from the management plane at any time, from the cluster's side if need be, so that enrolling it was never a one-way door.

**Acceptance.**
- Removing the access piece from the cluster ends the management plane's access at once.
- The management plane then reports the cluster as withdrawn, not as failing, and does not enroll it again by itself.
- Workloads stay in place unless I ask for them to be removed.
- The cluster is still registered with my hub, and OCM-io is unaffected.

**Today.** Observed on the test environment: after the service account and its binding were removed from a cluster, a credential that had worked was refused at once, and the hub went on reporting the cluster as available.

**Phase.** Leading. **Requirements.** EN-9, M-5, F-12.

### UC-O12 Enroll a cluster that cannot be reached from outside

**Story.** As a fleet operator whose clusters only make outbound connections, I want the management plane's own agent to connect out from the cluster, so that enrollment does not need a way in.

**Acceptance.**
- The agent is rolled out through the same routes as any access piece, and is opt-in per cluster.
- The credential it acts with stays on the cluster. The management plane holds none for that cluster.
- It reaches only its own cluster's records.
- A cluster can change from the direct form to the agent form without interrupting its workloads.

**Today.** OCM-io's klusterlet connects out to its hub, so OCM-io reaches such clusters. The resource manager's agent is designed and not built, so the resource manager reaches only clusters whose API it can call.

**Phase.** Leading. **Requirements.** EN-3, S-12, S-15, N-12.

### UC-O13 Keep working when the hub is away

**Story.** As a fleet operator, I want the management plane to keep delivering to and indexing my enrolled clusters while their hub is unreachable, so that the hub is not in the path of what the management plane does.

**Acceptance.**
- Delivery, inventory, and status for an enrolled cluster continue.
- What comes from the hub, such as labels, stays at its last known value and is marked stale.

**Today.** On the test environment nothing in the resource manager's delivery to a cluster involved the hub. No hub was taken away to show it.

**Phase.** Leading. **Requirements.** EN-10, R-11, M-3.

---

## 4. Scale

### UC-S1 Plan capacity independently of hub storage

**Story.** As a fleet operator, I want to plan management-data capacity independently of hub control-plane storage, so that fleet growth does not depend on expanding the hub's etcd workload.

**Acceptance.**
- A published capacity model gives records and bytes per cluster, per intent, per delivery, and per inventory item.
- Adding a cluster grows the management datastore and adds no hub object beyond what its enrollment route declares.
- The management plane reports datastore size and growth against the model.

**Today.** In OCM-io every per-cluster record is a hub object in etcd. The resource manager keeps its state in SQLite or PostgreSQL, and publishes no capacity model. A first measurement of its SQLite store found records of one to three kilobytes each, and found orchestration history, which is never removed, to be the largest consumer.

**Phase.** Alongside. **Requirements.** F-5, N-2, N-3, O-6.

### UC-S2 Grow past one hub without adding hubs

**Story.** As a fleet operator, I want one management plane to reach 6,400 clusters and be designed for 10,000, so that I stop splitting my fleet across hubs to stay inside a control plane's limits.

**Acceptance.**
- A release states the profile it validated and the access form it used, and claims service-provider scale only after a test at 6,400 clusters or more.
- No per-cluster record of anything the resource manager delivers is stored in a hub.

**Today.** A hub holds nine objects for each registered cluster before any work or add-on, and one more for every `ManifestWork`. A deployment through the resource manager adds none. The resource manager has an orchestration stress test and no fleet-scale validation, and it holds every watch on every cluster itself.

**Phase.** Leading. **Requirements.** N-1, N-2, N-12, W-8, V-5.

### UC-S3 Fan one intent out to every cluster

**Story.** As a fleet operator, I want to state an intent once and have it reach every selected cluster, so that the number of stored objects does not multiply with the number of clusters.

**Acceptance.**
- One intent record exists. Per-cluster delivery records are rows in the management datastore.
- The outcome is visible per cluster, and one failing cluster does not hide the others.

**Today.** OCM-io's `ManifestWorkReplicaSet` creates one `ManifestWork` on the hub for each selected cluster. The resource manager keeps one fulfillment and one delivery record per target.

**Phase.** Leading. **Requirements.** GO-7, W-8, W-10, N-7.

### UC-S4 Bring several hubs under one management API

**Story.** As a fleet operator who already runs several hubs, I want to see and search the clusters of all of them through one API, so that I manage one fleet instead of several.

**Acceptance.**
- Each hub is a separate hub connection with its own scope, credentials, and status.
- Two clusters with the same name on different hubs stay two clusters.
- Losing one hub marks only its clusters stale.

**Today.** Each OCM-io hub is its own API. The integration does not exist yet.

**Phase.** Alongside. **Requirements.** M-1, M-2, I-2, F-1, R-11.

### UC-S5 Run small with the same software

**Story.** As an operator of five clusters, I want the same management plane a service provider runs, without the service provider's footprint, so that I do not adopt a different product to start small.

**Acceptance.**
- One process with an embedded database is a supported deployment with declared limits.
- Nothing in the core requires a second replica or an external service.

**Today.** The resource manager treats a single process with embedded SQLite as a design invariant, and ships an all-in-one container.

**Phase.** Alongside. **Requirements.** AD-5, A-6, A-9, N-9.

---

## 5. Reliability and recovery

### UC-R1 Restore management services from a database backup

**Story.** As a platform operator, I want documented database backup and recovery procedures, so that I can restore management services against defined recovery targets.

**Acceptance.**
- A documented, tested procedure restores the management plane from a datastore backup and its configuration.
- The topology declares a recovery point objective and a recovery time objective, and a drill measures both.
- No Kubernetes object needs to be backed up to restore management-plane state.

**Today.** OCM-io's management state is Kubernetes objects in the hub's etcd and is backed up with the hub cluster. The resource manager has no backup tooling or documented procedure; the standard tools of its database apply.

**Phase.** Alongside. **Requirements.** R-1, R-2, U-7, V-6, F-9.

### UC-R2 Survive the loss of a database node

**Story.** As a platform operator, I want the management plane to keep serving, or fail over within a declared time, when one database node or one replica fails, so that a single failure is not an outage.

**Acceptance.**
- The high-availability topology has no single point of failure in the API tier or the datastore.
- Failover time is declared and measured.

**Today.** The resource manager connects to PostgreSQL through a connection URL. Running more than one replica is designed and has not been exercised, and no failover behavior has been tested.

**Phase.** Alongside. **Requirements.** R-6, A-5, A-9, OQ-1.

### UC-R3 Recover in another region

**Story.** As a platform operator, I want to bring the management plane up in a second region from replicated or backed-up data, so that losing a region does not end fleet management.

**Acceptance.**
- Recovery completes within the declared objectives.
- Enrolled clusters stay enrolled, and agents re-attach to the recovered management plane without re-registration.
- After recovery, older intent does not silently overwrite newer applied state.

**Today.** Not available in either system as a documented procedure.

**Phase.** Alongside for the management plane; Leading for enrolled clusters. **Requirements.** R-7, R-4, R-1, V-6.

### UC-R4 Upgrade the hosting cluster without interrupting management

**Story.** As a platform operator, I want to upgrade the Kubernetes cluster that hosts management components without interrupting fleet management, so that control-plane upgrades and management-plane availability are decoupled.

**Acceptance.**
- The management plane is not an extension of the hosting cluster's API, so an API server restart does not make the management API unavailable.
- Any interruption stays within the declared failover time.

**Today.** In OCM-io the hub's API server is the management API, so upgrading the hub's control plane is a management-plane event. The resource manager is a separate server that can run outside any cluster.

**Phase.** Alongside. **Requirements.** A-1, R-8, R-6.

### UC-R5 Clusters keep running while the management plane is down

**Story.** As a fleet operator, I want managed clusters to keep their workloads when the management plane or its database is unavailable, so that a management outage is not a workload outage.

**Acceptance.**
- No workload is removed because the management plane cannot be reached.
- When the management plane returns, it converges without an event history.
- Something deleted on one side during the outage is recognized afterward. It is not resurrected and not left as an orphan.

**Today.** OCM-io's work agent keeps applied resources while the hub is unreachable. The resource manager's delivery leaves applied resources in place.

**Phase.** Alongside. **Requirements.** R-5, R-9, R-13, I-9, V-4.

### UC-R6 Go back to a point in time

**Story.** As a platform operator, I want to restore the management datastore to a moment before a bad change, so that I can undo damage without rebuilding the fleet.

**Acceptance.**
- PostgreSQL topologies support recovery to a chosen point in time.
- After the restore, every divergence between restored intent and what clusters currently run is reported, and an operator decides each one.

**Today.** Point-in-time recovery is a PostgreSQL capability. The resource manager has not designed the reconciliation that must follow a restore.

**Phase.** Alongside. **Requirements.** R-3, R-4, W-3.

### UC-R7 Upgrade the management plane without touching hubs or agents

**Story.** As a platform operator, I want to upgrade the management plane on its own schedule, so that it is not tied to hub or cluster upgrades.

**Acceptance.**
- The supported version skew is declared.
- Operations in flight across the boundary survive the upgrade or are retried safely.

**Today.** The two systems are separate processes with separate releases. No skew is declared, because no integration exists.

**Phase.** Alongside. **Requirements.** R-8, K-11, U-2.

### UC-R8 See how far behind the management plane is

**Story.** As a platform operator, I want lag, errors, and coverage reported for each hub connection, so that I notice a problem before the people who depend on the fleet do.

**Acceptance.**
- For each hub connection and cluster, the management plane reports the phase, system of record, enrollment state, access form, freshness, and errors.
- "Healthy", "converged", and "covered" are reported as three different things.
- Logs and metrics carry the identifiers of both the OCM-io object and the management-plane record.

**Today.** The resource manager writes structured logs of its orchestration and deliveries and exposes no metrics. Nothing reports on a hub, because no integration exists.

**Phase.** Alongside. **Requirements.** O-1, O-2, O-3, O-4, M-3.

---

## 6. Evaluation and development

### UC-E1 Try it locally without a database service

**Story.** As an evaluator, I want to run the system locally without provisioning a database service, so that I can try it with minimal setup.

**Acceptance.**
- On a clean machine with no Kubernetes and no database service, documented steps produce a running management plane.
- The embedded database offers the same API and behavior, with declared limits.

**Today.** This works. The resource manager's all-in-one container starts the API, the console, and an identity provider with an embedded SQLite database, with no Kubernetes involved.

**Phase.** Alongside. **Requirements.** A-3, A-6, F-10, V-11.

### UC-E2 Evaluate against an existing hub without changing it

**Story.** As an OCM-io user, I want to point a local management plane at my hub and see my clusters in it, so that I can judge the result before committing to anything.

**Acceptance.**
- Connecting a hub changes nothing on it or on its clusters. Clusters are observed only for the scope I select, and none is enrolled.
- A dry run shows what observing the scope would record and what enrolling a cluster would add.
- Removing the management plane leaves the hub exactly as it was.

**Today.** The integration does not exist yet. The design's test environment has an OCM-io v1.4.0 hub and a resource manager side by side. The only link between them is the resource manager's general indexer, which copies the hub's objects because the resource manager created that cluster.

**Phase.** Alongside. **Requirements.** AD-1, U-1, U-4, U-3.

### UC-E3 Start managing clusters without already having one

**Story.** As a platform operator, I want to start a management plane on a workstation and use it to create or enroll my first cluster, so that managing clusters does not require a cluster first.

**Acceptance.**
- The management plane starts with no Kubernetes cluster present.
- It can create or enroll a first cluster and manage it.

**Today.** Creating a first cluster works in the resource manager. In the design's test environment, a resource manager in a container created the two local clusters that later became the OCM-io hub and its managed cluster. Enrolling a cluster that already exists is not available. An OCM-io hub needs a Kubernetes cluster before it can exist.

**Phase.** Standalone for OCM-io users; available in the resource manager now. **Requirements.** A-3, A-2, M-8.

### UC-E4 Keep the controller model for an add-on

**Story.** As an add-on developer, I want to keep writing reconcilers with the controller libraries I know, so that moving state out of the hub does not mean rewriting my add-on.

**Acceptance.**
- An OCM-io add-on keeps working, unchanged, wherever a hub API remains.
- An add-on that moves to the management API can keep its reconcile loop, and gains tenancy, attribution, and history.

**Today.** OCM-io add-ons work as they always have. A resource manager proof of concept runs standard reconcilers against delivery targets in place of a Kubernetes API.

**Phase.** Alongside for existing add-ons. **Requirements.** K-8, K-9, A-11.

### UC-E5 Integrate a service at the management API

**Story.** As a service developer, I want to build on a REST and gRPC management API with its own resource model, so that my service is multitenant and auditable without my building that myself.

**Acceptance.**
- My service's resources live under the management plane's tenancy model and appear in its history.
- I need no Kubernetes credentials.
- The API version I build on is declared stable.

**Today.** The resource manager serves typed extension APIs that add-ons define, at an alpha API version. Tenancy and history are designed and not implemented.

**Phase.** Leading. **Requirements.** A-1, A-11, T-1, IS-7.

### UC-E6 Test against an embedded database

**Story.** As a developer, I want continuous integration to run the full API against an embedded database, so that tests are fast and need no database service.

**Acceptance.**
- The same tests pass on both backends, or the difference is a declared one.

**Today.** The resource manager supports SQLite and PostgreSQL behind one persistence interface.

**Phase.** Alongside. **Requirements.** A-6, A-7.

### UC-E7 Move from evaluation to production

**Story.** As an evaluator who decides to adopt, I want to carry what I set up into a production datastore, so that I do not start again.

**Acceptance.**
- A documented export and import moves management state from the embedded database to PostgreSQL.

**Today.** No export or import exists.

**Phase.** Alongside. **Requirements.** U-9, A-7.

---

## 7. Tenancy

### UC-T1 Onboard a tenant without creating a hub

**Story.** As a fleet operator, I want to onboard a new tenant as a record in the management plane, so that tenancy does not cost a control plane per tenant.

**Acceptance.**
- A tenant with its workspace tree is created through the management API.
- The tenant's clusters, intent, and history are isolated from every other tenant.

**Today.** OCM-io separates tenants with separate hubs, or with namespaces and RBAC inside one hub. The resource manager's tenancy model is designed and not implemented: its schema has no tenant or workspace.

**Phase.** Alongside for what is observed; Leading for intent and delivery. **Requirements.** T-1, T-2, F-6.

### UC-T2 See only what is mine

**Story.** As a tenant administrator, I want every list, search, error, and history entry to contain only my tenant's data, so that sharing a management plane is safe.

**Acceptance.**
- No API, projection, or status surface shows another tenant's names, errors, or records.
- Until isolation is enforced, the management plane says visibility is provider-only.

**Today.** The resource manager authenticates callers and does not yet authorize them, so every authenticated caller sees everything.

**Phase.** Alongside for what is observed; Leading for intent and delivery. **Requirements.** T-2, T-6, T-7, IS-5.

### UC-T3 One tenant's burst does not starve the others

**Story.** As a fleet operator, I want per-tenant limits on request rate, concurrent operations, and stored records, so that one tenant cannot exhaust the plane for everyone.

**Acceptance.**
- Limits hold under load, and a tenant that exceeds them is told so.
- Overload behavior is declared.

**Today.** A hub has one API server and one etcd shared by everything on it. The resource manager has no per-tenant limits.

**Phase.** Alongside. **Requirements.** T-5, T-10, N-8, V-8.

### UC-T4 Map an existing hub to a tenant

**Story.** As a fleet operator whose hubs each serve one tenant, I want to declare that a hub connection, or a cluster set inside it, belongs to a tenant, so that consolidation keeps the separation I already have.

**Acceptance.**
- The mapping is declared, and the default is no mapping.
- Nobody gains access they did not have on the hub.

**Today.** OCM-io scopes access with `ManagedClusterSet`, cluster namespaces, and RBAC. No mapping exists.

**Phase.** Alongside. **Requirements.** T-3, T-4, T-8, U-8.

### UC-T5 Operate across tenants with a record

**Story.** As a fleet operator, I want my own actions across tenants to be recorded like anyone's, so that provider access is accountable.

**Acceptance.**
- Every provider action appears in history with the principal that performed it.

**Today.** No history exists in the resource manager.

**Phase.** Alongside. **Requirements.** S-2, IS-7, O-5, T-10.

### UC-T6 Offer a shared resource to tenants explicitly

**Story.** As a fleet operator, I want a resource that several tenants need, such as a shared cluster, to belong to the provider and be offered to each tenant by an explicit grant, so that sharing never weakens isolation.

**Acceptance.**
- Nothing is shared between tenants by default.
- A tenant sees a shared resource only after the provider grants it, and sees nothing of the other tenants that use it.

**Today.** The resource manager's design has one provider tenant that governs what is shared. It is not implemented.

**Phase.** Leading. **Requirements.** T-11, T-9, T-2.

---

## 8. Attribution and security

### UC-A1 Find out who changed what

**Story.** As a security auditor, I want to ask who changed a cluster's workload, what they changed, and when, so that an investigation ends with a person or a workload identity and not with a controller's service account.

**Acceptance.**
- Every accepted change records principal, time, content identity, and affected clusters, and the record can be queried.
- For a change that came from a hub, the record says honestly whether an actor is available.

**Today.** On an OCM-io managed cluster, an applied object records the work agent as its field manager and no user. The resource manager stores the caller of the current fulfillment and keeps no history.

**Phase.** Alongside for what the management plane observes and does. **Requirements.** S-2, IS-7, IS-10, F-7.

### UC-A2 Deploy as myself without my token being stored

**Story.** As a user, I want a delivery to reach the cluster with my own credential, and I want that credential never to be written down, so that the cluster sees me and nobody can replay me.

**Acceptance.**
- The token is not in the datastore, the workflow history, the logs, or any Kubernetes object.
- When the token expires, the delivery pauses, and an authorized user resumes it with a fresh credential.

**Today.** The resource manager passes the caller's token through to Kubernetes targets and can pause and resume a deployment. It also persists that token with the fulfillment, which the design says it must not. A cluster accepts the token only if its API server trusts the management plane's identity provider. On the test environment a cluster created outside the resource manager refused it.

**Phase.** Leading. **Requirements.** S-3, S-4, S-5, V-9.

### UC-A3 Let a pipeline deploy as itself

**Story.** As a GitOps engineer, I want a pipeline to act with its own workload identity, so that no person's credential is borrowed and the record names the pipeline.

**Acceptance.**
- The pipeline authenticates as a workload, and attribution records that identity.
- A commit signature is recorded as a source claim unless a configured profile binds it to the delivered content.

**Today.** The resource manager verifies signed deployments before applying with platform credentials. Delegated workload identity is designed.

**Phase.** Leading. **Requirements.** GO-10, GO-11, S-3, S-7.

### UC-A4 Know what each path can prove

**Story.** As a security auditor, I want every delivery path to say where authorization is verified, so that an apply status is never mistaken for proof.

**Acceptance.**
- Each path reports its verification locus: at the target, by the platform, or none.
- A delivery applied by OCM-io's work agent is reported as recorded by the management plane and not enforced at the target.

**Today.** The resource manager verifies on the platform side. Verification at the target is designed. OCM-io's work agent verifies nothing about who authorized a change.

**Phase.** Leading. **Requirements.** S-6, S-7, S-8, W-2.

### UC-A5 Give each agent only its own cluster

**Story.** As a fleet operator, I want an agent to authenticate as its own cluster and reach only that cluster's records, so that one compromised cluster cannot read or change another.

**Acceptance.**
- The management plane rejects an agent's request for another cluster's records.

**Today.** OCM-io's gRPC server authorizes each agent request against the agent's cluster identity. The management plane serves no agents yet, its own or OCM-io's.

**Phase.** Leading. **Requirements.** S-12, EN-3, K-3.

### UC-A6 Grant a team exactly the access it needs

**Story.** As a tenant administrator, I want to grant a team specific operations on specific workspaces, so that more people can use the management plane directly and nobody has to build a wrapper service in front of it.

**Acceptance.**
- Permissions are scoped by workspace, resource type, and operation.
- A refused request reveals nothing the caller cannot see.
- Grants, and changes to them, appear in history.

**Today.** OCM-io relies on Kubernetes RBAC over namespaces and cluster-scoped kinds. The resource manager authenticates callers and does not authorize them.

**Phase.** Alongside for inventory and history; Leading for intent. **Requirements.** S-14, T-1, IS-5, IS-7.

### UC-A7 Deliver without standing authority over every cluster

**Story.** As a security auditor, I want the management plane to reach a cluster with the requesting user's credential or a delegated identity, and not with a standing administrator credential, so that compromising the management plane does not hand over the fleet.

**Acceptance.**
- Each delivery path declares the rights the platform itself holds on a target.
- The standing credentials the management plane holds are reported per target.
- On a path that presents the user's credential, the platform's own rights on that target are no wider than the path declares.
- In the direct form the credential the management plane holds has only the rights the cluster's side granted. In the agent form it holds none.

**Today.** OCM-io's work agent applies with its own service account, or with an executor service account that a work names. The resource manager can present the caller's token to a target. For each cluster it creates, it also stores the token of a platform service account that has administrator rights on that cluster.

**Phase.** Leading. **Requirements.** S-15, S-3, S-6, W-2, EN-7.

---

## 9. Inventory, search, and history

### UC-I1 See every cluster in one inventory

**Story.** As a fleet operator, I want one list of every cluster, with its hub, membership state, enrollment state, labels, and claims, so that I do not query each hub in turn.

**Acceptance.**
- Each value shows its source and when it was last confirmed.
- A stale value is shown as stale, never as empty.
- A cluster that is observed and not enrolled is shown as that.

**Today.** Each hub lists its own clusters. The resource manager holds targets in its database and has no API that lists them.

**Phase.** Alongside. **Requirements.** IS-1, M-3, F-1, M-11, EN-12.

### UC-I2 Search resources across the fleet

**Story.** As a fleet operator, I want to search for resources across all clusters by type, label, and field, so that I can answer a question without visiting each cluster.

**Acceptance.**
- One query covers every enrolled cluster, whichever hub it came from or none.
- A cluster that is observed and not enrolled contributes the facts its hub holds, and nothing from inside the cluster.

**Today.** OCM-io has no search. The resource manager indexes every Kubernetes cluster it holds a credential for and serves a filtered query over the result, which its console's search uses.

**Phase.** Alongside for clusters and hub-held facts; Leading for resources on enrolled clusters. **Requirements.** IS-2, IS-3, IS-5.

### UC-I3 Ask a question that joins records

**Story.** As a fleet operator, I want to ask which clusters in a tenant are affected by an intent, so that I can assess a change before and after I make it.

**Acceptance.**
- The management API answers questions that join clusters, workspaces, intent, placement, and deliveries.

**Today.** Neither system answers this in one query.

**Phase.** Leading. **Requirements.** IS-4, A-4.

### UC-I4 See how state changed over time

**Story.** As a fleet operator, I want to see what a cluster's labels, membership, and deliveries were last week, so that I can explain a trend or an incident.

**Acceptance.**
- Changes to management state are kept as an append-only history.
- Retention is declared, and history outlives its subject for that period.

**Today.** OCM-io's API holds current state only. The resource manager keeps earlier versions of a fulfillment's strategies while the fulfillment exists, and nothing else.

**Phase.** Alongside. **Requirements.** IS-7, IS-8, IS-9.

### UC-I5 Keep inventory across restarts

**Story.** As a platform operator, I want inventory to survive a restart and catch up incrementally, so that search is not empty while an index is rebuilt.

**Acceptance.**
- After a restart, inventory is available at once and is marked with its age.
- Catching up reads only what changed and does not flood a hub's API.

**Today.** The resource manager stores inventory in its database.

**Phase.** Alongside. **Requirements.** IS-6, R-9, R-12.

---

## 10. Compatibility with OCM-io

### UC-C1 Keep clusters registered and agents as they are

**Story.** As an OCM-io user, I want the clusters I already registered to stay registered with my hub, with the same agent, while the management plane also manages them, so that I gain its capabilities without re-registering or reinstalling anything.

**Acceptance.**
- Enrolling a cluster does not replace, reconfigure, or restart its klusterlet, and the hub goes on reporting the cluster as it did.
- At most one target exists for each cluster.
- Add-ons and works that OCM-io delivers to the cluster are unaffected.

**Today.** The integration does not exist yet. On the test environment, the clusters that are both targets of the resource manager and managed clusters of the hub stayed available on the hub through every delivery, and the resource manager's delivery touched nothing that OCM-io owns. The resource manager can create a target only through an add-on's declaration or as the output of a delivery; it has no target API.

**Phase.** Leading. **Requirements.** K-2, F-2, EN-2, M-6, C-6, U-5.

### UC-C2 Keep using OCM-io as before

**Story.** As an OCM-io user, I want `kubectl`, `clusteradm`, and my existing objects to behave exactly as they do today, so that the management plane's arrival costs me nothing.

**Acceptance.**
- Objects in native mode are acted on by OCM-io's controllers alone.
- A published matrix says which OCM-io operations are available, degraded, or unavailable in each phase and mode.

**Today.** This is OCM-io as it is.

**Phase.** Alongside. **Requirements.** AD-2, K-4, K-10, C-4, F-3.

### UC-C3 Keep OCM-io's agent as the only agent on a cluster

**Story.** As a fleet operator who retires a hub and wants no second agent on some clusters, I want their existing klusterlet to take its work from the management plane, so that those clusters keep the one agent they have.

**Acceptance.**
- This is optional. No cluster needs it in order to be enrolled or delivered to.
- The klusterlet binary is unchanged. Only its configuration changes.
- No applied workload is deleted, recreated, or interrupted by the change.
- The change can be rolled back.

**Today.** OCM-io's work agent can follow a source over gRPC, and OCM-io documents that such a source may be a service with a database. The management plane does not serve that protocol. On a test cluster an unmodified klusterlet followed a change of its hub's address with no change to the workload it had applied: the agent made the new source a second owner of each resource, and evicted the old applied work after the grace period.

**Phase.** Standalone. **Requirements.** K-3, W-4, W-7, X-6, M-7.

### UC-C4 Keep OCM-io add-ons working

**Story.** As an add-on developer, I want my add-on to keep registering, renewing its lease, and receiving its manifests, so that the management plane's introduction does not break it.

**Acceptance.**
- The add-on behaves the same wherever a hub API remains.
- Moving the add-on to the management API is my choice.

**Today.** This is OCM-io as it is.

**Phase.** Alongside. **Requirements.** K-8, K-9.

### UC-C5 Keep a tool that reads OCM-io objects

**Story.** As an OCM-io user with tools that read `ManagedCluster` and `PlacementDecision`, I want those objects to stay available, so that my tools keep working.

**Acceptance.**
- While my hub exists, those objects stay on it, written by OCM-io, whether or not clusters are enrolled.
- Where no hub remains, a projection is offered for the scopes I select. It is marked as a projection and states its freshness.
- I can turn projection off for scopes that have no consumer.

**Today.** On a hub, this is OCM-io as it is. Projection is not available.

**Phase.** Alongside in native mode; Standalone as a projection. **Requirements.** K-7, K-6, GO-8.

### UC-C6 Link a cluster the management plane already knows

**Story.** As a fleet operator, I want a cluster the resource manager created to be linked to the `ManagedCluster` that appears when it registers with a hub, so that it is not carried as two unrelated records.

**Acceptance.**
- The declared match links the two when it identifies exactly one candidate.
- An unmatched target stays visible and is reported as unregistered.
- No `ManagedCluster` is created on the cluster's behalf.

**Today.** The design's test environment has two such pairs. For each, the resource manager and the hub record the same API address and the same certificate authority, so an exact-match key on those two would link them. One pair carries different names on the two sides, so a name match would not link it. The targets have no labels, and the `ManagedCluster` objects have no claims.

**Phase.** Alongside. **Requirements.** M-9, I-3, I-6.

### UC-C7 Keep two clusters with one name apart

**Story.** As a fleet operator with two hubs that each have a cluster named `prod-1`, I want them to stay two clusters, so that labels and work never cross between them.

**Acceptance.**
- Identity includes the hub connection.
- Any name the integration generates for a target cannot collide with an existing target.

**Today.** `ManagedCluster` names are unique per hub. The resource manager requires target names to be unique across the whole installation.

**Phase.** Alongside. **Requirements.** I-2, I-7.

### UC-C8 Correct a wrong link

**Story.** As a fleet operator, I want to clear or reassign a correlation that is wrong, so that a heuristic match does not become permanent.

**Acceptance.**
- The correction is an explicit, recorded operation.
- A later name match does not silently undo it.

**Today.** Not available.

**Phase.** Alongside. **Requirements.** I-5, I-8.

### UC-C9 Treat a re-registered cluster as new

**Story.** As a fleet operator, I want a cluster that was deleted and registered again under the same name to be a new cluster, so that it does not inherit its predecessor's history or cleanup obligations.

**Acceptance.**
- Recorded markers detect the new incarnation.
- The old target is gone before the new one exists, and nothing is inherited.

**Today.** OCM-io gives a re-registered cluster a new object under the old name and publishes no stable cluster identity by default.

**Phase.** Alongside. **Requirements.** I-4, I-9.

### UC-C10 Stop safely on an unsupported version

**Story.** As an OCM-io user, I want the integration to stop writing and say why when it meets a hub or agent version it was not validated with, so that an upgrade never turns into silent corruption.

**Acceptance.**
- A release declares the OCM-io versions, the combinations, and the skew it supports.
- Outside that set the integration makes no write and reports the reason. Scopes in native mode are unaffected.

**Today.** This design takes a hub and its agents within one minor version of each other as its baseline. No combination with the resource manager has been declared, because no integration exists.

**Phase.** Alongside. **Requirements.** C-8, K-1, K-11, V-12.

---

## 11. GitOps

### UC-G1 Keep applying OCM-io objects from Git

**Story.** As a GitOps engineer, I want my pipeline to keep applying OCM-io objects, and I want nothing to fight it, so that Git remains the authority for what it declares.

**Acceptance.**
- The integration never writes a field my applier owns, and injects no metadata.
- If the management plane intends something different, I see drift reported, not a write war.

**Today.** This is OCM-io as it is.

**Phase.** Alongside. **Requirements.** GO-3, GO-5, C-3, C-5, V-10.

### UC-G2 Drive the fleet from one object in Git

**Story.** As a GitOps engineer, I want to keep one fan-out object in Git and have the management plane deliver it to every selected cluster, so that the hub does not store an object per cluster.

**Acceptance.**
- The object's semantics are preserved, or each approximation is declared.
- The integration writes only the object's status.
- Per-cluster records are in the management datastore.

**Today.** The OCM-io v1.4.0 hub in the design's test environment serves the `ManifestWorkReplicaSet` API and runs no controller for it by default. OCM-io has since turned that controller's feature gate on by default, and where it runs the object is in native mode. Nothing drives the resource manager from it yet.

**Phase.** Leading. **Requirements.** K-5, GO-2, GO-4, GO-7, W-12, F-4.

### UC-G3 Keep selecting clusters from placement decisions

**Story.** As a GitOps engineer whose tooling generates applications from `PlacementDecision`, I want that object to stay available, so that my generators keep working.

**Acceptance.**
- `PlacementDecision` is available for the scopes I select, in native or projected mode.

**Today.** OCM-io's placement controller writes `PlacementDecision` objects, at most 100 decisions in each.

**Phase.** Native mode while a hub decides; a projection where no hub remains. **Requirements.** GO-8, K-7.

### UC-G4 Apply to the management API from a pipeline

**Story.** As a GitOps engineer without a hub, I want my pipeline to apply desired state straight to the management API, so that I need no Kubernetes API in the path.

**Acceptance.**
- One idempotent operation creates or updates, a dry run reports the difference, and delete removes exactly one unit.

**Today.** The resource manager's deployment API offers create, get, list, resume, and delete. It has no update or apply.

**Phase.** Leading. **Requirements.** GO-9, GO-10, A-1.

### UC-G5 Roll back with a revert

**Story.** As a GitOps engineer, I want a revert in Git to roll the fleet back, so that rollback is the same operation as any other change.

**Acceptance.**
- The revert is reconciled as an ordinary change and is recorded in history.

**Today.** OCM-io reconciles whatever is applied.

**Phase.** Alongside. **Requirements.** GO-12, IS-7.

### UC-G6 Keep pipelines running when the management plane is down

**Story.** As a GitOps engineer, I want my pipeline to succeed in applying even if the management plane is down, so that its outage is not my pipeline's outage.

**Acceptance.**
- No webhook, finalizer, or required field from the integration can block an apply.
- Effects resume when the management plane returns.

**Today.** No such dependency exists.

**Phase.** Alongside. **Requirements.** GO-6, R-13.

### UC-G7 Change a label in Git and see placement follow

**Story.** As a GitOps engineer, I want a label I change on a `ManagedCluster` in Git to reach the management plane's placement, so that Git stays the place where cluster facts are edited.

**Acceptance.**
- The change is visible in the management plane within the declared budget.
- The integration never writes the label back.

**Today.** The resource manager selects targets by exact label match, by an explicit list, or all of them. It has no API that updates a target's labels.

**Phase.** Alongside for seeing the change; Leading for placement, which selects enrolled clusters only. **Requirements.** M-3, M-4, M-10, GO-3.

### UC-G8 Keep a GitOps agent on the cluster in charge of what it applies

**Story.** As a GitOps engineer whose agent on each cluster applies from Git, I want the management plane to leave every resource my agent owns alone, so that enrolling the cluster starts no fight.

**Acceptance.**
- The management plane never takes over or deletes a resource my agent applied.
- If my agent and an intent in the management plane name the same resource, a conflict is reported that names both owners.
- I can deliver the access piece itself from Git.

**Today.** The resource manager applies with server-side apply and never forces, so a field that another manager owns is refused to it.

**Phase.** Leading. **Requirements.** C-5, W-14, W-6, EN-4.

---

## 12. Migration

### UC-M1 Install without effect

**Story.** As a platform operator, I want installation and hub connection to do nothing until I select a scope, so that trying the system is safe.

**Acceptance.**
- With no scope selected, the hub's objects and audit record show no write from the integration.

**Today.** Not applicable until the integration exists.

**Phase.** Alongside. **Requirements.** AD-1, Z-1, U-1.

### UC-M2 Move a set of workloads in steps, verify, and go back

**Story.** As a fleet operator, I want to move the workloads of one set of clusters to the resource manager a few at a time, check each step, and be able to go back, so that I never bet the fleet on one step.

**Acceptance.**
- A dry run shows what will change and what is approximated.
- At no moment do both systems accept changes to the same workload.
- Going back returns each workload to OCM-io without losing intent that was accepted in between.

**Today.** Not available as an operation. A single move was made by hand on the test environment (UC-O10).

**Phase.** Leading. **Requirements.** X-3, X-4, X-6, W-7, U-4, V-7.

### UC-M3 Stop depending on a hub for membership

**Story.** As a fleet operator, I want a set of clusters to be members of the management plane in their own right, so that their membership no longer depends on a hub.

**Acceptance.**
- An enrolled cluster needs no new agent and no new registration: making the resource manager the system of record for its membership is a recorded management API operation.
- The cluster keeps its management-plane identity and history.
- Where I keep the klusterlet as the cluster's agent, it registers with the management plane, acceptance is arranged before it switches, and no cluster is reported unreachable because of the move.

**Today.** Not available. OCM-io's registration agent can register over gRPC. On the hub that path still stores everything in the Kubernetes API. An agent that registers again stops renewing its lease until it is approved, and a hub reports the cluster unreachable after about five minutes.

**Phase.** Standalone. **Requirements.** M-7, K-2, K-3, I-10, S-2.

### UC-M4 Retire a hub

**Story.** As a fleet operator, I want to retire a hub once nothing depends on it, so that I stop operating a control plane I no longer need.

**Acceptance.**
- A procedure verifies that every cluster that is to stay managed is enrolled, that every workload that is to stay has been moved, and that no consumer still reads the hub.
- Enrolled clusters need no switch-over when the hub goes.

**Today.** Not available.

**Phase.** Standalone. **Requirements.** U-6, EN-10, X-5.

### UC-M5 Remove the integration and carry on

**Story.** As an OCM-io user, I want to remove the integration and continue with OCM-io, so that adopting it was never a one-way door.

**Acceptance.**
- Scopes in native mode behave as they did before.
- No OCM-io object is left invalid, blocked on a finalizer, or orphaned.
- Workloads the resource manager delivered keep running. The access piece on each cluster is removed by withdrawal, or by me.

**Today.** Not applicable until the integration exists.

**Phase.** Alongside. **Requirements.** F-12, U-3, R-13, V-13.

### UC-M6 Know what is lost before committing

**Story.** As a fleet operator, I want to see, per cluster, what a mode preserves, approximates, or cannot offer, so that I decide with the losses in front of me.

**Acceptance.**
- Fidelity is declared per aspect and mode and reported per cluster.

**Today.** Not available.

**Phase.** Alongside. **Requirements.** F-11, K-1, U-4, AD-8, O-1.

---

## 13. What must never happen

| ID | Must never happen | Guard |
|---|---|---|
| UC-N1 | A `ManagedCluster` is created to stand for a cluster OCM-io did not register. | I-6 |
| UC-N2 | A user's token is found at rest in the datastore, workflow history, logs, or a Kubernetes object. | S-4, V-9 |
| UC-N3 | The integration adds a label, annotation, finalizer, or owner reference to an object it does not own. | C-3 |
| UC-N4 | Two systems accept writes as the authority for the same aspect of the same scope. | X-1, X-4 |
| UC-N5 | A workload is deleted or recreated because of a move, a cutover, a restore, or a management-plane outage. | W-7, R-4, R-5 |
| UC-N6 | One tenant sees another tenant's names, errors, or records, including through a projection. | T-2 |
| UC-N7 | A cluster that was excluded or withdrawn is silently observed or enrolled again. | M-5 |
| UC-N8 | An object appears on a hub for a cluster that is only observed. | N-2, AD-1 |
| UC-N9 | An apply status is shown as health, or a missing value as healthy. | W-5, M-11 |
| UC-N10 | Older intent from a restored database silently overwrites newer applied state. | R-4, W-3 |
| UC-N11 | An ambiguous match is resolved by guessing. | I-3 |
| UC-N12 | An apply is forced over another field manager. | W-14, GO-5 |
| UC-N13 | A credential, a trust anchor, or a verification policy is placed in an OCM-io object, a driving object, or a projection. | S-9, S-11 |
| UC-N14 | Two intents deliver different content for the same resource on the same cluster. | W-13 |
| UC-N15 | The management plane creates a `ManifestWork`, or any other hub object, to deliver or to record its own intent. | W-8 |
| UC-N16 | Something is added to a cluster that nobody enrolled, or a klusterlet is changed by enrollment. | C-6, K-2, EN-2 |
| UC-N17 | A cluster is enrolled, or a workload is moved, without someone entitled to it asking. | EN-1, W-11 |
| UC-N18 | Removing intent deletes a resource that another owner still claims. | W-6 |
| UC-N19 | A delivery is shown as in place after another writer took over or removed what it applied. | W-16 |
| UC-N20 | A credential for a cluster is returned by a query, or a target is created from an enrollment that was not verified. | EN-8, EN-6 |
