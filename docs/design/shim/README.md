# OCM-io Fleet Integration

This directory is the design home for introducing **OCM Fleet's resource manager architecture** to **OCM-io**, gradually and without breaking the people, tools, and agents that use OCM-io today. It holds requirements and user stories first, then the reasoning behind them, the deployment shapes they permit, candidate mechanisms, and an honest ledger of what exists.

**Status: design only.** Nothing here is installable. OCM Fleet is an experimental variant of Open Cluster Management, and this design is input to upstream discussion, not a claim of acceptance.

## The short version

The core management API for a fleet becomes a **standalone server API**: the resource manager, with its own relational database. It is not an extension of the Kubernetes API. Kubernetes can drive it, and is not it.

Today an OCM-io hub's Kubernetes API and etcd are both the management API and its store. A dedicated management API with its own datastore is the conventional shape of managed, multitenant cluster services, and this design moves toward it.

It gets there beside OCM-io, not through it. Someone who already runs a hub and many managed clusters installs the management plane, connects the hub, and sees every cluster. The clusters they choose are then *enrolled*: each gets a small, declared access piece, stays registered with its hub, and keeps its agent untouched. From then on the resource manager deploys to those clusters, indexes them, and shows them with its own machinery, in its own API and console. OCM-io keeps doing what it does on the same clusters, and a workload moves from one system to the other only when its owner moves it. The resource manager never asks a hub to deliver or store anything for it. It replaces the *essential* need for Kubernetes controllers and Kubernetes-stored state in fleet management, and stays compatible with their *supplemental* use.

The price is one more service and one more database to operate. The return is six things an extension of the Kubernetes API does not provide efficiently.

## Why

| | Motivation | The requirement in one line |
|---|---|---|
| MO-1 | Scale | Management state grows in a database that scales independently of any Kubernetes control plane. The design target is 10,000 clusters. |
| MO-2 | Multitenancy | One management plane with its own tenancy model, hard isolation, and per-tenant quality of service, instead of a hub per tenant. |
| MO-3 | Security and attribution | Every change is attributable to an end user or a workload identity, a user's token is passed through without being stored, and access is fine-grained. |
| MO-4 | Inventory, search, and history | Durable fleet-wide inventory and search, and a record of who changed what, and when. |
| MO-5 | Reliability and recovery | Management state is backed up, restored, and failed over with database procedures against declared objectives. |
| MO-6 | Independence from Kubernetes | The management plane runs without Kubernetes, from one process on a laptop to a replicated deployment. |

The evidence for each, the alternatives considered, and the costs accepted are in [analysis.md](analysis.md).

## How it is introduced

| Phase | Who leads | What OCM-io users notice |
|---|---|---|
| **1. Alongside** | OCM-io. The management plane connects to one or more hubs and shows every cluster, with its labels, cluster sets, and state. It changes nothing on a hub or on a cluster. | Nothing. |
| **2. Leading** | The resource manager, for the clusters that enroll and for the intent it holds. It delivers through its own API and its own delivery, indexes those clusters, and keeps every record in its database. OCM-io leads for everything else, on the same clusters. | Their hub, agents, objects, and pipelines work as before. An enrolled cluster carries a small access piece. A hub stores nothing for what the resource manager delivers. |
| **3. Standalone** | The resource manager. No hub is required. | A hub is optional, kept only for what they still choose to run on it. |

Every step is opt-in: a hub to connect, a cluster to enroll, a workload to move. Each has a way back, and each phase is useful without the next.

## What stays true for OCM-io users

- OCM-io is not forked, patched, or required to change.
- Existing klusterlet agents stay as they are and keep serving their hub. Enrolling a cluster reinstalls and reconfigures nothing.
- OCM-io objects on a hub are handled by OCM-io's own controllers exactly as before.
- GitOps remains an authority. The integration never writes a field an applier owns and never fights it.
- No `ManagedCluster` is fabricated, and no metadata is injected into objects the integration does not own.
- No `ManifestWork`, and no other hub object, is created for a workload the resource manager delivers.
- Nothing happens until someone selects it.
- Removing the integration leaves OCM-io working.

## What exists today

The resource manager already is a standalone server on SQLite or PostgreSQL with durable orchestration, and it runs without Kubernetes. It deploys to, indexes, and shows the clusters it creates, with a console. It cannot yet take in a cluster that already exists. It does not yet have authorization, tenancy, history, more than one replica, backup procedures, a target API, an agent of its own on a cluster, or any connection to OCM-io. The integration itself is not built. [coverage.md](coverage.md) lists 40 declared gaps and what closes each one.

The first increment, which is still to be built, brings that experience to a first existing fleet: hubs are connected and their clusters shown, the clusters that opt in are enrolled in the direct form, and the resource manager deploys to them and indexes them itself ([topologies.md](topologies.md) section 9).

## Where to read

**To decide whether this direction is right**, read [requirements.md](requirements.md) sections 1 to 4, then [analysis.md](analysis.md) sections 2 to 5.

**To see what it means for a person**, read [use-cases.md](use-cases.md). Its section 3 follows someone with a working OCM-io fleet from installation to everyday use. The rest has stories for scale, recovery, evaluation, tenancy, attribution, inventory, compatibility, GitOps, and migration, and a list of what must never happen.

**If you maintain OCM-io**, read requirements.md sections 6, 11, and 20 for the compatibility contract, what enrollment adds to a cluster and to a hub, and the constraints. Then read [enhancements.md](enhancements.md) section 2 for options that would help and that stand on their own.

**If you build the resource manager or the integration**, read requirements.md in full, then [baseline-hypotheses.md](baseline-hypotheses.md), then coverage.md section 4.

**If you will operate it**, read [topologies.md](topologies.md), then requirements.md sections 11, 16, and 17.

## Questions this directory answers

1. **What is decided?** A standalone management API on a relational database, introduced gradually and beside OCM-io. See requirements.md section 1 and [decisions.md](decisions.md) section 1.
2. **Why not extend the Kubernetes API further?** See analysis.md sections 2 and 3.
3. **What does it cost?** See analysis.md section 4.
4. **What keeps working?** See requirements.md section 6.
5. **How does a cluster I already have come under it?** By enrollment, which is opt-in per cluster and leaves the cluster's agent and registration alone. See requirements.md section 11.
6. **Does a hub deliver or store anything for the resource manager?** No. See requirements.md section 12.
7. **Can Kubernetes and GitOps still drive it?** Yes. See requirements.md section 7.
8. **Who is the authority, and how does that change?** One system of record per aspect and per workload, moved by an explicit operation. See requirements.md sections 4 and 8.
9. **How is it made highly available?** The outcomes are required; the database arrangement is an open choice. See requirements.md section 16, topologies.md section 3, and OQ-1.
10. **What is missing today?** See coverage.md section 4.
11. **What is still open?** See decisions.md section 2.

## Document map

| Document | Contains | Normative |
|---|---|---|
| **README.md** | Orientation and glossary | No |
| **[requirements.md](requirements.md)** | The decision, motivations MO, principles AD, outcomes F, phases Z, and the requirement families A, K, GO, X, I, M, EN, W, T, S, IS, R, N, U, O, V, C, and non-goals G | **Yes** |
| **[use-cases.md](use-cases.md)** | Personas and user stories UC | No |
| **[analysis.md](analysis.md)** | Evidence for each motivation, alternatives, tradeoffs, what both systems do today, compatibility analysis, and sources | No |
| **[topologies.md](topologies.md)** | Deployment shapes, datastore arrangements, what runs where, and the first supported combination | No |
| **[decisions.md](decisions.md)** | Recorded directions DR, open questions OQ, and tensions | No |
| **[baseline-hypotheses.md](baseline-hypotheses.md)** | Candidate mechanisms BH | No |
| **[enhancements.md](enhancements.md)** | OCM-io options E, resource manager capabilities FC, and rejected solutions RS | No |
| **[coverage.md](coverage.md)** | What exists, what is hypothesis, and the declared gaps GAP | No |

Requirements are the only normative document. No other document can relax one.

## Glossary

Normative terms are defined in requirements.md section 1.3. The most used are repeated here.

**The design's terms**

- **Management plane**: the resource manager, its datastore, and the integration.
- **Integration**: everything that connects OCM-io and the resource manager. It is an extension of the resource manager: a server add-on and a console plugin.
- **Hub connection**: one OCM-io hub the management plane is configured to read.
- **Observed cluster**: a cluster the management plane knows through a hub connection. Nothing has been added to it.
- **Enrollment**: the opt-in step that gives the management plane access to one cluster, by adding a declared access piece to it. The cluster stays registered with its hub. **Withdrawal** reverses it.
- **Access form**: how the management plane reaches an enrolled cluster. *Direct*: the resource manager calls the cluster's API. *Agent*: the resource manager's own agent on the cluster connects out and acts locally.
- **Delivery path**: how intent reaches a cluster: *direct* or *agent*, following the access form, or optionally *work agent*, where the management plane serves OCM-io's work agent. Writing a `ManifestWork` to a hub is not one of them.
- **Aspect**: a set of management facts with one owner at a time, such as cluster membership or workload intent.
- **System of record**: the one place whose value for an aspect is authoritative.
- **Cutover**: the explicit operation that moves the system of record for an aspect from a hub to the resource manager. The smallest is the **move** of one workload.
- **Compatibility mode**: how an OCM-io API kind is offered. *Native*: OCM-io's controllers act on it. *Driven*: its objects drive the management API. *Projected*: the integration writes it, read-only, from management-plane state. On a hub everything is native unless a deployment selects otherwise.
- **Kubernetes-facing surface**: an optional Kubernetes API that holds driving objects and projections.

**OCM-io terms**

- **Hub**: the OCM-io control plane, a Kubernetes API with OCM-io's APIs and controllers.
- **Klusterlet**: the agent on a managed cluster. It has a registration agent and a work agent.
- **`ManagedCluster`**: the hub's record of a registered cluster.
- **`ManifestWork`**: a bundle of manifests for one cluster, applied by the work agent.
- **`ManifestWorkReplicaSet`**: a fan-out object that creates one `ManifestWork` per selected cluster.
- **`Placement` and `PlacementDecision`**: a selection of clusters, and its result.
- **`ManagedClusterSet`**: a grouping of clusters used for access and selection.
- **Add-on**: a controller and agent pair managed by OCM-io's addon-framework. The resource manager uses the same word for the server half of one of its extensions.

**Resource manager terms**

- **Resource manager**: the OCM Fleet server that owns the management API, authorization, persistence, and orchestration.
- **Target**: something the resource manager can deliver to. An enrolled cluster is one.
- **Extension**: a server add-on and a console plugin that add resource types, delivery, inventory, and console views to the resource manager.
- **Fulfillment**: one unit of intent: manifests, placement, and rollout.
- **Delivery**: the record of intent sent to one target, and its outcome.
- **Tenant and workspace**: the resource manager's units of isolation. They are designed and not yet implemented.

## Conventions

- **MUST**, **SHOULD**, and **MAY** carry their usual force and appear only in requirements.md.
- Identifiers are stable. A reference by identifier, such as `K-2` or `GAP-7`, still finds its item after text moves. Hypotheses are the exception: they are expected to change, and their identifiers follow the order of their document.
- "Today" always means unmodified OCM-io v1.4.x and the current resource manager.
