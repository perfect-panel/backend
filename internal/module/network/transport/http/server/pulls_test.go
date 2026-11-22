package server

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"

	serverv1 "github.com/perfect-panel/backend/api/server/v1"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const pullQuery = "?secret_key=" + testNodeSecret + "&server_id=7&protocol=shadowsocks"

// pullEndpoint is one of the two /v1 pulls. They negotiate alike: JSON is
// the facade's response as it is with the facade's headers, protobuf a
// message with an ETag of its own, and the facade's errors become a 304 or
// a 404.
type pullEndpoint struct {
	path string
	// serve gives the facade its answer.
	serve func(*nodeService)
	// request is what the facade receives for the pull's common parameters.
	request      func(dto.ServerCommon) any
	wantJSON     string
	wantProtobuf proto.Message
}

func pullEndpoints(t *testing.T) []pullEndpoint {
	t.Helper()
	config, err := structpb.NewStruct(map[string]any{"port": 8388, "cipher": "aes-256-gcm", "server_key": "c2VjcmV0"})
	if err != nil {
		t.Fatal(err)
	}
	return []pullEndpoint{
		{
			path: "/v1/server/config",
			serve: func(s *nodeService) {
				s.config = &dto.GetServerConfigResponse{
					Basic:    dto.ServerBasic{PushInterval: 60, PullInterval: 30},
					Protocol: "shadowsocks",
					Config:   map[string]any{"port": 8388, "cipher": "aes-256-gcm", "server_key": "c2VjcmV0"},
				}
			},
			request:  func(common dto.ServerCommon) any { return &dto.GetServerConfigRequest{ServerCommon: common} },
			wantJSON: `{"basic":{"push_interval":60,"pull_interval":30},"protocol":"shadowsocks","config":{"port":8388,"cipher":"aes-256-gcm","server_key":"c2VjcmV0"}}`,
			wantProtobuf: &serverv1.GetServerConfigResponse{Code: 200, Message: "success", Data: &serverv1.ServerConfigData{
				Basic:    &serverv1.ServerBasic{PushInterval: 60, PullInterval: 30},
				Protocol: "shadowsocks",
				Config:   config,
			}},
		},
		{
			path: "/v1/server/user",
			serve: func(s *nodeService) {
				s.users = &dto.GetServerUserListResponse{Users: []dto.ServerUser{
					{Id: 1, UUID: "uuid-1", SpeedLimit: 10, DeviceLimit: 2},
					{Id: 2, UUID: "uuid-2"},
				}}
			},
			request:  func(common dto.ServerCommon) any { return &dto.GetServerUserListRequest{ServerCommon: common} },
			wantJSON: `{"users":[{"id":1,"uuid":"uuid-1","speed_limit":10,"device_limit":2},{"id":2,"uuid":"uuid-2","speed_limit":0,"device_limit":0}]}`,
			wantProtobuf: &serverv1.GetServerUserListResponse{Code: 200, Message: "success", Data: &serverv1.ServerUserListData{
				Users: []*serverv1.ServerUser{{Id: 1, Uuid: "uuid-1", SpeedLimit: 10, DeviceLimit: 2}, {Id: 2, Uuid: "uuid-2"}},
			}},
		},
	}
}

// A JSON pull is answered with the facade's response itself, without the
// result envelope the other endpoints use, and with the headers the facade
// set; the node's If-None-Match goes to the facade, which owns the ETag of
// the JSON representation.
func TestPulls_answerJSONWithTheFacadeResponseAndHeaders(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		t.Run(endpoint.path, func(t *testing.T) {
			svc := &nodeService{headers: map[string]string{"ETag": "json-etag"}}
			endpoint.serve(svc)

			resp := send(nodeAPI(svc, testNodeSecret), http.MethodGet, endpoint.path+pullQuery, nil, ifNoneMatch("cached-etag"))

			assertStatus(t, resp, http.StatusOK)
			assertHeader(t, resp, "Content-Type", jsonContentType)
			assertHeader(t, resp, "ETag", "json-etag")
			assertHeader(t, resp, "Vary", "Accept")
			assertJSON(t, resp.Body(), endpoint.wantJSON)
			assertFacadeRequest(t, svc, endpoint.request(dto.ServerCommon{Protocol: "shadowsocks", ServerId: 7, SecretKey: testNodeSecret}))
			if svc.meta.IfNoneMatch != "cached-etag" {
				t.Fatalf("facade If-None-Match = %q, want the node's", svc.meta.IfNoneMatch)
			}
		})
	}
}

// The handler does not require server_id or protocol: a pull without them
// asks the facade for server 0, and the facade's refusal is what the node
// sees.
func TestPulls_leaveMissingParametersToTheFacade(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		t.Run(endpoint.path, func(t *testing.T) {
			svc := &nodeService{err: errors.New("server 0 not found")}

			resp := send(nodeAPI(svc, testNodeSecret), http.MethodGet, endpoint.path+"?secret_key="+testNodeSecret, nil)

			assertText(t, resp, http.StatusNotFound, "Not Found")
			assertFacadeRequest(t, svc, endpoint.request(dto.ServerCommon{SecretKey: testNodeSecret}))
		})
	}
}

// The facade reports a current JSON copy as xerr.ErrNotModified, possibly
// wrapped: the node gets a 304 that still carries the ETag the facade set.
func TestPulls_answerNotModifiedWhenTheFacadeSaysSo(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		for name, err := range map[string]error{
			"sentinel": xerr.ErrNotModified,
			"wrapped":  fmt.Errorf("cached config: %w", xerr.ErrNotModified),
		} {
			t.Run(endpoint.path+" "+name, func(t *testing.T) {
				svc := &nodeService{headers: map[string]string{"ETag": "json-etag"}, err: err}

				resp := send(nodeAPI(svc, testNodeSecret), http.MethodGet, endpoint.path+pullQuery, nil, ifNoneMatch("json-etag"))

				// Hertz sends no body with a 304.
				assertStatus(t, resp, http.StatusNotModified)
				assertHeader(t, resp, "ETag", "json-etag")
				assertHeader(t, resp, "Vary", "Accept")
			})
		}
	}
}

// Any other failure of a pull, such as an unknown server or a disabled
// protocol, is a 404: text for a JSON node, a protobuf result otherwise.
func TestPulls_answerNotFoundWhenTheFacadeFails(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		for name, err := range map[string]error{
			"plain": errors.New("protocol vless not found or disabled"),
			"coded": xerr.NewErrCode(xerr.NodeNotExist),
		} {
			t.Run(endpoint.path+" "+name, func(t *testing.T) {
				svc := &nodeService{err: err}
				engine := nodeAPI(svc, testNodeSecret)

				assertText(t, send(engine, http.MethodGet, endpoint.path+pullQuery, nil), http.StatusNotFound, "Not Found")
				resp := send(engine, http.MethodGet, endpoint.path+pullQuery, nil, acceptProtobuf)
				assertProtobufResult(t, resp, http.StatusNotFound, http.StatusNotFound, "Not Found")
			})
		}
	}
}

// A protobuf pull carries an ETag of the protobuf body itself. The facade's
// ETag describes the JSON representation, so the node's If-None-Match is
// not handed to the facade and the facade's ETag is replaced; the handler
// answers 304 itself when the node already holds this body.
func TestPulls_answerProtobufWithAnETagOfTheBody(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		t.Run(endpoint.path, func(t *testing.T) {
			svc := &nodeService{headers: map[string]string{"ETag": "json-etag"}}
			endpoint.serve(svc)
			engine := nodeAPI(svc, testNodeSecret)

			resp := send(engine, http.MethodGet, endpoint.path+pullQuery, nil, acceptProtobuf, ifNoneMatch("json-etag"))

			assertProtobuf(t, resp, http.StatusOK, endpoint.wantProtobuf)
			assertHeader(t, resp, "Vary", "Accept")
			etag := string(resp.Header.Peek("ETag"))
			if etag != httpx.GenerateETag(resp.Body()) {
				t.Fatalf("ETag = %q, want the ETag of the protobuf body", etag)
			}
			if svc.meta.IfNoneMatch != "" {
				t.Fatalf("facade If-None-Match = %q, want none for a protobuf pull", svc.meta.IfNoneMatch)
			}

			resp = send(engine, http.MethodGet, endpoint.path+pullQuery, nil, acceptProtobuf, ifNoneMatch(etag))
			assertStatus(t, resp, http.StatusNotModified)
			assertHeader(t, resp, "ETag", etag)
			if len(resp.Body()) != 0 {
				t.Fatalf("304 body = %q, want none", resp.Body())
			}
		})
	}
}

// The protobuf ETag only saves a download if the same answer always encodes
// to the same bytes. The config is a google.protobuf.Struct, a map, which
// protobuf encodes in random order unless it is asked to be deterministic.
func TestPulls_protobufETagIsStableAcrossPulls(t *testing.T) {
	for _, endpoint := range pullEndpoints(t) {
		t.Run(endpoint.path, func(t *testing.T) {
			svc := &nodeService{}
			endpoint.serve(svc)
			engine := nodeAPI(svc, testNodeSecret)

			etags := make(map[string]bool)
			for range 20 {
				resp := send(engine, http.MethodGet, endpoint.path+pullQuery, nil, acceptProtobuf)
				etags[string(resp.Header.Peek("ETag"))] = true
			}
			if len(etags) != 1 {
				t.Fatalf("20 pulls of one answer got %d ETags, want 1", len(etags))
			}
		})
	}
}

// An answer protobuf cannot carry is an internal-error result, not a
// broken message: protobuf strings must be UTF-8, and the config becomes a
// google.protobuf.Struct through JSON, so it must be a JSON object.
func TestPulls_answerAnInternalErrorForAnAnswerProtobufCannotCarry(t *testing.T) {
	for name, tc := range map[string]struct {
		path  string
		serve func(*nodeService)
	}{
		"config protocol that is not UTF-8": {"/v1/server/config", func(s *nodeService) {
			s.config = &dto.GetServerConfigResponse{Protocol: "\xff", Config: map[string]any{}}
		}},
		"config that is not a JSON object": {"/v1/server/config", func(s *nodeService) {
			s.config = &dto.GetServerConfigResponse{Protocol: "vless", Config: []any{"port", 443}}
		}},
		"config JSON cannot encode": {"/v1/server/config", func(s *nodeService) {
			s.config = &dto.GetServerConfigResponse{Protocol: "vless", Config: map[string]any{"ratio": math.NaN()}}
		}},
		"user UUID that is not UTF-8": {"/v1/server/user", func(s *nodeService) {
			s.users = &dto.GetServerUserListResponse{Users: []dto.ServerUser{{Id: 1, UUID: "\xff"}}}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &nodeService{}
			tc.serve(svc)

			resp := send(nodeAPI(svc, testNodeSecret), http.MethodGet, tc.path+pullQuery, nil, acceptProtobuf)

			assertProtobufResult(t, resp, http.StatusOK, xerr.ERROR, "Internal Server Error")
		})
	}
}

// A server_id that is not a number is a parameter error, in the result
// envelope with HTTP 200 as every node API parameter error, and the facade
// is not asked.
func TestPulls_rejectANonNumericServerID(t *testing.T) {
	const message = `strconv.ParseInt: parsing "abc": invalid syntax`
	for _, endpoint := range pullEndpoints(t) {
		t.Run(endpoint.path, func(t *testing.T) {
			svc := &nodeService{}
			engine := nodeAPI(svc, testNodeSecret)
			target := endpoint.path + "?secret_key=" + testNodeSecret + "&server_id=abc"

			assertJSONResult(t, send(engine, http.MethodGet, target, nil), xerr.InvalidParams, message)
			assertProtobufResult(t, send(engine, http.MethodGet, target, nil, acceptProtobuf), http.StatusOK, xerr.InvalidParams, message)
			assertFacadeNotCalled(t, svc)
		})
	}
}
