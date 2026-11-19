// Package v1alpha1 defines the Kubernetes API for Fleet configuration records.
package v1alpha1

import (
	"maps"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the served Kubernetes API version.
var GroupVersion = schema.GroupVersion{Group: "records.open-cluster-management.io", Version: "v1alpha1"}

// FleetRecord owns the complete spec of one UID-qualified Fleet record.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type FleetRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FleetRecordSpec   `json:"spec"`
	Status            FleetRecordStatus `json:"status,omitempty"`
}

// FleetRecordSpec contains the desired configuration. Empty values clear it.
type FleetRecordSpec struct {
	Values map[string]string `json:"values,omitempty"`
}

// FleetRecordStatus records the binding and the latest observed convergence.
// CreateSpec and CreateRequestID are a write-ahead record: they must survive
// restarts and spec edits until Fleet definitively resolves the create request.
type FleetRecordStatus struct {
	FleetName               string           `json:"fleetName,omitempty"`
	FleetUID                string           `json:"fleetUID,omitempty"`
	CreateRequestID         string           `json:"createRequestID,omitempty"`
	CreateSpec              *FleetRecordSpec `json:"createSpec,omitempty"`
	CreateConfirmed         bool             `json:"createConfirmed,omitempty"`
	ObservedGeneration      int64            `json:"observedGeneration,omitempty"`
	FleetGeneration         int64            `json:"fleetGeneration,omitempty"`
	FleetObservedGeneration int64            `json:"fleetObservedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FleetRecordList is a list of configuration records.
// +kubebuilder:object:root=true
type FleetRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FleetRecord `json:"items"`
}

// AddToScheme registers this API with a Kubernetes runtime scheme.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &FleetRecord{}, &FleetRecordList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// DeepCopy returns an independent record, including pending request state.
func (r *FleetRecord) DeepCopy() *FleetRecord {
	if r == nil {
		return nil
	}
	out := new(FleetRecord)
	r.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver into independently owned output.
func (r *FleetRecord) DeepCopyInto(out *FleetRecord) {
	*out = *r
	r.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.Values = maps.Clone(r.Spec.Values)
	out.Status.Conditions = slices.Clone(r.Status.Conditions)
	if r.Status.CreateSpec != nil {
		out.Status.CreateSpec = &FleetRecordSpec{Values: maps.Clone(r.Status.CreateSpec.Values)}
	}
}

// DeepCopyObject implements runtime.Object.
func (r *FleetRecord) DeepCopyObject() runtime.Object {
	if r == nil {
		return nil
	}
	return r.DeepCopy()
}

// DeepCopyObject implements runtime.Object.
func (r *FleetRecordList) DeepCopyObject() runtime.Object {
	if r == nil {
		return nil
	}
	out := new(FleetRecordList)
	r.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the list and its independently owned records.
func (r *FleetRecordList) DeepCopyInto(out *FleetRecordList) {
	*out = *r
	r.ListMeta.DeepCopyInto(&out.ListMeta)
	out.Items = make([]FleetRecord, len(r.Items))
	for i := range r.Items {
		out.Items[i] = *r.Items[i].DeepCopy()
	}
}
