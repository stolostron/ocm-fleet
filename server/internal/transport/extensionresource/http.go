package extensionresource

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/stolostron/ocm-fleet/server/internal/transport/dynamicapi"
)

// RegisterHTTP registers REST/JSON routes for the dynamic service on the
// given HTTP mux. Routes follow the AIP HTTP binding pattern at the
// canonical /apis/{service}/{version}/{collection} prefix:
//
//	POST   {prefix}                 -> Create
//	GET    {prefix}/{id}            -> Get
//	PATCH  {prefix}/{id}            -> Update
//	GET    {prefix}                 -> List
//	DELETE {prefix}/{id}            -> Delete
//	POST   {prefix}/{id}:resume     -> Resume
//
// The conn is a gRPC client connection to the server hosting the service.
//
// For dynamic (hot-swappable) registration, prefer [dynamicapi.DynamicHTTPMux]
// which uses handler indirection to support atomic replace and deregister.
func RegisterHTTP(mux *http.ServeMux, svc *RegisteredService, conn *grpc.ClientConn) {
	prefix := svc.Config.CanonicalHTTPPrefix()
	handler := BuildHTTPHandler(svc, conn, prefix)

	// Register both the exact path and the subtree pattern so that
	// /v1/{collection} (list, create) and /v1/{collection}/{id} (get,
	// delete) are both routed here instead of falling through to the
	// grpc-gateway catch-all on /v1/.
	mux.HandleFunc(prefix, handler)
	mux.HandleFunc(prefix+"/", handler)
}

// BuildHTTPHandler creates the HTTP handler function for a managed
// resource service without registering it on any mux. The conn is a
// shared gRPC client connection to the server's own loopback — routing
// to the correct service is handled by method name, not connection
// identity.
func BuildHTTPHandler(svc *RegisteredService, conn *grpc.ClientConn, prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, prefix)

		switch {
		case r.Method == http.MethodPost && (rest == "" || rest == "/"):
			if svc.Descriptors.CreateRequest == nil {
				http.NotFound(w, r)
				return
			}
			handleHTTPCreate(w, r, conn, svc)
		case r.Method == http.MethodPost && strings.HasSuffix(rest, ":resume"):
			if svc.Descriptors.ResumeRequest == nil {
				http.NotFound(w, r)
				return
			}
			id := strings.TrimPrefix(rest, "/")
			id = strings.TrimSuffix(id, ":resume")
			handleHTTPResume(w, r, conn, svc, id)
		case r.Method == http.MethodGet && (rest == "" || rest == "/"):
			handleHTTPList(w, r, conn, svc)
		case r.Method == http.MethodGet && len(rest) > 1:
			id := strings.TrimPrefix(rest, "/")
			handleHTTPGet(w, r, conn, svc, id)
		case r.Method == http.MethodPatch && len(rest) > 1:
			if svc.Descriptors.UpdateRequest == nil {
				http.NotFound(w, r)
				return
			}
			handleHTTPUpdate(w, r, conn, svc, strings.TrimPrefix(rest, "/"))
		case r.Method == http.MethodDelete && len(rest) > 1:
			if svc.Descriptors.DeleteRequest == nil {
				http.NotFound(w, r)
				return
			}
			id := strings.TrimPrefix(rest, "/")
			handleHTTPDelete(w, r, conn, svc, id)
		default:
			http.NotFound(w, r)
		}
	}
}

func handleHTTPCreate(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "read body: "+err.Error())
		return
	}

	// Parse the request body as the resource message (spec + optional fields).
	resource := dynamicpb.NewMessage(svc.Descriptors.Resource)
	if err := protojson.Unmarshal(body, resource); err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "parse body: "+err.Error())
		return
	}

	// Extract the ID from the query parameter (AIP pattern: ?{singular}_id=xxx)
	lower := strings.ToLower(svc.Config.Singular[:1]) + svc.Config.Singular[1:]
	id := r.URL.Query().Get(lower + "_id")
	if id == "" {
		dynamicapi.HTTPError(w, codes.InvalidArgument, lower+"_id query parameter is required")
		return
	}

	createReq := dynamicpb.NewMessage(svc.Descriptors.CreateRequest)
	idField := svc.Descriptors.CreateRequest.Fields().ByNumber(1)
	resourceField := svc.Descriptors.CreateRequest.Fields().ByNumber(2)
	createReq.Set(idField, stringValue(id))
	createReq.Set(resourceField, messageValue(resource))
	if err := populateMutationQuery(r, createReq); err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, err.Error())
		return
	}

	resp := dynamicpb.NewMessage(svc.Descriptors.Resource)
	method := "/" + svc.Config.GRPCServiceName() + "/Create" + svc.Config.Singular
	if err := conn.Invoke(dynamicapi.GRPCContext(r), method, createReq, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}

	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

func handleHTTPGet(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService, id string) {
	getReq := dynamicpb.NewMessage(svc.Descriptors.GetRequest)
	nameField := svc.Descriptors.GetRequest.Fields().ByName("name")
	getReq.Set(nameField, stringValue(svc.Config.Collection()+id))

	resp := dynamicpb.NewMessage(svc.Descriptors.Resource)
	method := "/" + svc.Config.GRPCServiceName() + "/Get" + svc.Config.Singular
	if err := conn.Invoke(dynamicapi.GRPCContext(r), method, getReq, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}

	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

func handleHTTPList(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService) {
	listReq := dynamicpb.NewMessage(svc.Descriptors.ListRequest)

	if v := r.URL.Query().Get("page_size"); v != "" {
		if field := svc.Descriptors.ListRequest.Fields().ByName("page_size"); field != nil {
			n, err := strconv.ParseInt(v, 10, 32)
			if err != nil {
				dynamicapi.HTTPError(w, codes.InvalidArgument, "invalid page_size: "+err.Error())
				return
			}
			listReq.Set(field, protoreflect.ValueOfInt32(int32(n)))
		}
	}
	if v := r.URL.Query().Get("page_token"); v != "" {
		if field := svc.Descriptors.ListRequest.Fields().ByName("page_token"); field != nil {
			listReq.Set(field, protoreflect.ValueOfString(v))
		}
	}

	resp := dynamicpb.NewMessage(svc.Descriptors.ListResponse)
	method := "/" + svc.Config.GRPCServiceName() + "/List" + svc.Config.Plural
	if err := conn.Invoke(dynamicapi.GRPCContext(r), method, listReq, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}

	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

func handleHTTPDelete(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService, id string) {
	deleteReq := dynamicpb.NewMessage(svc.Descriptors.DeleteRequest)
	nameField := svc.Descriptors.DeleteRequest.Fields().ByName("name")
	deleteReq.Set(nameField, stringValue(svc.Config.Collection()+id))
	deleteReq.Set(svc.Descriptors.DeleteRequest.Fields().ByName("etag"), stringValue(r.URL.Query().Get("etag")))
	if err := populateMutationQuery(r, deleteReq); err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, err.Error())
		return
	}

	resp := dynamicpb.NewMessage(svc.Descriptors.Resource)
	method := "/" + svc.Config.GRPCServiceName() + "/Delete" + svc.Config.Singular
	if err := conn.Invoke(dynamicapi.GRPCContext(r), method, deleteReq, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}

	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

func handleHTTPResume(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService, id string) {
	resumeReq := dynamicpb.NewMessage(svc.Descriptors.ResumeRequest)
	nameField := svc.Descriptors.ResumeRequest.Fields().ByName("name")
	resumeReq.Set(nameField, stringValue(svc.Config.Collection()+id))

	// Parse optional request body for user_signature / valid_until.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "read body: "+err.Error())
		return
	}
	if len(body) > 0 {
		if err := protojson.Unmarshal(body, resumeReq); err != nil {
			dynamicapi.HTTPError(w, codes.InvalidArgument, "parse body: "+err.Error())
			return
		}
		// Re-set name since body unmarshal might have cleared it.
		resumeReq.Set(nameField, stringValue(svc.Config.Collection()+id))
	}

	resp := dynamicpb.NewMessage(svc.Descriptors.Resource)
	method := "/" + svc.Config.GRPCServiceName() + "/Resume" + svc.Config.Singular
	if err := conn.Invoke(dynamicapi.GRPCContext(r), method, resumeReq, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}

	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

func stringValue(s string) protoreflect.Value {
	return protoreflect.ValueOfString(s)
}

func messageValue(m *dynamicpb.Message) protoreflect.Value {
	return protoreflect.ValueOfMessage(m)
}

func handleHTTPUpdate(w http.ResponseWriter, r *http.Request, conn *grpc.ClientConn, svc *RegisteredService, id string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "read body: "+err.Error())
		return
	}
	resource := dynamicpb.NewMessage(svc.Descriptors.Resource)
	if err := protojson.Unmarshal(body, resource); err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "parse body: "+err.Error())
		return
	}
	nameField := svc.Descriptors.Resource.Fields().ByName("name")
	name := svc.Config.Collection() + id
	if supplied := resource.Get(nameField).String(); supplied != "" && supplied != name {
		dynamicapi.HTTPError(w, codes.InvalidArgument, "body name does not match request path")
		return
	}
	resource.Set(nameField, stringValue(name))
	req := dynamicpb.NewMessage(svc.Descriptors.UpdateRequest)
	req.Set(svc.Descriptors.UpdateRequest.Fields().ByNumber(1), messageValue(resource))
	query := r.URL.Query()
	paths, present := query["update_mask"]
	if !present {
		paths, present = query["updateMask"]
	}
	if present {
		mask := req.Mutable(svc.Descriptors.UpdateRequest.Fields().ByName("update_mask")).Message()
		list := mask.Mutable(mask.Descriptor().Fields().ByName("paths")).List()
		for _, value := range paths {
			if value == "" {
				continue // an explicitly empty mask is a no-op
			}
			for path := range strings.SplitSeq(value, ",") {
				list.Append(stringValue(path))
			}
		}
	}
	if err := populateMutationQuery(r, req); err != nil {
		dynamicapi.HTTPError(w, codes.InvalidArgument, err.Error())
		return
	}
	resp := dynamicpb.NewMessage(svc.Descriptors.Resource)
	if err := conn.Invoke(dynamicapi.GRPCContext(r), "/"+svc.Config.GRPCServiceName()+"/Update"+svc.Config.Singular, req, resp); err != nil {
		dynamicapi.GRPCHTTPError(w, err)
		return
	}
	dynamicapi.WriteJSON(w, http.StatusOK, resp)
}

// populateMutationQuery handles request metadata outside the resource body,
// matching the standard HTTP binding for Create and Update.
func populateMutationQuery(r *http.Request, req *dynamicpb.Message) error {
	fields := req.Descriptor().Fields()
	query := r.URL.Query()
	if field := fields.ByName("request_id"); field != nil {
		req.Set(field, stringValue(query.Get("request_id")))
	}
	if field := fields.ByName("user_signature"); field != nil && query.Get("user_signature") != "" {
		value, err := base64.StdEncoding.DecodeString(query.Get("user_signature"))
		if err != nil {
			return fmt.Errorf("invalid user_signature: %w", err)
		}
		req.Set(field, protoreflect.ValueOfBytes(value))
	}
	if field := fields.ByName("valid_until"); field != nil && query.Get("valid_until") != "" {
		value, err := time.Parse(time.RFC3339Nano, query.Get("valid_until"))
		if err != nil {
			return fmt.Errorf("invalid valid_until: %w", err)
		}
		valueProto, err := dynamicapi.MarshalTimestamp(field, value)
		if err != nil {
			return err
		}
		req.Set(field, valueProto)
	}
	if field := fields.ByName("expected_generation"); field != nil && query.Get("expected_generation") != "" {
		value, err := strconv.ParseInt(query.Get("expected_generation"), 10, 64)
		if err != nil || value <= 0 {
			return fmt.Errorf("expected_generation must be a positive integer")
		}
		req.Set(field, protoreflect.ValueOfInt64(value))
	}
	return nil
}
