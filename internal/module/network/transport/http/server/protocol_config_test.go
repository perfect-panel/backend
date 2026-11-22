package server

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	serverv1 "github.com/perfect-panel/backend/api/server/v1"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"google.golang.org/protobuf/types/known/structpb"
)

const protocolConfigTarget = "/v2/server/9?secret_key=" + testNodeSecret

// protocolConfig is a node configuration as the facade builds it; its
// plugin options are a map, which protobuf carries as a Struct.
func protocolConfig() *dto.QueryServerConfigResponse {
	return &dto.QueryServerConfigResponse{
		TrafficReportThreshold: 1024,
		PushInterval:           60,
		PullInterval:           30,
		IPStrategy:             "prefer_ipv4",
		DNS:                    []dto.NodeDNS{{Proto: "https", Address: "https://dns.example/dns-query", Domains: []string{"example.com"}}},
		Block:                  []string{"blocked.example"},
		Outbound:               []dto.NodeOutbound{{Name: "direct", Protocol: "freedom"}},
		Protocols: []dto.Protocol{{
			Type: "shadowsocks", Port: 8388, Enable: true, Cipher: "aes-256-gcm", Plugin: "obfs",
			PluginOptions: map[string]any{"mode": "tls", "host": "cdn.example", "fast-open": true},
		}},
		Total: 1,
	}
}

// The server comes from the path: a server_id that is not a number is a
// 400, answered before the node secret is checked.
func TestQueryServerProtocolConfig_rejectsANonNumericServerID(t *testing.T) {
	svc := &nodeService{protocols: protocolConfig()}
	engine := nodeAPI(svc, testNodeSecret)

	resp := send(engine, http.MethodGet, "/v2/server/abc?secret_key="+testNodeSecret, nil)
	assertText(t, resp, http.StatusBadRequest, "Invalid Params")
	assertHeader(t, resp, "Vary", "Accept")

	resp = send(engine, http.MethodGet, "/v2/server/abc", nil, acceptProtobuf)
	assertProtobufResult(t, resp, http.StatusBadRequest, http.StatusBadRequest, "Invalid Params")
	assertFacadeNotCalled(t, svc)
}

// The v2 pull is outside the /v1 group and checks the node secret itself:
// its refusal is a 401, not the group's 403.
func TestQueryServerProtocolConfig_requiresTheNodeSecret(t *testing.T) {
	for name, tc := range map[string]struct{ provisioned, target string }{
		"no secret":    {testNodeSecret, "/v2/server/9"},
		"wrong secret": {testNodeSecret, "/v2/server/9?secret_key=guess"},
		// An unprovisioned secret must not let an empty secret_key through.
		"unprovisioned secret": {"", "/v2/server/9?secret_key="},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &nodeService{protocols: protocolConfig()}
			engine := nodeAPI(svc, tc.provisioned)

			resp := send(engine, http.MethodGet, tc.target, nil)
			assertText(t, resp, http.StatusUnauthorized, "Unauthorized")
			assertHeader(t, resp, "Vary", "Accept")
			resp = send(engine, http.MethodGet, tc.target, nil, acceptProtobuf)
			assertProtobufResult(t, resp, http.StatusUnauthorized, http.StatusUnauthorized, "Unauthorized")
			assertFacadeNotCalled(t, svc)
		})
	}
}

// A node names the protocols it runs with repeated protocols or protocols[]
// parameters; the facade receives all of them (the plain ones first), with
// the server from the path.
func TestQueryServerProtocolConfig_forwardsTheServerAndItsProtocols(t *testing.T) {
	svc := &nodeService{protocols: protocolConfig()}

	send(nodeAPI(svc, testNodeSecret), http.MethodGet, protocolConfigTarget+"&protocols=vless&protocols[]=trojan&protocols=vmess", nil)

	assertFacadeRequest(t, svc, &dto.QueryServerConfigRequest{
		ServerID: 9, SecretKey: testNodeSecret, Protocols: []string{"vless", "vmess", "trojan"},
	})
}

// A JSON pull is answered in the result envelope, with an ETag of the
// configuration (not of the envelope) and a 304 once the node holds it.
func TestQueryServerProtocolConfig_answersJSONWithAnETagOfTheConfiguration(t *testing.T) {
	svc := &nodeService{protocols: protocolConfig()}
	engine := nodeAPI(svc, testNodeSecret)
	data, err := json.Marshal(protocolConfig())
	if err != nil {
		t.Fatal(err)
	}
	etag := httpx.GenerateETag(data)

	resp := send(engine, http.MethodGet, protocolConfigTarget, nil, ifNoneMatch("stale"))
	result := decodeJSONResult(t, resp)
	if result.Code != 200 || result.Msg != "success" {
		t.Fatalf("result = %s, want success", resp.Body())
	}
	assertJSON(t, result.Data, string(data))
	assertHeader(t, resp, "ETag", etag)
	assertHeader(t, resp, "Vary", "Accept")

	resp = send(engine, http.MethodGet, protocolConfigTarget, nil, ifNoneMatch(etag))
	assertStatus(t, resp, http.StatusNotModified)
	assertHeader(t, resp, "ETag", etag)
	if len(resp.Body()) != 0 {
		t.Fatalf("304 body = %q, want none", resp.Body())
	}
}

// A protobuf pull carries the typed configuration with an ETag of the body,
// the same on every pull of one configuration, and a 304 once the node
// holds it.
func TestQueryServerProtocolConfig_answersProtobufWithAnETagOfTheBody(t *testing.T) {
	svc := &nodeService{protocols: protocolConfig()}
	engine := nodeAPI(svc, testNodeSecret)
	pluginOptions, err := structpb.NewValue(map[string]any{"mode": "tls", "host": "cdn.example", "fast-open": true})
	if err != nil {
		t.Fatal(err)
	}
	want := &serverv1.QueryServerProtocolConfigResponse{Code: 200, Message: "success", Data: &serverv1.QueryServerProtocolConfigData{
		TrafficReportThreshold: 1024,
		PushInterval:           60,
		PullInterval:           30,
		IpStrategy:             "prefer_ipv4",
		Dns:                    []*serverv1.DNSResolver{{Proto: "https", Address: "https://dns.example/dns-query", Domains: []string{"example.com"}}},
		Block:                  []string{"blocked.example"},
		Outbound:               []*serverv1.Outbound{{Name: "direct", Protocol: "freedom"}},
		Protocols: []*serverv1.ServerProtocol{{
			Type: "shadowsocks", Port: 8388, Enable: true, Cipher: "aes-256-gcm", Plugin: "obfs", PluginOptions: pluginOptions,
		}},
		Total: 1,
	}}

	resp := send(engine, http.MethodGet, protocolConfigTarget, nil, acceptProtobuf, ifNoneMatch("stale"))
	assertProtobuf(t, resp, http.StatusOK, want)
	assertHeader(t, resp, "Vary", "Accept")
	etag := string(resp.Header.Peek("ETag"))
	if etag != httpx.GenerateETag(resp.Body()) {
		t.Fatalf("ETag = %q, want the ETag of the protobuf body", etag)
	}
	for range 20 {
		assertHeader(t, send(engine, http.MethodGet, protocolConfigTarget, nil, acceptProtobuf), "ETag", etag)
	}

	resp = send(engine, http.MethodGet, protocolConfigTarget, nil, acceptProtobuf, ifNoneMatch(etag))
	assertStatus(t, resp, http.StatusNotModified)
	assertHeader(t, resp, "ETag", etag)
	if len(resp.Body()) != 0 {
		t.Fatalf("304 body = %q, want none", resp.Body())
	}
}

// A configuration that cannot be encoded is an internal-error result in
// the negotiated encoding, rather than a partial body.
func TestQueryServerProtocolConfig_answersAnInternalErrorForAnUnencodableConfiguration(t *testing.T) {
	// JSON has no NaN, and the protobuf plugin options are built from JSON.
	for name, nan := range map[string]*dto.QueryServerConfigResponse{
		"protocol": {Protocols: []dto.Protocol{{Type: "shadowsocks", PluginOptions: math.NaN()}}},
		"outbound": {Outbound: []dto.NodeOutbound{{Name: "proxy", PluginOptions: map[string]any{"ratio": math.NaN()}}}},
	} {
		t.Run(name+" plugin options JSON cannot encode", func(t *testing.T) {
			engine := nodeAPI(&nodeService{protocols: nan}, testNodeSecret)
			assertJSONResult(t, send(engine, http.MethodGet, protocolConfigTarget, nil), xerr.ERROR, "Internal Server Error")
			resp := send(engine, http.MethodGet, protocolConfigTarget, nil, acceptProtobuf)
			assertProtobufResult(t, resp, http.StatusOK, xerr.ERROR, "Internal Server Error")
		})
	}

	// A protobuf string must be UTF-8.
	notUTF8 := &dto.QueryServerConfigResponse{IPStrategy: "\xff"}
	resp := send(nodeAPI(&nodeService{protocols: notUTF8}, testNodeSecret), http.MethodGet, protocolConfigTarget, nil, acceptProtobuf)
	assertProtobufResult(t, resp, http.StatusOK, xerr.ERROR, "Internal Server Error")
}
