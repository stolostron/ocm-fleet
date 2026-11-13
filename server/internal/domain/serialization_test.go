package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlatformResourceJSONRejectsInvalidState(t *testing.T) {
	for _, data := range []string{
		`null`,
		`{}`,
		`{"Name":"clusters"}`,
		`{"Name":"clusters/prod","Aliases":[{"Namespace":"","Key":"key","Value":"value"}]}`,
		`{"Name":"clusters/prod","Aliases":[{"Namespace":"ns","Key":"key","Value":"one"},{"Namespace":"ns","Key":"key","Value":"two"}]}`,
		`{"Name":"clusters/prod","Relationships":[{"SourceName":"clusters/prod","Type":"dependsOn","TargetName":"clusters/target","SourceService":"service"},{"SourceName":"clusters/prod","Type":"dependsOn","TargetName":"clusters/target","SourceService":"service"}]}`,
		`{"Name":"clusters/prod","Representations":[{"ServiceName":"service","Version":"v1","Name":"clusters/other","ExtensionResourceUID":"00000000-0000-4000-8000-000000000001"}]}`,
		`{"Name":"clusters/prod","Relationships":[{"SourceName":"clusters/other","Type":"dependsOn","TargetName":"clusters/target","SourceService":"service"}]}`,
		`{"Name":"clusters/prod","Representations":[{"ServiceName":"service","Version":"v1","Name":"clusters/prod","ExtensionResourceUID":"invalid"}]}`,
	} {
		t.Run(data, func(t *testing.T) {
			resource := NewPlatformResource(mustResourceName(t, "clusters/original"), refTime)
			before := resource.Snapshot()
			if err := json.Unmarshal([]byte(data), resource); err == nil {
				t.Fatal("invalid persisted state was accepted")
			}
			if after := resource.Snapshot(); !reflect.DeepEqual(before, after) { //nolint:domainzero // This assertion tests that failed hydration preserves the receiver.
				t.Fatalf("failed hydration changed the receiver: %+v", after)
			}
		})
	}
}

func TestPlatformResourceJSONRestoresMissingHistoricalTimestamp(t *testing.T) {
	data := []byte(`{"Name":"clusters/prod","CreatedAt":"2026-06-01T12:00:00Z"}`)
	var resource PlatformResource
	if err := json.Unmarshal(data, &resource); err != nil {
		t.Fatal(err)
	}
	if resource.CreatedAt() != refTime || resource.UpdatedAt() != refTime {
		t.Fatalf("historical timestamps were not restored: %+v", resource.Snapshot())
	}
}

func TestDeploymentUIDHydrationRejectsInvalidState(t *testing.T) {
	uid, err := ParseDeploymentUID("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`null`, `""`, `"invalid"`, `"00000000-0000-0000-0000-000000000000"`} {
		t.Run("json/"+data, func(t *testing.T) {
			value := uid
			if err := json.Unmarshal([]byte(data), &value); err == nil {
				t.Fatal("invalid UID was accepted")
			}
			if value != uid { //nolint:domainzero // This assertion tests that failed hydration preserves the receiver.
				t.Fatal("failed hydration changed the UID")
			}
		})
	}
	for _, data := range []string{"", "invalid", "00000000-0000-0000-0000-000000000000"} {
		t.Run("text/"+data, func(t *testing.T) {
			value := uid
			if err := value.UnmarshalText([]byte(data)); err == nil {
				t.Fatal("invalid UID was accepted")
			}
			if value != uid { //nolint:domainzero // This assertion tests that failed hydration preserves the receiver.
				t.Fatal("failed hydration changed the UID")
			}
		})
	}
	for _, source := range []any{nil, "", []byte{}, "invalid", 42, make([]byte, 16)} {
		t.Run("sql", func(t *testing.T) {
			value := uid
			if err := value.Scan(source); err == nil {
				t.Fatalf("invalid SQL UID %v was accepted", source)
			}
			if value != uid { //nolint:domainzero // This assertion tests that failed hydration preserves the receiver.
				t.Fatal("failed hydration changed the UID")
			}
		})
	}
}

func TestDeploymentUIDSerializationRoundTrips(t *testing.T) {
	uid := NewDeploymentUID()
	t.Run("json", func(t *testing.T) {
		data, err := json.Marshal(uid)
		if err != nil {
			t.Fatal(err)
		}
		var restored DeploymentUID
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if restored != uid {
			t.Fatalf("restored %s, want %s", restored, uid)
		}
	})
	t.Run("text", func(t *testing.T) {
		data, err := uid.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var restored DeploymentUID
		if err := restored.UnmarshalText(data); err != nil {
			t.Fatal(err)
		}
		if restored != uid {
			t.Fatalf("restored %s, want %s", restored, uid)
		}
	})
	t.Run("sql", func(t *testing.T) {
		data, err := uid.Value()
		if err != nil {
			t.Fatal(err)
		}
		var restored DeploymentUID
		if err := restored.Scan(data); err != nil {
			t.Fatal(err)
		}
		if restored != uid {
			t.Fatalf("restored %s, want %s", restored, uid)
		}
	})
}
