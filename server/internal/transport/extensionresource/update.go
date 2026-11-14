package extensionresource

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/stolostron/ocm-fleet/server/internal/application"
	"github.com/stolostron/ocm-fleet/server/internal/domain"
	"github.com/stolostron/ocm-fleet/server/internal/transport/dynamicapi"
)

func (h *dynamicHandler) handleUpdate(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	req := dynamicpb.NewMessage(h.descs.UpdateRequest)
	if err := dec(req); err != nil {
		return nil, err
	}
	if interceptor != nil {
		return interceptor(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/" + h.cfg.GRPCServiceName() + "/Update" + h.cfg.Singular},
			func(ctx context.Context, req any) (any, error) { return h.doUpdate(ctx, req.(proto.Message)) })
	}
	return h.doUpdate(ctx, req)
}

func (h *dynamicHandler) doUpdate(ctx context.Context, req proto.Message) (proto.Message, error) {
	msg := req.ProtoReflect()
	resourceField := h.descs.UpdateRequest.Fields().ByNumber(1)
	if !msg.Has(resourceField) {
		return nil, status.Error(codes.InvalidArgument, "resource is required")
	}
	resource := msg.Get(resourceField).Message()
	name, err := h.parseName(resource.Get(h.descs.Resource.Fields().ByName("name")).String())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid name: %v", err)
	}
	in := application.UpdateExtensionResourceInput{
		ResourceType: h.cfg.ResourceType, Name: name,
		Etag:               domain.Etag(resource.Get(h.descs.Resource.Fields().ByName("etag")).String()),
		UserSignature:      msg.Get(h.descs.UpdateRequest.Fields().ByName("user_signature")).Bytes(),
		ExpectedGeneration: domain.Generation(msg.Get(h.descs.UpdateRequest.Fields().ByName("expected_generation")).Int()),
	}
	maskField := h.descs.UpdateRequest.Fields().ByName("update_mask")
	if msg.Has(maskField) {
		mask := msg.Get(maskField).Message()
		paths := mask.Get(mask.Descriptor().Fields().ByName("paths")).List()
		for i := 0; i < paths.Len(); i++ {
			switch paths.Get(i).String() {
			case "spec":
				in.UpdateSpec = true
			case "labels":
				in.UpdateLabels = true
			case "*":
				in.UpdateSpec, in.UpdateLabels = true, true
			default:
				return nil, status.Error(codes.InvalidArgument, "supported update_mask paths are spec, labels, and *")
			}
		}
	} else {
		in.UpdateSpec = resource.Has(h.descs.Resource.Fields().ByName("spec"))
		in.UpdateLabels = resource.Get(h.descs.Resource.Fields().ByName("labels")).Map().Len() > 0
	}
	if in.UpdateSpec {
		spec := resource.Get(h.descs.Resource.Fields().ByName("spec")).Message().Interface()
		if err := h.validator.Validate(spec); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "spec validation failed: %v", err)
		}
		in.Spec, err = protojson.Marshal(spec)
		if err != nil {
			return nil, status.Error(codes.Internal, "marshal spec")
		}
	}
	if in.UpdateLabels {
		in.Labels = dynamicapi.ExtractMapStringString(resource, h.descs.Resource.Fields().ByName("labels"))
	}
	validUntil := h.descs.UpdateRequest.Fields().ByName("valid_until")
	if msg.Has(validUntil) {
		data, err := proto.Marshal(msg.Get(validUntil).Message().Interface())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid valid_until")
		}
		ts := &timestamppb.Timestamp{}
		if err := proto.Unmarshal(data, ts); err != nil || ts.CheckValid() != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid valid_until")
		}
		in.ValidUntil = ts.AsTime()
	}
	view, err := h.resources.Update(ctx, in)
	if err != nil {
		return nil, dynamicapi.ToStatusError(err)
	}
	return h.viewToResource(view)
}

func readRequestID(msg protoreflect.Message) (string, error) {
	field := msg.Descriptor().Fields().ByName("request_id")
	if field == nil {
		return "", nil
	}
	id := msg.Get(field).String()
	if id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			return "", status.Error(codes.InvalidArgument, "request_id must be a nonzero UUID")
		}
		return parsed.String(), nil
	}
	return "", nil
}
