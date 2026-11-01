package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	hertzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/route"
	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/internal/module/network"
	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"google.golang.org/protobuf/proto"
)

const (
	testNodeSecret  = "node-secret"
	jsonContentType = "application/json; charset=utf-8"
	textContentType = "text/plain; charset=utf-8"
)

var (
	acceptProtobuf = ut.Header{Key: "Accept", Value: protobufContentType}
	jsonContent    = ut.Header{Key: "Content-Type", Value: "application/json"}
)

func ifNoneMatch(etag string) ut.Header {
	return ut.Header{Key: "If-None-Match", Value: etag}
}

// nodeService stands in for the network facade behind the node API: every
// method answers with the canned result and records the request it was
// handed.
type nodeService struct {
	config    *dto.GetServerConfigResponse
	users     *dto.GetServerUserListResponse
	protocols *dto.QueryServerConfigResponse
	alive     *dto.GetServerAliveListResponse
	claim     *dto.ClaimServerDeviceResponse
	// headers is the response metadata the pulls return, with or without an
	// error, as the facade does.
	headers map[string]string
	err     error

	calls int
	req   any
	meta  network.RequestMeta
}

var (
	_ ServerConfigReader         = (*nodeService)(nil)
	_ ServerUserListReader       = (*nodeService)(nil)
	_ ServerProtocolConfigReader = (*nodeService)(nil)
	_ OnlineUsersReporter        = (*nodeService)(nil)
	_ ServerStatusReporter       = (*nodeService)(nil)
	_ UserTrafficReporter        = (*nodeService)(nil)
)

func (s *nodeService) record(req any) {
	s.calls++
	s.req = req
}

func (s *nodeService) GetServerConfig(_ context.Context, req *dto.GetServerConfigRequest, meta network.RequestMeta) (*dto.GetServerConfigResponse, network.ResponseMeta, error) {
	s.record(req)
	s.meta = meta
	return s.config, network.ResponseMeta{Headers: s.headers}, s.err
}

func (s *nodeService) GetServerUserList(_ context.Context, req *dto.GetServerUserListRequest, meta network.RequestMeta) (*dto.GetServerUserListResponse, network.ResponseMeta, error) {
	s.record(req)
	s.meta = meta
	return s.users, network.ResponseMeta{Headers: s.headers}, s.err
}

func (s *nodeService) QueryServerProtocolConfig(_ context.Context, req *dto.QueryServerConfigRequest) (*dto.QueryServerConfigResponse, error) {
	s.record(req)
	return s.protocols, s.err
}

func (s *nodeService) PushOnlineUsers(_ context.Context, req *dto.OnlineUsersRequest) error {
	s.record(req)
	return s.err
}

func (s *nodeService) GetServerAliveList(_ context.Context, req *dto.GetServerUserListRequest) (*dto.GetServerAliveListResponse, error) {
	s.record(req)
	return s.alive, s.err
}

func (s *nodeService) ClaimServerDevice(_ context.Context, req *dto.ClaimServerDeviceRequest) (*dto.ClaimServerDeviceResponse, error) {
	s.record(req)
	return s.claim, s.err
}

func (s *nodeService) ServerPushStatus(_ context.Context, req *dto.ServerPushStatusRequest) error {
	s.record(req)
	return s.err
}

func (s *nodeService) ServerPushUserTraffic(_ context.Context, req *dto.ServerPushUserTrafficRequest) error {
	s.record(req)
	return s.err
}

// nodeAPI serves the node API with svc as the network facade, registered as
// the route table does: the /v1 pulls and reports behind ServerMiddleware,
// and the /v2 protocol pull, which checks the node secret itself. secret is
// the provisioned node secret.
func nodeAPI(svc *nodeService, secret string) *route.Engine {
	h := hertzserver.Default()
	nodeSecret := func() string { return secret }
	group := h.Group("/v1/server", ServerMiddleware(nodeSecret))
	group.GET("/config", GetServerConfigHandler(svc))
	group.POST("/online", PushOnlineUsersHandler(svc))
	group.POST("/push", ServerPushUserTrafficHandler(svc))
	group.POST("/status", ServerPushStatusHandler(svc))
	group.GET("/user", GetServerUserListHandler(svc))
	group.GET("/alivelist", GetServerAliveListHandler(svc))
	group.POST("/device", ClaimServerDeviceHandler(svc))
	h.GET("/v2/server/:server_id", QueryServerProtocolConfigHandler(svc, nodeSecret))
	return h.Engine
}

// send performs one request and returns the response the node receives.
func send(engine *route.Engine, method, target string, body []byte, headers ...ut.Header) *protocol.Response {
	var requestBody *ut.Body
	if body != nil {
		requestBody = &ut.Body{Body: bytes.NewReader(body), Len: len(body)}
	}
	return ut.PerformRequest(engine, method, target, requestBody, headers...).Result()
}

func assertStatus(t *testing.T, resp *protocol.Response, want int) {
	t.Helper()
	if got := resp.StatusCode(); got != want {
		t.Fatalf("status = %d, want %d (body %q)", got, want, resp.Body())
	}
}

func assertHeader(t *testing.T, resp *protocol.Response, key, want string) {
	t.Helper()
	if got := string(resp.Header.Peek(key)); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

// assertText checks a plain-text answer, as a node that does not accept
// protobuf receives a refusal.
func assertText(t *testing.T, resp *protocol.Response, status int, want string) {
	t.Helper()
	assertStatus(t, resp, status)
	assertHeader(t, resp, "Content-Type", textContentType)
	if got := string(resp.Body()); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// decodeProtobuf checks that resp carries a protobuf body and decodes it
// into message.
func decodeProtobuf(t *testing.T, resp *protocol.Response, message proto.Message) {
	t.Helper()
	assertHeader(t, resp, "Content-Type", protobufContentType)
	if err := proto.Unmarshal(resp.Body(), message); err != nil {
		t.Fatalf("decode protobuf body %q: %v", resp.Body(), err)
	}
}

// assertProtobuf checks that resp carries exactly the protobuf message want.
func assertProtobuf(t *testing.T, resp *protocol.Response, status int, want proto.Message) {
	t.Helper()
	assertStatus(t, resp, status)
	got := want.ProtoReflect().New().Interface()
	decodeProtobuf(t, resp, got)
	if !proto.Equal(got, want) {
		t.Fatalf("protobuf body = %v, want %v", got, want)
	}
}

func assertProtobufResult(t *testing.T, resp *protocol.Response, status int, code uint32, message string) {
	t.Helper()
	assertProtobuf(t, resp, status, &serverv1.Result{Code: code, Message: message})
}

// jsonResult is the JSON result envelope; its HTTP status is always 200.
type jsonResult struct {
	Code uint32          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func decodeJSONResult(t *testing.T, resp *protocol.Response) jsonResult {
	t.Helper()
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", jsonContentType)
	var result jsonResult
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		t.Fatalf("decode JSON result %q: %v", resp.Body(), err)
	}
	return result
}

// assertJSONResult checks a result envelope without data.
func assertJSONResult(t *testing.T, resp *protocol.Response, code uint32, msg string) {
	t.Helper()
	result := decodeJSONResult(t, resp)
	if result.Code != code || result.Msg != msg || result.Data != nil {
		t.Fatalf("result = %s, want code %d and message %q without data", resp.Body(), code, msg)
	}
}

// assertJSON compares two JSON documents by value.
func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("body %q is not JSON: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("want %q is not JSON: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

// assertFacadeRequest checks that the facade was called once, with want.
func assertFacadeRequest(t *testing.T, svc *nodeService, want any) {
	t.Helper()
	if svc.calls != 1 {
		t.Fatalf("facade calls = %d, want 1", svc.calls)
	}
	if !reflect.DeepEqual(svc.req, want) {
		t.Fatalf("facade request = %+v, want %+v", svc.req, want)
	}
}

func assertFacadeNotCalled(t *testing.T, svc *nodeService) {
	t.Helper()
	if svc.calls != 0 {
		t.Fatalf("facade was called %d times with %+v, want no call", svc.calls, svc.req)
	}
}

// Every /v1 route answers 403 until the request carries the provisioned
// node secret, negotiated like any answer: a protobuf node gets a protobuf
// result. The secret is checked before the handler reads anything.
func TestServerMiddleware_forbidsRequestsWithoutTheNodeSecret(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/v1/server/config"},
		{http.MethodGet, "/v1/server/user"},
		{http.MethodGet, "/v1/server/alivelist"},
		{http.MethodPost, "/v1/server/device"},
		{http.MethodPost, "/v1/server/online"},
		{http.MethodPost, "/v1/server/push"},
		{http.MethodPost, "/v1/server/status"},
	}
	cases := []struct {
		name        string
		provisioned string
		query       string
	}{
		{"no secret", testNodeSecret, "?server_id=1"},
		{"wrong secret", testNodeSecret, "?server_id=1&secret_key=guess"},
		{"secret in upper case", testNodeSecret, "?server_id=1&secret_key=NODE-SECRET"},
		// An unprovisioned secret must not let an empty secret_key through.
		{"unprovisioned secret", "", "?server_id=1&secret_key="},
	}
	for _, tc := range cases {
		for _, r := range routes {
			t.Run(tc.name+" "+r.method+" "+r.path, func(t *testing.T) {
				svc := &nodeService{}
				engine := nodeAPI(svc, tc.provisioned)

				resp := send(engine, r.method, r.path+tc.query, []byte(`{}`), jsonContent)
				assertText(t, resp, http.StatusForbidden, "Forbidden")
				assertHeader(t, resp, "Vary", "Accept")

				resp = send(engine, r.method, r.path+tc.query, []byte(`{}`), jsonContent, acceptProtobuf)
				assertProtobufResult(t, resp, http.StatusForbidden, http.StatusForbidden, "Forbidden")
				assertHeader(t, resp, "Vary", "Accept")
				assertFacadeNotCalled(t, svc)
			})
		}
	}
}
