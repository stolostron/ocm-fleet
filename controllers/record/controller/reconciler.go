// Package controller reconciles Kubernetes record intent into Fleet.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	api "github.com/stolostron/ocm-fleet/controllers/record/api/v1alpha1"
	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
)

// Finalizer keeps Kubernetes intent until all possible creates are resolved
// and Fleet confirms absence of the bound resource incarnation.
const Finalizer = "records.open-cluster-management.io/fleet-cleanup"

// Fleet is the remote managed-resource API. Conditional mutations and retained
// creation decisions are required server contracts, not client-side assumptions.
type Fleet interface {
	Get(context.Context, string) (fleet.Record, error)
	Create(context.Context, string, string, fleet.Spec) (fleet.Record, error)
	Update(context.Context, fleet.Record, fleet.Spec) (fleet.Record, error)
	Delete(context.Context, fleet.Record) error
}

// Reconciler has no process-local lifecycle state. Fresh Kubernetes reads and
// optimistic patches protect concurrent spec edits, status writes, finalizers,
// and overlapping reconciles during leader transitions. Fleet etags fence stale
// remote writes. Polling repairs drift and observes Fleet's asynchronous work.
type Reconciler struct {
	client          client.Client
	reader          client.Reader
	fleet           Fleet
	sourceClusterID string
	pollInterval    time.Duration
	requestTimeout  time.Duration
	now             func() time.Time
}

// Option configures operational bounds without changing ownership semantics.
type Option func(*Reconciler)

// WithPollInterval sets the drift and status refresh interval (default 30s).
func WithPollInterval(interval time.Duration) Option {
	return func(r *Reconciler) { r.pollInterval = interval }
}

// WithRequestTimeout bounds each reconciliation's I/O (default 15s).
func WithRequestTimeout(timeout time.Duration) Option {
	return func(r *Reconciler) { r.requestTimeout = timeout }
}

// NewReconciler requires a direct, uncached API reader. The source cluster ID
// must be stable across restarts and unique among clusters sharing Fleet.
func NewReconciler(kube client.Client, reader client.Reader, remote Fleet, sourceClusterID string, opts ...Option) (*Reconciler, error) {
	if kube == nil || reader == nil || remote == nil || strings.TrimSpace(sourceClusterID) == "" {
		return nil, fmt.Errorf("kubernetes client, direct reader, Fleet client, and stable source cluster ID are required")
	}
	r := &Reconciler{client: kube, reader: reader, fleet: remote, sourceClusterID: sourceClusterID, pollInterval: 30 * time.Second, requestTimeout: 15 * time.Second, now: time.Now}
	for _, opt := range opts {
		opt(r)
	}
	if r.pollInterval < time.Second || r.requestTimeout <= 0 {
		return nil, fmt.Errorf("poll interval must be at least 1s and request timeout must be positive")
	}
	return r, nil
}

// FleetName reserves one name per Kubernetes incarnation, including cluster,
// namespace, name, and UID. The JSON tuple avoids ambiguous concatenation. A
// 224-bit digest fits a DNS label; no user-controlled path segments reach Fleet.
func FleetName(sourceClusterID string, record *api.FleetRecord) string {
	identity, _ := json.Marshal([]string{sourceClusterID, record.Namespace, record.Name, string(record.UID)})
	hash := sha256.Sum256(identity)
	return fmt.Sprintf("records/fcr-%x", hash[:28])
}

// Reconcile performs bounded, idempotent progress. Every persisted lifecycle
// decision precedes the remote operation it authorizes.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	var record api.FleetRecord
	if err := r.reader.Get(ctx, request.NamespacedName, &record); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	deleting := !record.DeletionTimestamp.IsZero()
	if !controllerutil.ContainsFinalizer(&record, Finalizer) {
		if deleting {
			return ctrl.Result{}, nil
		}
		before := record.DeepCopy()
		controllerutil.AddFinalizer(&record, Finalizer)
		return soon(), r.client.Patch(ctx, &record, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	name := FleetName(r.sourceClusterID, &record)
	if record.UID == "" || (record.Status.FleetName != "" && record.Status.FleetName != name) {
		return r.blocked(ctx, &record, "IdentityConflict", "The source identity or cluster ID differs from the persisted Fleet binding")
	}
	if record.Status.CreateRequestID == "" {
		// With no write-ahead request, no Fleet call has been authorized.
		if deleting {
			return r.finalize(ctx, &record)
		}
		before := record.DeepCopy()
		record.Status.FleetName = name
		record.Status.CreateRequestID = uuid.NewString()
		record.Status.CreateSpec = &api.FleetRecordSpec{Values: maps.Clone(record.Spec.Values)}
		r.conditions(&record, false, false, "Creating", "Waiting for Fleet to resolve the recorded create request")
		return soon(), r.patchStatus(ctx, before, &record)
	}
	if !record.Status.CreateConfirmed {
		return r.resolveCreate(ctx, &record)
	}
	current, err := r.fleet.Get(ctx, name)
	if fleet.IsNotFound(err) {
		if deleting {
			return r.finalize(ctx, &record)
		}
		// The prior create is sealed. A new request can safely repair a
		// manually deleted resource without reopening the old request.
		before := record.DeepCopy()
		record.Status.CreateRequestID = uuid.NewString()
		record.Status.CreateSpec = &api.FleetRecordSpec{Values: maps.Clone(record.Spec.Values)}
		record.Status.CreateConfirmed = false
		record.Status.FleetUID = ""
		record.Status.FleetGeneration = 0
		record.Status.FleetObservedGeneration = 0
		r.conditions(&record, false, false, "Creating", "Recreating the missing Fleet record")
		return soon(), r.patchStatus(ctx, before, &record)
	}
	if err != nil {
		return r.failed(ctx, &record, err)
	}
	if current.Name != name || current.UID != record.Status.FleetUID {
		return r.blocked(ctx, &record, "IdentityConflict", "The Fleet name refers to a different resource UID; restore the binding before proceeding")
	}
	if deleting {
		return r.delete(ctx, &record, current)
	}
	if current.State == "DELETING" {
		return r.observe(ctx, &record, current, false, "Deleting", "Waiting for Fleet deletion before recreating intent")
	}
	if !maps.Equal(current.Spec.Values, record.Spec.Values) {
		updated, err := r.fleet.Update(ctx, current, fleet.Spec{Values: record.Spec.Values})
		if err != nil {
			return r.failed(ctx, &record, err)
		}
		current = updated
	}
	return r.observe(ctx, &record, current, true, "Accepted", "Fleet accepted the current Kubernetes spec")
}

func (r *Reconciler) resolveCreate(ctx context.Context, record *api.FleetRecord) (ctrl.Result, error) {
	if record.Status.CreateSpec == nil {
		return r.blocked(ctx, record, "InvalidBinding", "The unresolved create has no persisted spec snapshot")
	}
	created, err := r.fleet.Create(ctx, record.Status.FleetName, record.Status.CreateRequestID, fleet.Spec{Values: record.Status.CreateSpec.Values})
	if fleet.IsCreationRetired(err) {
		// A retained accepted decision can outlive its incarnation. This is
		// a definitive Create result, unlike absence observed by an ordinary
		// Get while the Create is still unresolved. A route-level 404 is not
		// classified as Fleet NotFound by the client.
		created = fleet.Record{}
		err = nil
	}
	if fleet.IsAlreadyExists(err) {
		// Name alignment permits initial adoption within this controller's
		// reserved name range. Once bound, every later UID must match.
		created, err = r.fleet.Get(ctx, record.Status.FleetName)
		if fleet.IsNotFound(err) {
			created = fleet.Record{}
			err = nil
		}
	}
	if err != nil {
		return r.failed(ctx, record, err)
	}
	if created.Name != "" && created.Name != record.Status.FleetName {
		return r.blocked(ctx, record, "IdentityConflict", "Fleet returned a different resource name")
	}
	before := record.DeepCopy()
	record.Status.CreateConfirmed = true
	record.Status.CreateSpec = nil
	record.Status.FleetUID = created.UID
	r.conditions(record, false, false, "Bound", "The create request is resolved; refreshing Fleet state")
	return soon(), r.patchStatus(ctx, before, record)
}

func (r *Reconciler) delete(ctx context.Context, record *api.FleetRecord, current fleet.Record) (ctrl.Result, error) {
	// Reissuing Delete is safe and refreshes credentials on a paused delete.
	// An in-flight delete at this generation requires no additional mutation.
	if current.State != "DELETING" || current.PauseReason != "" {
		if err := r.fleet.Delete(ctx, current); err != nil && !fleet.IsNotFound(err) {
			return r.failed(ctx, record, err)
		}
	}
	return r.observe(ctx, record, current, false, "Deleting", "Waiting for Fleet to confirm deletion")
}

func (r *Reconciler) finalize(ctx context.Context, record *api.FleetRecord) (ctrl.Result, error) {
	before := record.DeepCopy()
	controllerutil.RemoveFinalizer(record, Finalizer)
	return ctrl.Result{}, client.IgnoreNotFound(r.client.Patch(ctx, record, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})))
}

func (r *Reconciler) observe(ctx context.Context, record *api.FleetRecord, current fleet.Record, synced bool, reason, message string) (ctrl.Result, error) {
	before := record.DeepCopy()
	record.Status.FleetGeneration = current.Generation
	record.Status.FleetObservedGeneration = current.ObservedGeneration
	ready := synced && current.Ready()
	r.conditions(record, synced, ready, reason, message)
	if synced && !ready {
		readyReason, readyMessage := "Reconciling", "Fleet has not finished the current generation"
		if current.PauseReason != "" {
			readyReason, readyMessage = "Paused", current.PauseReason
		}
		if current.State == "FAILED" {
			readyReason, readyMessage = "Failed", current.StatusReason
		}
		r.condition(record, "Ready", false, readyReason, readyMessage)
	}
	return r.poll(record), r.patchStatus(ctx, before, record)
}

func (r *Reconciler) blocked(ctx context.Context, record *api.FleetRecord, reason, message string) (ctrl.Result, error) {
	before := record.DeepCopy()
	r.conditions(record, false, false, reason, message)
	return r.poll(record), r.patchStatus(ctx, before, record)
}

func (r *Reconciler) failed(ctx context.Context, record *api.FleetRecord, err error) (ctrl.Result, error) {
	if fleet.IsConflict(err) || fleet.IsNotFound(err) {
		_, patchErr := r.blocked(ctx, record, "ConcurrentChange", "Fleet changed during reconciliation; refreshing its identity and etag")
		return ctrl.Result{RequeueAfter: time.Second}, patchErr
	}
	result, patchErr := r.blocked(ctx, record, "FleetUnavailable", err.Error())
	if patchErr != nil {
		return ctrl.Result{}, errors.Join(err, patchErr)
	}
	var apiErr *fleet.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		return result, nil
	}
	return ctrl.Result{}, err // Workqueue rate limiting bounds transient retries.
}

func (r *Reconciler) conditions(record *api.FleetRecord, synced, ready bool, reason, message string) {
	record.Status.ObservedGeneration = record.Generation
	r.condition(record, "Synced", synced, reason, message)
	r.condition(record, "Ready", ready, reason, message)
}

func (r *Reconciler) condition(record *api.FleetRecord, conditionType string, positive bool, reason, message string) {
	status := metav1.ConditionFalse
	if positive {
		status = metav1.ConditionTrue
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	meta.SetStatusCondition(&record.Status.Conditions, metav1.Condition{Type: conditionType, Status: status, Reason: reason, Message: message, ObservedGeneration: record.Generation, LastTransitionTime: metav1.NewTime(r.now())})
}

func (r *Reconciler) patchStatus(ctx context.Context, before, after *api.FleetRecord) error {
	if reflect.DeepEqual(before.Status, after.Status) {
		return nil
	}
	err := r.client.Status().Patch(ctx, after, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *Reconciler) poll(record *api.FleetRecord) ctrl.Result {
	// Stable jitter spreads periodic reads without process-local randomness,
	// making both failover behavior and reconciliation tests reproducible.
	hash := sha256.Sum256([]byte(record.UID))
	return ctrl.Result{RequeueAfter: r.pollInterval + r.pollInterval*time.Duration(hash[0])/2550}
}

func soon() ctrl.Result { return ctrl.Result{RequeueAfter: time.Millisecond} }
