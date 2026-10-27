# Topologies

**Status: descriptive, not normative.** This document records the deployment shapes the design permits, what each one survives, and what every one of them must preserve. It selects no product and no mechanism. Requirements are in [requirements.md](requirements.md); candidate mechanisms are in [baseline-hypotheses.md](baseline-hypotheses.md).

## 1. Components and who owns them

| Component | Owner | Where it runs | Needed |
|---|---|---|---|
| OCM-io hub: API server, etcd, hub controllers | OCM-io | A Kubernetes cluster | For as long as anything is still run on it |
| Klusterlet: registration agent and work agent | OCM-io | Each managed cluster | For every cluster registered with a hub. Enrollment does not touch it. |
| OCM-io add-on managers and agents | Their authors | Hub and managed clusters | Optional |
| Resource manager, with its console | OCM Fleet | Anywhere: a process, a container, or a workload on a cluster | Always |
| Management datastore | The operator | Inside the resource manager process, or a PostgreSQL service | Always |
| Taking in an existing cluster: verification, the stored credential and its renewal, the target, the indexer | OCM Fleet | With the resource manager | When a cluster is enrolled, with a hub or without one |
| The integration: hub connections, observed clusters, and their console views | An extension of the resource manager | With the resource manager | When a hub is connected |
| Access piece, direct form: a service account and its role binding | The cluster's administrator | Each enrolled cluster | For the direct form |
| Access piece, agent form: the resource manager's agent | OCM Fleet | Each enrolled cluster | For the agent form |
| Enrollment add-on: an add-on template and its registration | The hub's administrator | The hub, with a small enrollment agent on each cluster while the add-on is enabled there | Optional. One route for rolling out the access piece. |
| Agent endpoint for OCM-io agents | The integration | With the resource manager | Optional. Only when the management plane serves OCM-io agents. |
| Kubernetes-facing surface: driver and projector | The integration | Next to a Kubernetes API | Optional |

Nothing is added to a cluster that is not enrolled (C-6).

## 2. Management plane topologies

| Topology | Processes | Datastore | Survives | Does not survive | State today |
|---|---|---|---|---|---|
| **Evaluation** | One process, or the all-in-one container | Embedded SQLite file | A process restart | Loss of the host or the file. It is a single point of failure (R-14). | Works |
| **Single instance** | One resource manager replica | One PostgreSQL instance | A restart; any loss, by restoring a backup | A node loss without a restore | PostgreSQL is supported. No backup procedure is documented. |
| **Highly available** | Several replicas behind a load balancer | PostgreSQL with standbys and automatic failover | Loss of a replica, a node, or a zone | Loss of the region | Replicas are designed, not exercised. Failover is untested. |
| **Regional recovery** | A highly available deployment, plus a standby deployment in another region | Replication or backup shipping between regions | Loss of a region, within declared objectives | Simultaneous loss of both regions | Not available |

Every topology declares its recovery objectives and proves them in a drill (R-1, V-6).

## 3. Datastore arrangements

The design requires outcomes, not a product (A-5, G-7). These arrangements are known to provide them. Which of them the first production release validates is an open question (OQ-1).

| Arrangement | Failover | What else it needs | Scales reads | Scales writes |
|---|---|---|---|---|
| Single instance | None. Restore from backup. | Nothing | No | No |
| Primary with standbys, managed by Patroni | Automatic | A consensus store for the failover manager, and a proxy or load balancer in front | Yes, from standbys | No |
| Primary with standbys, managed by pg_auto_failover | Automatic | A monitor node, and a proxy or load balancer | Yes | No |
| Primary with standbys, managed by CloudNativePG | Automatic | A Kubernetes cluster to host the operator and the database | Yes | No |
| Sharded, by an extension such as Citus or by a sharding proxy, with each shard highly available by one of the above | Per shard | A coordinator and workers, or the proxy | Yes | Yes |
| Multi-primary | Not needed for one node | An extension or product that resolves write conflicts | Yes | Yes, with conflict handling |
| Active and passive sites | Promotion of the passive site | Replication or backup shipping between sites | No | No |

Three points apply to all of them.

- **Failover is not instant.** One primary with two standbys is the common arrangement. Its failover is commonly ten to thirty seconds, run by tooling outside the database. A three-member etcd re-elects a leader in seconds by itself. The design accepts this difference and bounds it with R-5: while the management plane is failing over, clusters keep running.
- **Connection pooling and backup are separate concerns.** A pooler such as PgBouncer and a backup tool such as pgBackRest are common companions. Combinations in wide use are Patroni with PgBouncer and pgBackRest, CloudNativePG with PgBouncer, and Citus with Patroni or with pg_auto_failover.
- **Hosting the database on Kubernetes is a choice with a consequence.** It ties the datastore's availability to that cluster. That is acceptable where the management plane itself runs on Kubernetes. The management API is still not a Kubernetes API (A-1).

Multi-primary operation is not assumed anywhere in this design. Using it would need its own analysis of write conflicts.

## 4. Where the management plane runs

| Placement | Suits | Note |
|---|---|---|
| A workstation or a virtual machine, with no Kubernetes | Evaluation, and starting a first cluster (A-3) | This works today. |
| A workload on a Kubernetes cluster | Production | The cluster provides compute only. Its API is not used to store management state, so upgrading it interrupts management only for the failover time (R-8). |
| The hub cluster itself | Small deployments | The hub's failure domain and the management plane's are then the same, and the deployment has to say so. The hub's control plane is never a delivery target (X-9). |
| A service a provider operates for tenants | Managed service providers | The API is internet-facing and multitenant (T-10). |

In the direct form the management plane has to run where it can reach the API of every cluster enrolled that way. In the agent form it only has to be reachable from the clusters (EN-3).

## 5. How the management plane sits beside OCM-io

```
1. Alongside               2. Leading                          3. Standalone

 +--------+  reads  +----+   +--------+  reads  +----+                 +----+
 | hub(s) | <------ | RM |   | hub(s) | <------ | RM |                 | RM |
 +--------+         +----+   +--------+         +----+                 +----+
     ^                           ^                 |                      |
     | registers,                | as before       | delivers,            | delivers,
     | takes work                |                 | indexes              | indexes
     |                           |                 v                      v
 +---------+                 +---------------------------+           +---------+
 | cluster |                 | cluster: klusterlet,      |           | cluster |
 +---------+                 |          access piece     |           +---------+
                             +---------------------------+
```

RM is the resource manager with its datastore and console. In the Leading phase one cluster is served by both: its klusterlet follows the hub as before, and the resource manager reaches it through its access piece. Nothing passes from the resource manager through the hub to the cluster.

| Shape | Phase | Note |
|---|---|---|
| One hub, observed | Alongside | The smallest first step |
| Several hubs, observed | Alongside | Consolidation under one API. Identity includes the hub connection (I-2). Each hub fails independently (M-2). |
| Hubs, with some clusters enrolled | Leading | Each workload is held by one system (X-2) |
| Hubs, and clusters that have no hub | Leading and Standalone | Both use the same identity model and access forms (M-8) |
| No hub | Standalone | A Kubernetes-facing surface is optional (GO-1) |

## 6. What is added to a managed cluster

| Delivery path | Added to the cluster | Added for each delivery | Reaches a cluster that only connects out | Identity the cluster sees | Credential the management plane holds | Where evidence is verified |
|---|---|---|---|---|---|---|
| Direct | A service account and its role binding | Nothing beyond the workload | No | The enrolled service account, or the caller's own token where the cluster trusts the identity provider | One per cluster, with the rights the cluster's side granted | On the platform side |
| Agent | The resource manager's agent | Nothing beyond the workload | Yes | The agent's local identity | None | At the target, once implemented |
| Work agent, optional | Nothing. The klusterlet's connection settings change. | One applied-work record | Yes | The work agent, or an executor service account | None | Nowhere. The management plane records the user. |

No path writes to a hub for a delivery (W-8). What enrollment costs on a hub depends on the route: nothing for a manifest applied to the cluster, and for a roll-out through OCM-io's add-on mechanism the objects that mechanism keeps while the add-on is enabled for the cluster (EN-5).

## 7. The Kubernetes-facing surface

A surface is optional, and nothing about enrolling a cluster or delivering to it needs one.

| Choice | Holds | Suits |
|---|---|---|
| None | Nothing | Deployments that use the management API, its console, and its command line |
| The existing hub | Driving objects, next to the objects OCM-io keeps in native mode | Deployments that want to drive the management API from objects on their hub |
| A small API server kept for this purpose | Driving objects and selected projections only | Deployments that retire their hubs and keep Kubernetes-native workflows |

The surface is never the system of record for fan-out, and no per-target record is written to it (GO-7). Its objects are counted (K-12).

## 8. Failure and upgrade independence

| Event | Managed workloads | What OCM-io does on its own | Management API | Enrolled clusters |
|---|---|---|---|---|
| Management plane down | Keep running (R-5) | Unaffected (R-13) | Unavailable | Nothing new is delivered. Access resumes when it returns, or a lapsed credential is reported (EN-8). |
| Datastore failover | Keep running | Unaffected | Unavailable for the failover time | As above, for the failover time |
| One hub down | Keep running | That hub's own works and add-ons stop changing | Available. That hub's clusters are reported stale (M-2). | Delivery and indexing continue (EN-10) |
| The access piece is removed from a cluster | Keep running | Unaffected | Available | That cluster is reported withdrawn (EN-9) |
| The cluster hosting the management plane is upgraded | Keep running | Unaffected | Interrupted for no longer than the failover time (R-8) | As for management plane down, for that time |
| Management plane upgraded | Keep running | Unaffected | Rolling (U-2) | Unaffected |
| Hub or klusterlet upgraded within the declared skew | Keep running | As OCM-io behaves | Unaffected (K-11) | Unaffected |

## 9. The first supported combination

A first release validates one combination. Everything else is unclaimed until it is tested.

| Dimension | First choice |
|---|---|
| OCM-io | v1.4.x, with v1.4.0 as the reference; hub and agents within one minor version |
| Hubs | One or more existing hubs, each connected with a read-only credential |
| Phases | Alongside for every cluster in scope. Leading for the clusters that enroll. |
| Access form | Direct |
| Enrollment routes | An OCM-io add-on that the hub's administrator applies, and a manifest applied to the cluster |
| Delivery | The resource manager's own, on the direct path. No hub object for any delivery. |
| Inventory | Hub-held facts for observed clusters. Indexed resources for enrolled clusters. |
| Workloads that OCM-io delivers | Left with OCM-io, on the same clusters |
| Console | Connect a hub, see its clusters, enroll, deploy, search |
| Management plane | The Evaluation topology for trial. The Single instance topology on PostgreSQL for use. |
| Scale | The Standard profile, validated in the direct form before support is claimed (N-1, N-12) |
| Visibility | Provider-only (T-7) |
| On managed clusters | Nothing on observed clusters. On enrolled ones, a service account and its role binding, and the add-on's enrollment agent for as long as the add-on is enabled there. |
| Kubernetes-facing surface | None |

It does not claim tenant isolation, high availability, service-provider scale, clusters that accept no inbound connection, the agent form, noticing when another writer changes what it delivered, moving workloads between the two systems, driven or projected modes, service to OCM-io agents, or membership held by the management plane. Each of those needs its own evidence (Z-8).

## 10. What every topology must preserve

A deployment shape is permitted only if it preserves OCM-io unmodified (C-1 to C-4), agents left alone (K-2), nothing handed to a hub (W-8), one system of record per aspect and per workload (X-1, X-2), a standalone management API with no Kubernetes in its core path (A-1, A-2), declared recovery objectives (R-1), static stability (R-5), declared scale and declared hub and cluster object budgets (N-1, N-2, N-11), declared visibility (T-7), and a way back (F-12). A shape that cannot preserve one of these is a different design and needs its own decision.
