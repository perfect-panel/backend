package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/transport/http/middleware"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// capturedService records what the handlers hand to the identity service.
type capturedService struct {
	calls    int
	metadata requestmeta.Metadata
	request  any
	err      error
}

var (
	_ UserLoginService              = (*capturedService)(nil)
	_ TelephoneLoginService         = (*capturedService)(nil)
	_ DeviceLoginService            = (*capturedService)(nil)
	_ UserRegisterService           = (*capturedService)(nil)
	_ TelephoneUserRegisterService  = (*capturedService)(nil)
	_ ResetPasswordService          = (*capturedService)(nil)
	_ TelephoneResetPasswordService = (*capturedService)(nil)
)

// answer records the call of a flow with req and returns the session token,
// or the error the test set.
func (s *capturedService) answer(ctx context.Context, req any) (*dto.LoginResponse, error) {
	s.calls++
	s.request = req
	s.metadata, _ = requestmeta.From(ctx)
	if s.err != nil {
		return nil, s.err
	}
	return &dto.LoginResponse{Token: "token"}, nil
}

func (s *capturedService) UserLogin(ctx context.Context, req *dto.UserLoginRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) TelephoneLogin(ctx context.Context, req *dto.TelephoneLoginRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) DeviceLogin(ctx context.Context, req *dto.DeviceLoginRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) UserRegister(ctx context.Context, req *dto.UserRegisterRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) TelephoneUserRegister(ctx context.Context, req *dto.TelephoneRegisterRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) ResetPassword(ctx context.Context, req *dto.ResetPasswordRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

func (s *capturedService) TelephoneResetPassword(ctx context.Context, req *dto.TelephoneResetPasswordRequest) (*dto.LoginResponse, error) {
	return s.answer(ctx, *req)
}

// signInEndpoint is a route of a flow that ends in a session: a sign-in, a
// registration or a password reset.
type signInEndpoint struct {
	path    string
	handler func(*capturedService) app.HandlerFunc
	// body is a request the handler accepts and want what the service
	// receives for it.
	body string
	want any
	// invalid is a request the validation refuses with message.
	invalid, message string
}

var signInEndpoints = map[string]signInEndpoint{
	"email login": {
		path:    "/v1/auth/login",
		handler: func(s *capturedService) app.HandlerFunc { return UserLoginHandler(s) },
		body:    `{"email":"owner@example.com","password":"password-1","identifier":"device-1","cf_token":"turnstile-1"}`,
		want:    dto.UserLoginRequest{Email: "owner@example.com", Password: "password-1", Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"email":"not-an-email","password":"password-1"}`,
		message: "Email must be a valid email address",
	},
	"telephone login": {
		path:    "/v1/auth/login/telephone",
		handler: func(s *capturedService) app.HandlerFunc { return TelephoneLoginHandler(s) },
		body:    `{"telephone":"13800138000","telephone_area_code":"86","password":"password-1","telephone_code":"123456","identifier":"device-1","cf_token":"turnstile-1"}`,
		want: dto.TelephoneLoginRequest{Telephone: "13800138000", TelephoneAreaCode: "86", Password: "password-1",
			TelephoneCode: "123456", Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"telephone":"13800138000","password":"password-1"}`,
		message: "TelephoneAreaCode is a required field",
	},
	"device login": {
		path:    "/v1/auth/login/device",
		handler: func(s *capturedService) app.HandlerFunc { return DeviceLoginHandler(s) },
		body:    `{"identifier":"device-1","invite":"INVITE-1","cf_token":"turnstile-1"}`,
		want:    dto.DeviceLoginRequest{Identifier: "device-1", Invite: "INVITE-1", CfToken: "turnstile-1"},
		invalid: `{"identifier":"` + strings.Repeat("d", 256) + `"}`,
		message: "Identifier must be a maximum of 255 characters in length",
	},
	"email registration": {
		path:    "/v1/auth/register",
		handler: func(s *capturedService) app.HandlerFunc { return UserRegisterHandler(s) },
		body:    `{"email":"new@example.com","password":"password-1","invite":"INVITE-1","code":"123456","identifier":"device-1","cf_token":"turnstile-1"}`,
		want: dto.UserRegisterRequest{Email: "new@example.com", Password: "password-1", Invite: "INVITE-1", Code: "123456",
			Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"email":"new@example.com","password":"short"}`,
		message: "Password must be at least 8 characters in length",
	},
	"telephone registration": {
		path:    "/v1/auth/register/telephone",
		handler: func(s *capturedService) app.HandlerFunc { return TelephoneUserRegisterHandler(s) },
		body:    `{"telephone":"13800138000","telephone_area_code":"86","password":"password-1","invite":"INVITE-1","code":"123456","identifier":"device-1","cf_token":"turnstile-1"}`,
		want: dto.TelephoneRegisterRequest{Telephone: "13800138000", TelephoneAreaCode: "86", Password: "password-1",
			Invite: "INVITE-1", Code: "123456", Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"telephone_area_code":"86","password":"password-1"}`,
		message: "Telephone is a required field",
	},
	"email password reset": {
		path:    "/v1/auth/reset",
		handler: func(s *capturedService) app.HandlerFunc { return ResetPasswordHandler(s) },
		body:    `{"email":"owner@example.com","password":"password-2","code":"123456","identifier":"device-1","cf_token":"turnstile-1"}`,
		want: dto.ResetPasswordRequest{Email: "owner@example.com", Password: "password-2", Code: "123456",
			Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"password":"password-2","code":"123456"}`,
		message: "Email is a required field",
	},
	"telephone password reset": {
		path:    "/v1/auth/reset/telephone",
		handler: func(s *capturedService) app.HandlerFunc { return TelephoneResetPasswordHandler(s) },
		body:    `{"telephone":"13800138000","telephone_area_code":"86","password":"password-2","code":"123456","identifier":"device-1","cf_token":"turnstile-1"}`,
		want: dto.TelephoneResetPasswordRequest{Telephone: "13800138000", TelephoneAreaCode: "86", Password: "password-2",
			Code: "123456", Identifier: "device-1", CfToken: "turnstile-1"},
		invalid: `{"telephone":"13800138000","telephone_area_code":"86","password":"` + strings.Repeat("p", 129) + `"}`,
		message: "Password must be a maximum of 128 characters in length",
	},
}

// reply is the JSON envelope the handlers answer with.
type reply struct {
	Code uint32          `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// signInRequest is one request to an endpoint.
type signInRequest struct {
	target, contentType, body string
	header                    map[string]string
}

// serve sends req through the access-log middleware, which resolves the
// request metadata, to the endpoint's handler, and returns the reply and the
// client address Hertz resolved for the request.
func serve(t *testing.T, endpoint signInEndpoint, svc *capturedService, req signInRequest) (reply, string) {
	t.Helper()
	logtest.Discard(t)
	router := server.New()
	router.Use(middleware.LoggerMiddleware())
	router.POST(endpoint.path, endpoint.handler(svc))
	c := router.NewContext()
	c.Request.Header.SetMethod(http.MethodPost)
	c.Request.Header.Set("Content-Type", req.contentType)
	c.Request.Header.Set("User-Agent", "Client-UA/1.0")
	// A client-supplied header is not the client address.
	c.Request.Header.Set("X-Original-Forwarded-For", "198.51.100.66")
	for key, value := range req.header {
		c.Request.Header.Set(key, value)
	}
	c.Request.SetRequestURI(endpoint.path + req.target)
	c.Request.SetBodyString(req.body)
	router.ServeHTTP(context.Background(), c)
	if status := c.Response.StatusCode(); status != http.StatusOK {
		t.Fatalf("status = %d, want the envelope's 200", status)
	}
	var envelope reply
	if err := json.Unmarshal(c.Response.Body(), &envelope); err != nil {
		t.Fatalf("response %q: %v", c.Response.Body(), err)
	}
	return envelope, c.ClientIP()
}

// The flows get the client address and user agent from the request
// metadata; the request body and headers cannot supply them. The request's
// fields reach the service as sent, and the session token it issues is the
// reply's data.
func TestLoginHandlersPassTheRequestMetadata(t *testing.T) {
	for name, endpoint := range signInEndpoints {
		t.Run(name, func(t *testing.T) {
			svc := &capturedService{}
			spoofed := strings.TrimSuffix(endpoint.body, "}") + `,"IP":"198.51.100.77","UserAgent":"spoofed"}`
			got, clientIP := serve(t, endpoint, svc, signInRequest{contentType: "application/json", body: spoofed})
			if got.Code != xerr.SUCCESS || got.Msg != "success" || string(got.Data) != `{"token":"token"}` || svc.calls != 1 {
				t.Fatalf("reply = {%d %q %s}, calls = %d", got.Code, got.Msg, got.Data, svc.calls)
			}
			if svc.request != endpoint.want {
				t.Fatalf("request = %+v, want %+v", svc.request, endpoint.want)
			}
			if svc.metadata.ClientIP != clientIP || svc.metadata.UserAgent != "Client-UA/1.0" {
				t.Fatalf("metadata = %+v, want %s and the raw User-Agent", svc.metadata, clientIP)
			}
		})
	}
}

// A JSON request is read from its body alone: neither the query string nor
// the Login-Type header reaches the request the service gets. A device
// sign-in is conveyed by the device transport instead, which puts the login
// type in the request context when device sign-in is enabled.
func TestLoginHandlersReadTheJSONBodyAlone(t *testing.T) {
	for name, endpoint := range signInEndpoints {
		t.Run(name, func(t *testing.T) {
			svc := &capturedService{}
			serve(t, endpoint, svc, signInRequest{
				target:      "?email=query@example.com&telephone=13900139000&identifier=query-device&LoginType=query",
				contentType: "application/json",
				body:        endpoint.body,
				header:      map[string]string{"Login-Type": "device"},
			})
			if svc.calls != 1 || svc.request != endpoint.want {
				t.Fatalf("calls = %d, request = %+v; want one call with %+v", svc.calls, svc.request, endpoint.want)
			}
		})
	}
}

// Every handler validates the request before the service, which applies the
// Turnstile check, runs. A body that is not JSON, or does not parse, is a
// parameter error too.
func TestLoginHandlersValidateBeforeCallingTheService(t *testing.T) {
	for name, endpoint := range signInEndpoints {
		for _, tc := range []struct {
			name              string
			contentType, body string
			message           string
		}{
			{"invalid", "application/json", endpoint.invalid, endpoint.message},
			{"unparsable", "application/json", strings.TrimSuffix(endpoint.body, "}"), ""},
			{"form encoded", "application/x-www-form-urlencoded", "email=owner%40example.com&telephone=13800138000&password=password-1", ""},
		} {
			t.Run(name+" "+tc.name, func(t *testing.T) {
				svc := &capturedService{}
				got, _ := serve(t, endpoint, svc, signInRequest{contentType: tc.contentType, body: tc.body})
				if got.Code != xerr.InvalidParams || (tc.message != "" && got.Msg != tc.message) || got.Data != nil {
					t.Fatalf("reply = {%d %q %s}, want code %d with %q", got.Code, got.Msg, got.Data, xerr.InvalidParams, tc.message)
				}
				if svc.calls != 0 {
					t.Fatalf("calls = %d; want a parameter error before the service", svc.calls)
				}
			})
		}
	}
}

// A failed flow is reported in the envelope, still with HTTP 200: the code
// the service chose and its message, or the generic code for an error that
// carries none, and never a token.
func TestLoginHandlersReportServiceErrorsInTheEnvelope(t *testing.T) {
	for name, endpoint := range signInEndpoints {
		for _, tc := range []struct {
			name string
			err  error
			code uint32
			msg  string
		}{
			{"coded", xerr.Errorf(xerr.UserPasswordError, "wrong password"), xerr.UserPasswordError, "User password error"},
			{"uncoded", errors.New("redis unavailable"), xerr.ERROR, "Internal Server Error"},
		} {
			t.Run(name+" "+tc.name, func(t *testing.T) {
				got, _ := serve(t, endpoint, &capturedService{err: tc.err}, signInRequest{contentType: "application/json", body: endpoint.body})
				if got.Code != tc.code || got.Msg != tc.msg || got.Data != nil {
					t.Fatalf("reply = {%d %q %s}, want {%d %q} without data", got.Code, got.Msg, got.Data, tc.code, tc.msg)
				}
			})
		}
	}
}
