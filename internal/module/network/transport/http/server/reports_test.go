package server

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	serverv1 "github.com/perfect-panel/backend/api/server/v1"
	"github.com/perfect-panel/backend/internal/module/network"
	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
	"google.golang.org/protobuf/proto"
)

const reportQuery = "?secret_key=" + testNodeSecret + "&server_id=7&protocol=vless"

// reportEndpoint is one of the three /v1 reports: a body in JSON or protobuf
// by its Content-Type, answered with a result by Accept.
type reportEndpoint struct {
	path     string
	json     string
	protobuf proto.Message
	// request is what the facade receives for the report from server 7
	// over vless.
	request any
}

func reportEndpoints() []reportEndpoint {
	common := dto.ServerCommon{Protocol: "vless", ServerId: 7, SecretKey: testNodeSecret}
	return []reportEndpoint{
		{
			path:     "/v1/server/online",
			json:     `{"users":[{"uid":42,"ip":"203.0.113.1"},{"uid":43,"ip":"2001:db8::1"}]}`,
			protobuf: &serverv1.PushOnlineUsersRequest{Users: []*serverv1.OnlineUser{{UserId: 42, Ip: "203.0.113.1"}, {UserId: 43, Ip: "2001:db8::1"}}},
			request: &dto.OnlineUsersRequest{ServerCommon: common, Users: []dto.OnlineUser{
				{SID: 42, IP: "203.0.113.1"}, {SID: 43, IP: "2001:db8::1"},
			}},
		},
		{
			path:     "/v1/server/push",
			json:     `{"traffic":[{"uid":42,"upload":100,"download":200},{"uid":43,"upload":0,"download":5}]}`,
			protobuf: &serverv1.PushUserTrafficRequest{Traffic: []*serverv1.UserTraffic{{UserId: 42, Upload: 100, Download: 200}, {UserId: 43, Download: 5}}},
			request: &dto.ServerPushUserTrafficRequest{ServerCommon: common, Traffic: []dto.UserTraffic{
				{SID: 42, Upload: 100, Download: 200}, {SID: 43, Download: 5},
			}},
		},
		{
			path:     "/v1/server/status",
			json:     `{"cpu":0.5,"mem":0.25,"disk":0.75,"updated_at":1700000000}`,
			protobuf: &serverv1.PushServerStatusRequest{Cpu: 0.5, Mem: 0.25, Disk: 0.75, UpdatedAt: 1700000000},
			request:  &dto.ServerPushStatusRequest{ServerCommon: common, Cpu: 0.5, Mem: 0.25, Disk: 0.75, UpdatedAt: 1700000000},
		},
	}
}

// A report body is decoded by its Content-Type and the result encoded by
// Accept, independently of each other: a node may post protobuf and read
// JSON, or the other way round. The server comes from the query.
func TestReports_decodeByContentTypeAndAnswerByAccept(t *testing.T) {
	for _, endpoint := range reportEndpoints() {
		protobufBody, err := proto.Marshal(endpoint.protobuf)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name           string
			body           []byte
			contentType    string
			answerProtobuf bool
		}{
			{"JSON answered in JSON", []byte(endpoint.json), "application/json", false},
			{"JSON answered in protobuf", []byte(endpoint.json), "application/json", true},
			{"protobuf answered in protobuf", protobufBody, "application/protobuf; charset=binary", true},
			{"protobuf answered in JSON", protobufBody, protobufContentType, false},
		} {
			t.Run(endpoint.path+" "+tc.name, func(t *testing.T) {
				svc := &nodeService{}
				headers := []ut.Header{{Key: "Content-Type", Value: tc.contentType}}
				if tc.answerProtobuf {
					headers = append(headers, acceptProtobuf)
				}

				expected := endpoint.request
				if traffic, ok := expected.(*dto.ServerPushUserTrafficRequest); ok {
					headers = append(headers, ut.Header{Key: "Idempotency-Key", Value: "frozen-batch"})
					copy := *traffic
					copy.ReportID = "frozen-batch"
					expected = &copy
				}
				resp := send(nodeAPI(svc, testNodeSecret), http.MethodPost, endpoint.path+reportQuery, tc.body, headers...)
				assertFacadeRequest(t, svc, expected)
				if tc.answerProtobuf {
					assertProtobufResult(t, resp, http.StatusOK, 200, "success")
				} else {
					assertJSONResult(t, resp, 200, "success")
				}
				assertHeader(t, resp, "Vary", "Accept")
			})
		}
	}
}

// A report the handler cannot read is a parameter error (HTTP 200, code
// 400, the decoder's message) in the negotiated encoding, and never reaches
// the facade.
func TestReports_rejectReportsTheyCannotRead(t *testing.T) {
	for _, endpoint := range reportEndpoints() {
		protobufBody, err := proto.Marshal(endpoint.protobuf)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name        string
			query       string
			body        []byte
			contentType string
			message     string
		}{
			{name: "truncated JSON", query: reportQuery, body: []byte(`{"`), contentType: "application/json"},
			{name: "empty body", query: reportQuery, contentType: "application/json"},
			{name: "malformed protobuf", query: reportQuery, body: []byte{0xff}, contentType: protobufContentType},
			// Only application/protobuf selects the protobuf decoder.
			{name: "protobuf under another media type", query: reportQuery, body: protobufBody, contentType: "application/x-protobuf"},
			{
				name: "server_id that is not a number", query: "?secret_key=" + testNodeSecret + "&server_id=abc",
				body: []byte(endpoint.json), contentType: "application/json", message: `strconv.ParseInt: parsing "abc": invalid syntax`,
			},
		} {
			t.Run(endpoint.path+" "+tc.name, func(t *testing.T) {
				svc := &nodeService{}
				engine := nodeAPI(svc, testNodeSecret)
				contentType := ut.Header{Key: "Content-Type", Value: tc.contentType}

				resp := send(engine, http.MethodPost, endpoint.path+tc.query, tc.body, contentType)
				result := decodeJSONResult(t, resp)
				if result.Code != xerr.InvalidParams || result.Msg == "" || (tc.message != "" && result.Msg != tc.message) {
					t.Fatalf("result = %s, want a parameter error", resp.Body())
				}

				resp = send(engine, http.MethodPost, endpoint.path+tc.query, tc.body, contentType, acceptProtobuf)
				var answer serverv1.Result
				assertStatus(t, resp, http.StatusOK)
				decodeProtobuf(t, resp, &answer)
				if answer.Code != xerr.InvalidParams || answer.Message != result.Msg {
					t.Fatalf("protobuf result = %v, want the JSON result's parameter error %q", &answer, result.Msg)
				}
				assertFacadeNotCalled(t, svc)
			})
		}
	}
}

// The certificate pin is transport metadata: it comes from the
// X-Node-Certificate-SHA256 header ppanel-node sets, and a body cannot
// supply one.
func TestServerPushStatus_takesTheCertificatePinFromTheHeader(t *testing.T) {
	const pin = "5d41402abc4b2a76b9719d911017c592ae2a3d2c9d8b1e84f4e0d1c6c4b1a2f3"
	body := []byte(`{"cpu":0.5,"CertPinSHA256":"forged","cert_pin_sha256":"forged"}`)
	for name, tc := range map[string]struct {
		headers []ut.Header
		want    string
	}{
		"header":    {[]ut.Header{jsonContent, {Key: certificateSHA256Header, Value: pin}}, pin},
		"no header": {[]ut.Header{jsonContent}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &nodeService{}

			send(nodeAPI(svc, testNodeSecret), http.MethodPost, "/v1/server/status"+reportQuery, body, tc.headers...)

			request, ok := svc.req.(*dto.ServerPushStatusRequest)
			if !ok || request.CertPinSHA256 != tc.want || request.Cpu != 0.5 {
				t.Fatalf("facade request = %+v, want the status with certificate pin %q", svc.req, tc.want)
			}
		})
	}
}

// The reports and the v2 pull answer a facade failure in the negotiated
// result with HTTP 200: a coded error with its code and message, anything
// else as an internal error that does not leak its text.
func TestNodeAPI_reportsFacadeErrorsInTheResult(t *testing.T) {
	type request struct {
		method, target string
		body           []byte
	}
	requests := []request{{http.MethodGet, protocolConfigTarget, nil}}
	for _, endpoint := range reportEndpoints() {
		requests = append(requests, request{http.MethodPost, endpoint.path + reportQuery, []byte(endpoint.json)})
	}
	for name, tc := range map[string]struct {
		err     error
		code    uint32
		message string
	}{
		"coded": {fmt.Errorf("find server: %w", xerr.NewErrCodeMsg(xerr.NodeNotExist, "node does not exist")), xerr.NodeNotExist, "node does not exist"},
		"plain": {errors.New("redis: connection refused"), xerr.ERROR, "Internal Server Error"},
	} {
		for _, r := range requests {
			t.Run(name+" "+r.method+" "+r.target, func(t *testing.T) {
				svc := &nodeService{err: tc.err}
				engine := nodeAPI(svc, testNodeSecret)

				assertJSONResult(t, send(engine, r.method, r.target, r.body, jsonContent), tc.code, tc.message)
				resp := send(engine, r.method, r.target, r.body, jsonContent, acceptProtobuf)
				assertProtobufResult(t, resp, http.StatusOK, tc.code, tc.message)
			})
		}
	}
}

// A result protobuf cannot carry, such as an error message that is not
// UTF-8, falls back to a plain-text 500 rather than an undecodable body.
func TestNodeAPI_answersPlainInternalErrorForAResultProtobufCannotCarry(t *testing.T) {
	svc := &nodeService{err: xerr.NewErrCodeMsg(xerr.NodeNotExist, "\xff")}

	resp := send(nodeAPI(svc, testNodeSecret), http.MethodPost, "/v1/server/status"+reportQuery, []byte(`{}`), jsonContent, acceptProtobuf)

	assertText(t, resp, http.StatusInternalServerError, "Internal Server Error")
}

func TestTrafficReportConflictInBothEncodings(t *testing.T) {
	for _, binary := range []bool{false, true} {
		svc := &nodeService{err: network.ErrTrafficReportConflict}
		headers := []ut.Header{{Key: "Content-Type", Value: "application/json"}, {Key: "Idempotency-Key", Value: "stable-id"}}
		if binary {
			headers = append(headers, acceptProtobuf)
		}
		res := send(nodeAPI(svc, testNodeSecret), http.MethodPost, "/v1/server/push"+reportQuery, []byte(`{"traffic":[]}`), headers...)
		if binary {
			assertProtobufResult(t, res, http.StatusConflict, 409, network.ErrTrafficReportConflict.Error())
			continue
		}
		if res.StatusCode() != http.StatusConflict {
			t.Fatal("expected HTTP 409")
		}
		assertJSON(t, res.Body(), `{"code":409,"msg":"traffic report ID already identifies a different batch"}`)
	}
}
