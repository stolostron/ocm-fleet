//go:build integration

package controller_test

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	api "github.com/stolostron/ocm-fleet/controllers/record/api/v1alpha1"
	"github.com/stolostron/ocm-fleet/controllers/record/controller"
	"github.com/stolostron/ocm-fleet/controllers/record/fleet"
	"github.com/stolostron/ocm-fleet/server/testserver"
)

func TestIntegration_KubernetesWatchRBACAndFinalizers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	controlPlane := &envtest.Environment{
		CRDDirectoryPaths:           []string{filepath.Join("..", "config", "crd")},
		ErrorIfCRDPathMissing:       true,
		DownloadBinaryAssets:        os.Getenv("KUBEBUILDER_ASSETS") == "",
		DownloadBinaryAssetsVersion: "1.36.0",
		BinaryAssetsDirectory:       filepath.Join(os.TempDir(), "ocm-fleet-envtest"),
	}
	cfg, err := controlPlane.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := controlPlane.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, corev1.AddToScheme, rbacv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	kube, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	namespace := "fleet-record-demo"
	leaderNamespace := "fleet-record-system"
	if err := kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
		t.Fatal(err)
	}
	if err := kube.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: leaderNamespace}}); err != nil {
		t.Fatal(err)
	}
	applyRBAC(t, ctx, kube)
	user, err := controlPlane.AddUser(envtest.User{Name: "system:serviceaccount:" + leaderNamespace + ":fleet-record-controller", Groups: []string{"system:serviceaccounts", "system:serviceaccounts:" + leaderNamespace, "system:authenticated"}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := ctrl.NewManager(user.Config(), ctrl.Options{
		Scheme: scheme, Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: "0",
		LeaderElection: true, LeaderElectionNamespace: leaderNamespace, LeaderElectionID: "fleet-record-integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := fleet.NewClient(testserver.StartRecords(t), &http.Client{Timeout: 10 * time.Second}, func(context.Context) (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	reconciler, err := controller.NewReconciler(manager.GetClient(), manager.GetAPIReader(), remote, "integration-cluster", controller.WithPollInterval(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := reconciler.SetupWithManager(manager, 2); err != nil {
		t.Fatal(err)
	}
	managerCtx, stopManager := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- manager.Start(managerCtx) }()
	t.Cleanup(func() {
		stopManager()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("manager did not stop")
		}
	})
	events, err := kube.Watch(ctx, &api.FleetRecordList{}, client.InNamespace(namespace))
	if err != nil {
		t.Fatal(err)
	}
	defer events.Stop()
	record := &api.FleetRecord{ObjectMeta: metav1.ObjectMeta{Name: "integration", Namespace: namespace, Finalizers: []string{"test.example.io/other-owner"}}, Spec: api.FleetRecordSpec{Values: map[string]string{"message": "first"}}}
	if err := kube.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	ready := awaitObject(t, ctx, events, func(r *api.FleetRecord) bool {
		return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") && r.Status.ObservedGeneration == 1
	})
	stale := ready.DeepCopy()
	before := ready.DeepCopy()
	ready.Spec.Values = map[string]string{"message": "second"}
	if err := kube.Patch(ctx, ready, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		t.Fatal(err)
	}
	if ready.Generation != 2 {
		t.Fatalf("API server did not advance source generation: %d", ready.Generation)
	}
	staleBefore := stale.DeepCopy()
	stale.Status.FleetUID = "must-not-overwrite"
	if err := kube.Status().Patch(ctx, stale, client.MergeFromWithOptions(staleBefore, client.MergeFromWithOptimisticLock{})); !apierrors.IsConflict(err) {
		t.Fatalf("stale status write should conflict: %v", err)
	}
	ready = awaitObject(t, ctx, events, func(r *api.FleetRecord) bool {
		return meta.IsStatusConditionTrue(r.Status.Conditions, "Ready") && r.Status.ObservedGeneration == 2
	})
	current, err := remote.Get(ctx, ready.Status.FleetName)
	if err != nil || !maps.Equal(current.Spec.Values, ready.Spec.Values) {
		t.Fatalf("Fleet did not follow watched edit: %+v, %v", current, err)
	}
	if err := kube.Delete(ctx, ready); err != nil {
		t.Fatal(err)
	}
	finalized := awaitObject(t, ctx, events, func(r *api.FleetRecord) bool {
		return !r.DeletionTimestamp.IsZero() && !slices.Contains(r.Finalizers, controller.Finalizer)
	})
	if !slices.Contains(finalized.Finalizers, "test.example.io/other-owner") {
		t.Fatal("controller removed another owner's finalizer")
	}
	if _, err := remote.Get(ctx, ready.Status.FleetName); !fleet.IsNotFound(err) {
		t.Fatalf("Kubernetes finalized before Fleet cleanup: %v", err)
	}
}

func awaitObject(t *testing.T, ctx context.Context, events watch.Interface, predicate func(*api.FleetRecord) bool) *api.FleetRecord {
	t.Helper()
	for {
		select {
		case event, ok := <-events.ResultChan():
			if !ok {
				t.Fatal("Kubernetes watch ended before convergence")
			}
			if event.Type == watch.Error {
				t.Fatalf("Kubernetes watch error: %+v", event.Object)
			}
			if record, ok := event.Object.(*api.FleetRecord); ok && predicate(record) {
				return record.DeepCopy()
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func applyRBAC(t *testing.T, ctx context.Context, kube client.Client) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "config", "rbac.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var object unstructured.Unstructured
		if err := decoder.Decode(&object); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		if err := kube.Create(ctx, &object); err != nil {
			t.Fatal(err)
		}
	}
}
