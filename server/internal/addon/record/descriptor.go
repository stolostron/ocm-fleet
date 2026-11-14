// Package record provides managed configuration records. The provider accepts
// validated intent synchronously and has no external state to recover.
package record

import (
	_ "embed"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

const (
	// ResourceType is the Fleet managed record type.
	ResourceType domain.ResourceType = "records.open-cluster-management.io/Record"
	// TargetID is the registered, in-process record provider.
	TargetID domain.TargetID = "record-local"
	// TargetType routes record delivery to this provider.
	TargetType domain.TargetType = "record"
	// ManifestType identifies a managed-resource spec envelope for a record.
	ManifestType domain.ManifestType = "managed.record"
)

const specProtoPath = "ocm/record/v1alpha/record_spec.proto"

//go:embed record_spec.proto
var specProto string

// Descriptor declares the record management and delivery capabilities.
func Descriptor() domain.AddonDescriptor {
	return domain.AddonDescriptor{
		ID: "records.open-cluster-management.io", Name: "Configuration Records",
		Capabilities: []domain.Capability{
			domain.ManagedResourceCapability{ResourceType: ResourceType},
			domain.DeliveryCapability{TargetType: TargetType},
		},
	}
}

// Schema defines the complete desired state and its self-target relation.
func Schema() domain.ExtensionResourceSchema {
	return domain.ExtensionResourceSchema{
		ResourceType: ResourceType, ProtoPackage: "ocm.record.v1alpha", Version: "v1alpha",
		CollectionID: "records", Singular: "Record", Plural: "Records",
		ProtoFiles: map[string]string{specProtoPath: specProto}, EntryFile: specProtoPath,
		Management: &domain.ManagementSchema{
			SpecMessage: "ocm.record.v1alpha.RecordSpec",
			Relation:    domain.NewRegisteredSelfTarget(TargetID, ManifestType),
		},
	}
}
