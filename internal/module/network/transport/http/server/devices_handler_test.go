package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"

	serverv1 "github.com/perfect-panel/server/api/server/v1"
	"github.com/perfect-panel/server/internal/module/network"
	dto "github.com/perfect-panel/server/internal/module/network/contract"
	"google.golang.org/protobuf/proto"
)

func TestDeviceAdmissionOverloadReturns503(t *testing.T) {
	for _, binary := range []bool{false, true} {
		svc := &nodeService{err: network.ErrServerBusy}
		h := nodeAPI(svc, testNodeSecret)
		headers := []ut.Header{jsonContent}
		if binary {
			headers = append(headers, acceptProtobuf)
		}
		resp := send(h, http.MethodPost, "/v1/server/device"+reportQuery, []byte(`{"uid":21,"ip":"127.0.0.1"}`), headers...)
		assertStatus(t, resp, http.StatusServiceUnavailable)
		if string(resp.Header.Peek("Retry-After")) != "1" || string(resp.Header.Peek("Cache-Control")) != "no-store" {
			t.Fatal("missing overload headers")
		}
		if binary {
			var result serverv1.Result
			if err := proto.Unmarshal(resp.Body(), &result); err != nil || result.Code != 503 {
				t.Fatalf("overload protobuf: %+v %v", &result, err)
			}
		} else {
			assertJSON(t, resp.Body(), `{"code":503,"message":"Server busy"}`)
		}
	}
}

func TestUserListRefreshTimeoutReturns503(t *testing.T) {
	for _, binary := range []bool{false, true} {
		svc := &nodeService{err: errors.Join(network.ErrServerBusy, context.DeadlineExceeded)}
		h := nodeAPI(svc, testNodeSecret)
		var headers []ut.Header
		if binary {
			headers = append(headers, acceptProtobuf)
		}
		resp := send(h, http.MethodGet, "/v1/server/user"+reportQuery, nil, headers...)
		assertStatus(t, resp, http.StatusServiceUnavailable)
		if string(resp.Header.Peek("Retry-After")) != "1" || string(resp.Header.Peek("Cache-Control")) != "no-store" {
			t.Fatal("missing retry headers for refresh timeout")
		}
		if binary {
			var result serverv1.Result
			if err := proto.Unmarshal(resp.Body(), &result); err != nil || result.Code != 503 {
				t.Fatalf("timeout protobuf: %+v %v", &result, err)
			}
		} else {
			assertJSON(t, resp.Body(), `{"code":503,"message":"Server busy"}`)
		}
	}
}

func TestDeviceEndpointsJSONAndProtobuf(t *testing.T) {
	svc := &nodeService{alive: &dto.GetServerAliveListResponse{Alive: map[int64]int64{21: 2}}, claim: &dto.ClaimServerDeviceResponse{Allowed: true, TTLSeconds: 300}}
	h := nodeAPI(svc, testNodeSecret)
	resp := send(h, http.MethodGet, "/v1/server/alivelist"+reportQuery, nil)
	assertStatus(t, resp, 200)
	assertJSON(t, resp.Body(), `{"alive":{"21":2}}`)
	resp = send(h, http.MethodGet, "/v1/server/alivelist"+reportQuery, nil, acceptProtobuf)
	var alive serverv1.GetServerAliveListResponse
	if err := proto.Unmarshal(resp.Body(), &alive); err != nil || alive.Code != 200 || alive.GetData().GetAlive()[21] != 2 {
		t.Fatalf("protobuf counts: %+v, %v", &alive, err)
	}
	for _, binary := range []bool{false, true} {
		svc.calls = 0
		body := []byte(`{"uid":21,"ip":"127.0.0.1","server_id":999}`)
		if binary {
			body, _ = proto.Marshal(&serverv1.ClaimServerDeviceRequest{UserId: 21, Ip: "127.0.0.1"})
			resp = send(h, http.MethodPost, "/v1/server/device"+reportQuery, body, ut.Header{Key: "Content-Type", Value: protobufContentType}, acceptProtobuf)
			var claim serverv1.ClaimServerDeviceResponse
			if err := proto.Unmarshal(resp.Body(), &claim); err != nil || claim.Code != 200 || !claim.GetData().GetAllowed() || claim.GetData().GetTtlSeconds() != 300 {
				t.Fatalf("protobuf claim: %+v, %v", &claim, err)
			}
		} else {
			resp = send(h, http.MethodPost, "/v1/server/device"+reportQuery, body, jsonContent)
			assertJSON(t, resp.Body(), `{"allowed":true,"ttl_seconds":300}`)
		}
		want := &dto.ClaimServerDeviceRequest{ServerCommon: dto.ServerCommon{ServerId: 7, Protocol: "vless", SecretKey: testNodeSecret}, SID: 21, IP: "127.0.0.1"}
		assertFacadeRequest(t, svc, want)
	}
}
