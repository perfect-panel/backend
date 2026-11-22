package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	account "github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// What the facade answers the signed-in account with.
var (
	accountInfo = &dto.User{Id: 7, ReferCode: "REF-7", Enable: true,
		AuthMethods: []dto.UserAuthMethod{{AuthType: "email", AuthIdentifier: "owner@example.com", Verified: true}}}
	accountDevices = &dto.GetDeviceListResponse{Total: 1,
		List: []dto.UserDevice{{Id: 3, Identifier: "device-1", Ip: "192.0.2.1", UserAgent: "app/1.0", Enabled: true}}}
	accountMethods = &dto.GetOAuthMethodsResponse{
		Methods: []dto.UserAuthMethod{{AuthType: "github", AuthIdentifier: "583231", Verified: true}}}
	telegramLink = &dto.BindTelegramResponse{Url: "https://t.me/panel_bot?start=bind-token", ExpiredAt: 1_790_000_000_000}
)

// profileService records whom the handlers of the signed-in account ask the
// identity facade to act for.
type profileService struct {
	calls   int
	account *account.User
	session any
	err     error
}

var (
	_ QueryUserInfoService   = (*profileService)(nil)
	_ LogoutService          = (*profileService)(nil)
	_ GetDeviceListService   = (*profileService)(nil)
	_ GetOAuthMethodsService = (*profileService)(nil)
	_ BindTelegramService    = (*profileService)(nil)
	_ UnbindTelegramService  = (*profileService)(nil)
)

// record notes the account and session of the call's context and returns
// the error the test set.
func (s *profileService) record(ctx context.Context) error {
	s.calls++
	s.account, _ = account.FromContext(ctx)
	s.session = ctx.Value(requestctx.CtxKeySessionID)
	return s.err
}

// answer records the call and returns resp, or the error the test set.
func answer[T any](ctx context.Context, s *profileService, resp *T) (*T, error) {
	if err := s.record(ctx); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *profileService) QueryUserInfo(ctx context.Context) (*dto.User, error) {
	return answer(ctx, s, accountInfo)
}

func (s *profileService) Logout(ctx context.Context) error { return s.record(ctx) }

func (s *profileService) GetDeviceList(ctx context.Context) (*dto.GetDeviceListResponse, error) {
	return answer(ctx, s, accountDevices)
}

func (s *profileService) GetOAuthMethods(ctx context.Context) (*dto.GetOAuthMethodsResponse, error) {
	return answer(ctx, s, accountMethods)
}

func (s *profileService) BindTelegram(ctx context.Context) (*dto.BindTelegramResponse, error) {
	return answer(ctx, s, telegramLink)
}

func (s *profileService) UnbindTelegram(ctx context.Context) error { return s.record(ctx) }

// The routes whose only input is the signed-in account: they read no
// parameter, so the account and session the auth middleware resolved from
// the token are all the facade acts on. data is what a success carries.
var accountEndpoints = map[string]struct {
	method, path string
	handler      func(*profileService) app.HandlerFunc
	data         any
}{
	"account info":    {http.MethodGet, "/v1/public/user/info", func(s *profileService) app.HandlerFunc { return QueryUserInfoHandler(s) }, accountInfo},
	"logout":          {http.MethodPost, "/v1/public/user/logout", func(s *profileService) app.HandlerFunc { return LogoutHandler(s) }, nil},
	"devices":         {http.MethodGet, "/v1/public/user/devices", func(s *profileService) app.HandlerFunc { return GetDeviceListHandler(s) }, accountDevices},
	"oauth methods":   {http.MethodGet, "/v1/public/user/oauth_methods", func(s *profileService) app.HandlerFunc { return GetOAuthMethodsHandler(s) }, accountMethods},
	"bind telegram":   {http.MethodGet, "/v1/public/user/bind_telegram", func(s *profileService) app.HandlerFunc { return BindTelegramHandler(s) }, telegramLink},
	"unbind telegram": {http.MethodPost, "/v1/public/user/unbind_telegram", func(s *profileService) app.HandlerFunc { return UnbindTelegramHandler(s) }, nil},
}

// signedIn stands in for the auth middleware of the /v1/public/user group:
// it puts the account and the session it resolved from the token in the
// request context, as middleware.AuthenticateRequest does.
func signedIn(u *account.User, sessionID string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		ctx = account.NewContext(ctx, u)
		c.Next(context.WithValue(ctx, requestctx.CtxKeySessionID, sessionID))
	}
}

// callAs sends the JSON body to the route as the signed-in u, with a query
// string naming another account.
func callAs(t *testing.T, u *account.User, method, path string, handler app.HandlerFunc, body string) *ut.ResponseRecorder {
	t.Helper()
	logtest.Discard(t)
	h := server.New()
	h.Use(signedIn(u, "session-7"))
	h.Handle(method, path, handler)
	return ut.PerformRequest(h.Engine, method, path+"?id=99&user_id=99",
		&ut.Body{Body: strings.NewReader(body), Len: len(body)},
		ut.Header{Key: "Content-Type", Value: "application/json"})
}

// foreignAccount is a body naming an account other than the signed-in one.
const foreignAccount = `{"id":99,"user_id":99}`

// envelope reads the reply, which always travels with HTTP 200.
func envelope(t *testing.T, w *ut.ResponseRecorder) (code uint32, msg string, data json.RawMessage) {
	t.Helper()
	var reply struct {
		Code uint32          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); w.Code != http.StatusOK || err != nil {
		t.Fatalf("answer = %d %q (%v)", w.Code, w.Body, err)
	}
	return reply.Code, reply.Msg, reply.Data
}

// sameJSON reports whether data encodes the value want.
func sameJSON(t *testing.T, data json.RawMessage, want any) bool {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got, expected any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("data %s: %v", data, err)
	}
	if err := json.Unmarshal(encoded, &expected); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(got, expected)
}

// The facade acts for the account and session the request context carries,
// whatever account the query string or body names, and the reply carries
// what it answered: the data of a read, none for logout and unbinding.
func TestAccountHandlersActForTheSignedInAccount(t *testing.T) {
	for name, endpoint := range accountEndpoints {
		t.Run(name, func(t *testing.T) {
			owner := &account.User{Id: 7}
			svc := &profileService{}
			code, msg, data := envelope(t, callAs(t, owner, endpoint.method, endpoint.path, endpoint.handler(svc), foreignAccount))
			if svc.calls != 1 || svc.account != owner || svc.session != "session-7" {
				t.Fatalf("calls = %d, account = %+v, session = %v; want one call for the signed-in account's session", svc.calls, svc.account, svc.session)
			}
			if code != xerr.SUCCESS || msg != "success" {
				t.Fatalf("reply = {%d %q %s}, want success", code, msg, data)
			}
			if (endpoint.data == nil && data != nil) || (endpoint.data != nil && !sameJSON(t, data, endpoint.data)) {
				t.Fatalf("data = %s, want %+v", data, endpoint.data)
			}
		})
	}
}

// A failure is reported in the envelope with the code the facade chose, or
// the generic code for an error that carries none, and no data.
func TestAccountHandlersReportFacadeErrorsInTheEnvelope(t *testing.T) {
	for name, endpoint := range accountEndpoints {
		for _, tc := range []struct {
			err  error
			code uint32
			msg  string
		}{
			{xerr.Errorf(xerr.InvalidAccess, "no session to end"), xerr.InvalidAccess, "Invalid access"},
			{errors.New("redis unavailable"), xerr.ERROR, "Internal Server Error"},
		} {
			t.Run(name+" "+tc.msg, func(t *testing.T) {
				w := callAs(t, &account.User{Id: 7}, endpoint.method, endpoint.path, endpoint.handler(&profileService{err: tc.err}), foreignAccount)
				if code, msg, data := envelope(t, w); code != tc.code || msg != tc.msg || data != nil {
					t.Fatalf("reply = {%d %q %s}, want {%d %q} without data", code, msg, data, tc.code, tc.msg)
				}
			})
		}
	}
}
