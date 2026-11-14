package controller_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	api "github.com/stolostron/ocm-fleet/controllers/record/api/v1alpha1"
	"github.com/stolostron/ocm-fleet/controllers/record/controller"
	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
	"github.com/stolostron/ocm-fleet/server/testserver"
)

func TestReconcile_LifecycleAndDrift(t *testing.T) {
	env := setup(t, nil)
	ready := env.drive(func(r *api.FleetRecord) bool { return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") })
	if ready.Status.FleetUID == "" || !ready.Status.CreateConfirmed {
		t.Fatal("binding was not recorded")
	}
	current, err := env.remote.Get(env.ctx, ready.Status.FleetName)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(current.Spec.Values, ready.Spec.Values) {
		t.Fatal("spec did not converge")
	}
	// Polling a converged record must not write either API's spec or status.
	beforeVersion, beforeGeneration := ready.ResourceVersion, current.Generation
	env.step()
	if got := env.get(); got.ResourceVersion != beforeVersion {
		t.Fatal("unchanged poll wrote Kubernetes status")
	}
	current, err = env.remote.Get(env.ctx, ready.Status.FleetName)
	if err != nil || current.Generation != beforeGeneration {
		t.Fatalf("unchanged poll wrote Fleet: %+v, %v", current, err)
	}
	ready.Spec.Values = map[string]string{"message": "edited in Kubernetes"}
	ready.Generation++ // The real API server supplies this; the reusable fake does not.
	if err := env.kube.Update(env.ctx, ready); err != nil {
		t.Fatal(err)
	}
	ready = env.drive(func(r *api.FleetRecord) bool {
		return r.Status.ObservedGeneration == 2 && meta.IsStatusConditionTrue(r.Status.Conditions, "Ready")
	})
	current, err = env.remote.Get(env.ctx, ready.Status.FleetName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.remote.Update(env.ctx, current, fleet.Spec{Values: map[string]string{"message": "external drift"}}); err != nil {
		t.Fatal(err)
	}
	for {
		env.step()
		current, err = env.remote.Get(env.ctx, ready.Status.FleetName)
		if err != nil {
			t.Fatal(err)
		}
		if maps.Equal(current.Spec.Values, ready.Spec.Values) && current.Ready() {
			break
		}
	}
	// Within the replay window a resolved create stays sealed. Repair needs
	// a new request ID and records a new Fleet incarnation at the same name.
	if err := env.remote.Delete(env.ctx, current); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := env.remote.Get(env.ctx, current.Name)
		if fleet.IsNotFound(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	oldUID, oldRequestID := ready.Status.FleetUID, ready.Status.CreateRequestID
	ready = env.drive(func(r *api.FleetRecord) bool {
		return r.Status.FleetUID != oldUID && meta.IsStatusConditionTrue(r.Status.Conditions, "Ready")
	})
	if ready.Status.CreateRequestID == oldRequestID {
		t.Fatal("repair reused a sealed create request")
	}
	if err := env.kube.Delete(env.ctx, env.get()); err != nil {
		t.Fatal(err)
	}
	env.driveDeleted()
	if _, err := env.remote.Get(env.ctx, ready.Status.FleetName); !fleet.IsNotFound(err) {
		t.Fatalf("Fleet record survived finalization: %v", err)
	}
}

func TestReconcile_ConcurrentEditsConvergeWithoutStaleStatus(t *testing.T) {
	var beforePatch func() error
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPatch && beforePatch != nil {
			fn := beforePatch
			beforePatch = nil
			if err := fn(); err != nil {
				return nil, err
			}
		}
		return http.DefaultTransport.RoundTrip(req)
	})
	env := setup(t, transport)
	ready := env.drive(func(r *api.FleetRecord) bool { return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") })
	ready.Spec.Values = map[string]string{"message": "intermediate"}
	ready.Generation++
	if err := env.kube.Update(env.ctx, ready); err != nil {
		t.Fatal(err)
	}
	beforePatch = func() error {
		source := env.get()
		source.Spec.Values = map[string]string{"message": "latest"}
		source.Generation++
		if err := env.kube.Update(env.ctx, source); err != nil {
			return err
		}
		current, err := env.remote.Get(env.ctx, ready.Status.FleetName)
		if err != nil {
			return err
		}
		_, err = env.remote.Update(env.ctx, current, fleet.Spec{Values: map[string]string{"message": "competing Fleet write"}})
		return err
	}
	if _, err := env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key}); !apierrors.IsConflict(err) {
		t.Fatalf("stale source snapshot must not update status: %v", err)
	}
	ready = env.drive(func(r *api.FleetRecord) bool {
		return r.Status.ObservedGeneration == 3 && meta.IsStatusConditionTrue(r.Status.Conditions, "Ready")
	})
	current, err := env.remote.Get(env.ctx, ready.Status.FleetName)
	if err != nil || current.Spec.Values["message"] != "latest" {
		t.Fatalf("concurrent edits did not converge: %+v, %v", current, err)
	}
}

func TestReconcile_UnavailableRouteRetainsFinalizer(t *testing.T) {
	unavailable := false
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if unavailable {
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("404 page not found")), Request: req}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})
	env := setup(t, transport)
	ready := env.drive(func(r *api.FleetRecord) bool { return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") })
	if err := env.kube.Delete(env.ctx, ready); err != nil {
		t.Fatal(err)
	}
	unavailable = true
	if _, err := env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key}); err == nil {
		t.Fatal("missing route was treated as confirmed absence")
	}
	if !slices.Contains(env.get().Finalizers, controller.Finalizer) {
		t.Fatal("unavailable Fleet route released the finalizer")
	}
	unavailable = false
	env.driveDeleted()
}

func TestReconcile_CreateNotFoundDoesNotResolveUnknownAttempt(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":5,"message":"resource type temporarily unavailable"}`)), Request: req}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	})
	env := setup(t, transport)
	env.step() // finalizer
	env.step() // write-ahead Create
	_, _ = env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key})
	if env.get().Status.CreateConfirmed {
		t.Fatal("an ordinary NotFound does not prove the creation nonce was retired")
	}
	if err := env.kube.Delete(env.ctx, env.get()); err != nil {
		t.Fatal(err)
	}
	_, _ = env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key})
	if !slices.Contains(env.get().Finalizers, controller.Finalizer) {
		t.Fatal("an ambiguous Create must retain the finalizer")
	}
}

func TestReconcile_UnknownCreateThenDeletionAndLateRequest(t *testing.T) {
	gate := &delayedCreate{release: make(chan struct{}), done: make(chan error, 1)}
	env := setup(t, gate)
	defer gate.unblock()
	env.step() // finalizer
	env.step() // durable create request and spec snapshot
	if _, err := env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key}); err == nil {
		t.Fatal("expected ambiguous create failure")
	}
	pending := env.get()
	if pending.Status.CreateConfirmed || pending.Status.CreateRequestID == "" || pending.Status.CreateSpec == nil {
		t.Fatal("unresolved create was not retained")
	}
	if _, err := env.remote.Get(env.ctx, pending.Status.FleetName); !fleet.IsNotFound(err) {
		t.Fatalf("gated create should not exist yet: %v", err)
	}
	// A concurrent spec edit must not alter the payload used to seal the
	// unresolved create, even when the object is already being deleted.
	pending.Spec.Values = map[string]string{"message": "later"}
	pending.Generation++
	if err := env.kube.Update(env.ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := env.kube.Delete(env.ctx, pending); err != nil {
		t.Fatal(err)
	}
	// Construct a new reconciler to prove recovery uses only persisted state.
	env.reconciler = newReconciler(t, env.kube, env.remote)
	env.driveDeleted()
	gate.unblock()
	select {
	case err := <-gate.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-env.ctx.Done():
		t.Fatal(env.ctx.Err())
	}
	if _, err := env.remote.Get(env.ctx, pending.Status.FleetName); !fleet.IsNotFound(err) {
		t.Fatalf("late create resurrected deleted intent: %v", err)
	}
}

func TestReconcile_ConflictedCreateStaysRejectedAfterAdoptionAndDeletion(t *testing.T) {
	gate := &delayedCreate{release: make(chan struct{}), done: make(chan error, 1)}
	env := setup(t, gate)
	defer gate.unblock()
	env.step() // finalizer
	env.step() // write-ahead N2
	pending := env.get()
	gate.requestID = pending.Status.CreateRequestID
	// A separately created incarnation N1 occupies the reserved name before
	// N2 is attempted. Only N2's first transport copy is held back.
	existing, err := env.remote.Create(env.ctx, pending.Status.FleetName, uuid.NewString(), fleet.Spec{Values: map[string]string{"message": "existing"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.reconciler.Reconcile(env.ctx, ctrl.Request{NamespacedName: env.key}); err == nil {
		t.Fatal("expected the ambiguous first N2 request")
	}
	env.step() // N2 retry durably conflicts, then adopts N1 by name
	bound := env.get()
	if !bound.Status.CreateConfirmed || bound.Status.FleetUID != existing.UID {
		t.Fatal("N2 was not resolved before binding N1")
	}
	if err := env.kube.Delete(env.ctx, bound); err != nil {
		t.Fatal(err)
	}
	env.driveDeleted()
	gate.unblock()
	select {
	case err := <-gate.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-env.ctx.Done():
		t.Fatal(env.ctx.Err())
	}
	if _, err := env.remote.Get(env.ctx, pending.Status.FleetName); !fleet.IsNotFound(err) {
		t.Fatalf("the delayed, rejected N2 created an orphan: %v", err)
	}
}

func TestReconcile_ReplacementUIDIsNeverMutatedOrDeleted(t *testing.T) {
	env := setup(t, nil)
	bound := env.drive(func(r *api.FleetRecord) bool { return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") })
	current, err := env.remote.Get(env.ctx, bound.Status.FleetName)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.remote.Delete(env.ctx, current); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := env.remote.Get(env.ctx, current.Name)
		if fleet.IsNotFound(err) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	replacement, err := env.remote.Create(env.ctx, current.Name, uuid.NewString(), fleet.Spec{Values: map[string]string{"owner": "someone else"}})
	if err != nil {
		t.Fatal(err)
	}
	env.step()
	condition := meta.FindStatusCondition(env.get().Status.Conditions, "Synced")
	if condition == nil || condition.Reason != "IdentityConflict" || condition.Status != metav1.ConditionFalse {
		t.Fatalf("replacement not reported: %+v", condition)
	}
	if err := env.kube.Delete(env.ctx, env.get()); err != nil {
		t.Fatal(err)
	}
	env.step()
	got, err := env.remote.Get(env.ctx, replacement.Name)
	if err != nil || got.UID != replacement.UID || !maps.Equal(got.Spec.Values, replacement.Spec.Values) {
		t.Fatalf("replacement changed: %+v, %v", got, err)
	}
	if len(env.get().Finalizers) == 0 {
		t.Fatal("identity conflict must retain the finalizer")
	}
}

func TestFleetNameIncludesEveryIdentityBoundary(t *testing.T) {
	r := &api.FleetRecord{ObjectMeta: metav1.ObjectMeta{Namespace: "one", Name: "same", UID: "first"}}
	base := controller.FleetName("cluster-a", r)
	if base != controller.FleetName("cluster-a", r.DeepCopy()) {
		t.Fatal("name is not stable")
	}
	variants := []string{controller.FleetName("cluster-b", r)}
	for _, mutate := range []func(*api.FleetRecord){func(r *api.FleetRecord) { r.Namespace = "two" }, func(r *api.FleetRecord) { r.Name = "different" }, func(r *api.FleetRecord) { r.UID = "second" }} {
		copy := r.DeepCopy()
		mutate(copy)
		variants = append(variants, controller.FleetName("cluster-a", copy))
	}
	for _, name := range variants {
		if name == base {
			t.Fatalf("identity collision: %s", name)
		}
	}
}

type environment struct {
	t          *testing.T
	ctx        context.Context
	kube       client.Client
	remote     *fleet.Client
	reconciler *controller.Reconciler
	key        types.NamespacedName
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func setup(t *testing.T, transport http.RoundTripper) *environment {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	r := &api.FleetRecord{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "example", UID: types.UID(uuid.NewString()), Generation: 1}, Spec: api.FleetRecordSpec{Values: map[string]string{"message": "hello"}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.FleetRecord{}).WithObjects(r).Build()
	remote, err := fleet.NewClient(testserver.StartRecords(t), &http.Client{Transport: transport, Timeout: 10 * time.Second}, func(context.Context) (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	return &environment{t: t, ctx: ctx, kube: kube, remote: remote, reconciler: newReconciler(t, kube, remote), key: client.ObjectKeyFromObject(r)}
}

func newReconciler(t *testing.T, kube client.Client, remote *fleet.Client) *controller.Reconciler {
	t.Helper()
	r, err := controller.NewReconciler(kube, kube, remote, "test-cluster")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (e *environment) step() {
	e.t.Helper()
	if _, err := e.reconciler.Reconcile(e.ctx, ctrl.Request{NamespacedName: e.key}); err != nil && !apierrors.IsConflict(err) {
		e.t.Fatal(err)
	}
}

func (e *environment) get() *api.FleetRecord {
	e.t.Helper()
	var r api.FleetRecord
	if err := e.kube.Get(e.ctx, e.key, &r); err != nil {
		e.t.Fatal(err)
	}
	return &r
}

func (e *environment) drive(predicate func(*api.FleetRecord) bool) *api.FleetRecord {
	e.t.Helper()
	for {
		if err := e.ctx.Err(); err != nil {
			e.t.Fatal(err)
		}
		e.step()
		r := e.get()
		if predicate(r) {
			return r
		}
	}
}

func (e *environment) driveDeleted() {
	e.t.Helper()
	for {
		if err := e.ctx.Err(); err != nil {
			e.t.Fatal(err)
		}
		e.step()
		var r api.FleetRecord
		if err := e.kube.Get(e.ctx, e.key, &r); apierrors.IsNotFound(err) {
			return
		} else if err != nil {
			e.t.Fatal(err)
		}
	}
}

// delayedCreate is a transport fault injector, not an alternate Fleet
// implementation. It releases an actual HTTP request after finalization, which
// exercises Fleet's retained creation decision rather than counting calls.
type delayedCreate struct {
	once        sync.Once
	releaseOnce sync.Once
	release     chan struct{}
	done        chan error
	requestID   string
}

func (g *delayedCreate) unblock() { g.releaseOnce.Do(func() { close(g.release) }) }

func (g *delayedCreate) RoundTrip(req *http.Request) (*http.Response, error) {
	deferred := false
	if req.Method == http.MethodPost && req.URL.Query().Get("record_id") != "" && (g.requestID == "" || req.URL.Query().Get("request_id") == g.requestID) {
		g.once.Do(func() { deferred = true })
	}
	if !deferred {
		return http.DefaultTransport.RoundTrip(req)
	}
	clone := req.Clone(context.WithoutCancel(req.Context()))
	var err error
	clone.Body, err = req.GetBody()
	if err != nil {
		return nil, err
	}
	go func() {
		<-g.release
		ctx, cancel := context.WithTimeout(clone.Context(), 10*time.Second)
		defer cancel()
		resp, err := http.DefaultTransport.RoundTrip(clone.WithContext(ctx))
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		g.done <- err
	}()
	return nil, errors.New("injected lost create response")
}
