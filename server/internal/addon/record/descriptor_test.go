package record_test

import (
	"context"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/stolostron/ocm-fleet/server/internal/addon/record"
	"github.com/stolostron/ocm-fleet/server/internal/transport/dynamicapi"
)

func TestRecordSchema(t *testing.T) {
	schema := record.Schema()
	desc, err := dynamicapi.CompileInline(context.Background(), schema.ProtoFiles, schema.EntryFile, schema.Management.SpecMessage)
	if err != nil {
		t.Fatal(err)
	}
	validator, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		spec  string
		valid bool
	}{
		{`{}`, true},
		{`{"values":{"message":"hello"}}`, true},
		{`{"values":{"":"empty key"}}`, false},
		{`{"values":{"message":42}}`, false},
		{`{"unknown":"field"}`, false},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			msg := dynamicpb.NewMessage(desc.Message)
			err := protojson.Unmarshal([]byte(tc.spec), msg)
			if err == nil {
				err = validator.Validate(msg)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, valid = %t", err, tc.valid)
			}
		})
	}
}
