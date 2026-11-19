package controller

import (
	"fmt"
	"slices"
	"time"

	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	controlleroptions "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/stolostron/ocm-fleet/controllers/record/api/v1alpha1"
)

// SetupWithManager watches intent and lifecycle changes. Status writes do not
// enqueue themselves; explicit requeues advance lifecycle and poll Fleet.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager, concurrency int) error {
	if concurrency < 1 {
		return fmt.Errorf("controller concurrency must be positive")
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&api.FleetRecord{}).
		WithEventFilter(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
				!e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp()) ||
				!slices.Equal(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers())
		}}).
		WithOptions(controlleroptions.Options{
			MaxConcurrentReconciles: concurrency,
			RateLimiter:             workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](time.Second, time.Minute),
		}).Complete(r)
}
