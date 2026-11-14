# Fleet record controller

`FleetRecord` is a namespaced Kubernetes API whose complete spec is mirrored to
Fleet's `records.open-cluster-management.io/Record` managed resource. Kubernetes
owns intent. Direct Fleet spec edits are corrected on the next poll. The record
provider accepts configuration and creates no external infrastructure.

```yaml
apiVersion: records.open-cluster-management.io/v1alpha1
kind: FleetRecord
metadata:
  name: example
  namespace: fleet-record-demo
spec:
  values:
    message: hello from Kubernetes
```

The spec supports up to 32 string values, keys of 1–128 characters, and values of
up to 1,024 characters. An empty map clears configuration. These bounds also keep
the original spec snapshot in status small enough for Kubernetes persistence.

## Run

Start Fleet with the record addon and persistent storage, using its existing
OIDC configuration:

```sh
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:build
./bin/resource-manager serve --addons record --db /path/to/fleet.db
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run record-controller:build
kubectl apply -f controllers/record/config/crd/fleetrecords.yaml
kubectl apply -f controllers/record/config/namespace.yaml
./bin/fleet-record-controller \
  --namespace=fleet-record-demo \
  --source-cluster-id=YOUR-STABLE-CLUSTER-ID \
  --fleet-url=https://fleet.example.test \
  --token-file=/path/to/fleet-token \
  --leader-elect=false
```

The cluster ID must be unique among source clusters sharing Fleet and must remain
stable across controller restarts. For an explicitly local plaintext Fleet
endpoint, pass `--allow-http`. Use `--ca-file` for an additional TLS trust bundle.
The token file is read on every request, so projected credentials can rotate.
The token must authenticate to Fleet's configured issuer with its resource
audience; the Kubernetes API token is not automatically a Fleet credential.

For an in-cluster deployment, build the container with
`npx nx run record-controller:image`, configure the Fleet URL and cluster ID in
[config/manager.yaml](config/manager.yaml), and provide a
`fleet-record-credentials` Secret containing the `token` key in
`fleet-record-system`. Create the namespaces before creating that Secret:

```sh
kubectl apply -f controllers/record/config/namespace.yaml
kubectl -n fleet-record-system create secret generic fleet-record-credentials \
  --from-file=token=/path/to/fleet-token
kubectl apply -k controllers/record/config
```

The example watches
`fleet-record-demo`, holds its leader lease in `fleet-record-system`, uses
namespaced RBAC, and runs two replicas. Keeping the controller and its lease
outside the watched namespace allows cleanup to continue when that namespace
is being deleted. Load or publish the image where the cluster's container
runtime can reach it. For either deployment, apply
[config/example.yaml](config/example.yaml), edit `spec.values`, and delete the
object to exercise its lifecycle:

```sh
kubectl apply -f controllers/record/config/example.yaml
kubectl -n fleet-record-demo get fleetrecords
kubectl -n fleet-record-demo get fleetrecord example -o yaml
```

The detailed status shows the binding and generation-specific conditions.

## Consistency and recovery

The controller first persists its finalizer, then records a create UUID and the
original spec in Kubernetes status. Only then does it call Fleet. Every retry
uses that UUID and original payload until Fleet definitively records acceptance
or `AlreadyExists`. The Create request ID is the creation nonce for this attempt.
A source spec edit, timeout, deletion, or controller restart does not change the
unresolved request. Even a Fleet `GET` returning 404 is
insufficient to finalize while a create is unresolved: the original request
could still arrive later. If Kubernetes deletion starts with an unresolved
Create, the controller finishes resolving it first. A retry can be the first
request Fleet executes, so this can create a resource that is immediately deleted.
For providers with external effects this can start real provisioning. Unavailable
Fleet APIs or unusable credentials can keep the Kubernetes finalizer pending.
There is no creation-cancellation operation in this implementation.

An accepted Create replay returns the current incarnation. If that incarnation
has already been deleted, Fleet returns `NotFound` with a `google.rpc.ErrorInfo`
reason of `CREATION_RETIRED` in domain `open-cluster-management.io`. This specific
result also resolves the attempt. An ordinary Create `NotFound`, such as an
unavailable resource type, does not resolve it.

Fleet names include a digest of cluster ID, namespace, name, and Kubernetes UID.
Name alignment permits initial binding within this reserved range; the controller
then records Fleet's UID and refuses to modify or delete a replacement with a
different UID. An active object whose bound Fleet record disappears can start a
new create request after the previous one is resolved. Deletion retains the
finalizer until Fleet confirms absence, including its asynchronous cleanup.

Fresh Kubernetes reads and patches with resource-version preconditions protect
concurrent source edits and other finalizers. Fleet updates replace `spec` with
an etag precondition. A conflict causes a fresh read and retry. Lost update or
delete responses are recovered by observing Fleet's current state. No process
memory, cached etag, leader lease, or successful HTTP response alone is treated
as proof that both APIs converged.

The queue bounds concurrency and backs off transient failures. Periodic reads
observe Fleet status and repair drift, with stable jitter across objects. A
converged poll performs no writes. Watch predicates ignore status-only updates;
explicit requeues advance lifecycle. This first implementation watches one
namespace per manager, with four concurrent reconciliations and a 30-second
poll interval by default. Reads scale with the number of managed records; it
does not require a Fleet watch API.

`Synced=True` means Fleet accepted the Kubernetes spec at the condition's
`observedGeneration`. `Ready=True` additionally requires Fleet's current
generation to be observed, active, unpaused, and no longer reconciling. Readiness
means configuration acceptance by this provider. It does not establish health of
any external system. A condition for an older Kubernetes generation must not be
interpreted as readiness of a newly edited spec.

## Fleet changes and boundaries

Fleet now provides managed-resource `Update`, whole-spec field masks, conditional
update/delete using etags, compact creation decisions, and observed-generation
status. Intent and derived strategies commit atomically. Write transactions
serialize competing aggregate reads in PostgreSQL; SQLite already serializes
writers. A reconciliation that finishes after a newer edit retains delivery
history for cleanup while preserving newer lifecycle and credentials. Cleanup
is fenced by fulfillment identity.

Creation decisions are part of the extension-resource repository. They contain
only the request ID/nonce, request fingerprint, accepted resource UID (if any),
and retention deadline. Accepted identities remain while their incarnation is
live. Rejected attempts remain for **30 days after the decision**; accepted
attempts remain for **30 days after final deletion**. Retries do not extend these
deadlines. A periodic collector removes expired decisions in bounded batches;
Create also checks expiry when processing a retry. A request processed after the
window may create again. This bounds replay protection; it cannot fence an
arbitrarily delayed request. Clients must use a fresh UUID for each new logical
creation attempt and preserve the original payload while its outcome is unknown.
The name remains reusable with a fresh nonce during retention.

Update and Delete do not retain mutation receipts or expose `request_id`.
After an ambiguous write, clients read current state and retry with a fresh etag.
Before scheduling a write without a caller etag, Fleet captures an internal etag
so a replay cannot overwrite a newer mutation. Such a write can also return a
concurrency conflict. A server maintenance pass runs at startup and every 30
seconds, finding current generation gaps and unfinished deletions with paginated
queries. It restarts durable work independently of the original conditional
mutation, including after its activity retries are exhausted. Completed deletion
reconciliation is durable, so cleanup recovery does not require repeating an
external delete or recovering an old signal. Maintenance can notify a waiting
cleanup workflow again from the persisted completion state.

These are RM-side retry guarantees. The nonce is not yet bound to signed creation
evidence or enforced by target-side verifiers; that security mechanism and its
retention agreement remain separate work. External side effects still depend on
the provider's own durable execution and idempotency contracts.

Ownership is a convention over a reserved name range and recorded UID. It is
not a server-enforced owner reference, lease, or field-ownership mechanism.
Operators must protect Kubernetes status/finalizers, preserve its backing store,
keep the cluster ID stable, and reserve these names in Fleet. Force-removing a
finalizer or losing source state can orphan Fleet records. There is no orphan
scanner, cross-cluster ownership transfer, automatic endpoint migration, or
bidirectional synchronization in this controller.

The provider completes validation and reporting synchronously, with no
background operation or external side effect. Durable activity replay can repeat
it safely after a process failure. This does not establish recovery guarantees
for other addons' asynchronous work or make an arbitrary external provider
production-ready. The controller uses authenticated token delivery; generating
detached user signatures for Kubernetes-originated intent is outside its API.
Fleet's update API preserves signed-intent requirements for other clients.

This change does not implement ManifestWork or add Deployment updates. ManifestWork
needs a separate semantic mapping and coverage for apply modes, deletion options,
resource feedback, and status before it can make equivalent guarantees.

## Verify

```sh
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run record-controller:verify
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run record-controller:test-integration
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:verify
NX_DEFAULT_OUTPUT_STYLE=summary npx nx run server:test-integration
```

Normal controller tests exercise the real Fleet HTTP API, SQLite, and durable
workflow engine alongside Kubernetes' reusable in-memory client. Fault injection
holds an actual create request until after finalization to check late-request
safety. Integration tests use a real Kubernetes API server to check the CRD,
status subresource, optimistic concurrency, watches, and finalizers. They require
envtest binaries (or download the pinned version); server integration requires
Podman. No test uses a fixed sleep to decide that reconciliation has completed.
