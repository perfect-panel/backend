package log

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/internal/handlertest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fakeLogs is the log port: it records every call and answers the
// recorder's answer for the method.
type fakeLogs struct{ handlertest.Recorder }

var _ Logs = (*fakeLogs)(nil)

func (f *fakeLogs) FilterBalanceLog(_ context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error) {
	return handlertest.Answer[dto.FilterBalanceLogResponse](&f.Recorder, "FilterBalanceLog", req)
}

func (f *fakeLogs) FilterCommissionLog(_ context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error) {
	return handlertest.Answer[dto.FilterCommissionLogResponse](&f.Recorder, "FilterCommissionLog", req)
}

func (f *fakeLogs) FilterEmailLog(_ context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error) {
	return handlertest.Answer[dto.FilterEmailLogResponse](&f.Recorder, "FilterEmailLog", req)
}

func (f *fakeLogs) FilterGiftLog(_ context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error) {
	return handlertest.Answer[dto.FilterGiftLogResponse](&f.Recorder, "FilterGiftLog", req)
}

func (f *fakeLogs) FilterLoginLog(_ context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error) {
	return handlertest.Answer[dto.FilterLoginLogResponse](&f.Recorder, "FilterLoginLog", req)
}

func (f *fakeLogs) FilterMobileLog(_ context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error) {
	return handlertest.Answer[dto.FilterMobileLogResponse](&f.Recorder, "FilterMobileLog", req)
}

func (f *fakeLogs) FilterOrderLog(_ context.Context, req *dto.FilterOrderLogRequest) (*dto.FilterOrderLogResponse, error) {
	return handlertest.Answer[dto.FilterOrderLogResponse](&f.Recorder, "FilterOrderLog", req)
}

func (f *fakeLogs) FilterRegisterLog(_ context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error) {
	return handlertest.Answer[dto.FilterRegisterLogResponse](&f.Recorder, "FilterRegisterLog", req)
}

func (f *fakeLogs) FilterResetSubscribeLog(_ context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error) {
	return handlertest.Answer[dto.FilterResetSubscribeLogResponse](&f.Recorder, "FilterResetSubscribeLog", req)
}

func (f *fakeLogs) FilterServerTrafficLog(_ context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error) {
	return handlertest.Answer[dto.FilterServerTrafficLogResponse](&f.Recorder, "FilterServerTrafficLog", req)
}

func (f *fakeLogs) FilterSubscribeLog(_ context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error) {
	return handlertest.Answer[dto.FilterSubscribeLogResponse](&f.Recorder, "FilterSubscribeLog", req)
}

func (f *fakeLogs) FilterTrafficLogDetails(_ context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error) {
	return handlertest.Answer[dto.FilterTrafficLogDetailsResponse](&f.Recorder, "FilterTrafficLogDetails", req)
}

func (f *fakeLogs) FilterUserSubscribeTrafficLog(_ context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error) {
	return handlertest.Answer[dto.FilterSubscribeTrafficResponse](&f.Recorder, "FilterUserSubscribeTrafficLog", req)
}

func (f *fakeLogs) GetLogSetting(context.Context) (*dto.LogSetting, error) {
	return handlertest.Answer[dto.LogSetting](&f.Recorder, "GetLogSetting", nil)
}

func (f *fakeLogs) UpdateLogSetting(_ context.Context, req *dto.LogSetting) error {
	return f.Do("UpdateLogSetting", req)
}

func (f *fakeLogs) GetMessageLogList(_ context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error) {
	return handlertest.Answer[dto.GetMessageLogListResponse](&f.Recorder, "GetMessageLogList", req)
}

func (f *fakeLogs) FilterAdminActionLog(_ context.Context, req *dto.FilterAdminActionLogRequest) (*dto.FilterAdminActionLogResponse, error) {
	return handlertest.Answer[dto.FilterAdminActionLogResponse](&f.Recorder, "FilterAdminActionLog", req)
}

func (f *fakeLogs) FilterUnmatchedPaymentLog(_ context.Context, req *dto.FilterUnmatchedPaymentLogRequest) (*dto.FilterUnmatchedPaymentLogResponse, error) {
	return handlertest.Answer[dto.FilterUnmatchedPaymentLogResponse](&f.Recorder, "FilterUnmatchedPaymentLog", req)
}

// logRoutes registers the log handlers on their production routes.
func logRoutes(port Logs) *server.Hertz {
	h := server.New()
	group := h.Group("/v1/admin/log")
	group.GET("/admin/list", FilterAdminActionLogHandler(port))
	group.GET("/payment/unmatched/list", FilterUnmatchedPaymentLogHandler(port))
	group.GET("/balance/list", FilterBalanceLogHandler(port))
	group.GET("/commission/list", FilterCommissionLogHandler(port))
	group.GET("/email/list", FilterEmailLogHandler(port))
	group.GET("/gift/list", FilterGiftLogHandler(port))
	group.GET("/login/list", FilterLoginLogHandler(port))
	group.GET("/message/list", GetMessageLogListHandler(port))
	group.GET("/mobile/list", FilterMobileLogHandler(port))
	group.GET("/order/list", FilterOrderLogHandler(port))
	group.GET("/register/list", FilterRegisterLogHandler(port))
	group.GET("/server/traffic/list", FilterServerTrafficLogHandler(port))
	group.GET("/setting", GetLogSettingHandler(port))
	group.POST("/setting", UpdateLogSettingHandler(port))
	group.GET("/subscribe/list", FilterSubscribeLogHandler(port))
	group.GET("/subscribe/reset/list", FilterResetSubscribeLogHandler(port))
	group.GET("/subscribe/traffic/list", FilterUserSubscribeTrafficLogHandler(port))
	group.GET("/traffic/details", FilterTrafficLogDetailsHandler(port))
	return h
}

// window is the paging and date range every list case below asks for.
const window = "page=2&size=20&start_date=2026-09-01&end_date=2026-09-30&search=alice"

var windowParams = dto.FilterLogParams{Page: 2, Size: 20, StartDate: "2026-09-01", EndDate: "2026-09-30", Search: "alice"}

type logCase struct {
	name, httpMethod, target, body string
	call                           string
	request                        any // the request the port receives
	answer                         any // the port's answer, the reply's data
}

var logCases = []logCase{
	{name: "admin action", target: "/v1/admin/log/admin/list?user_id=7&" + window, call: "FilterAdminActionLog",
		request: &dto.FilterAdminActionLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer: &dto.FilterAdminActionLogResponse{Total: 1, List: []dto.AdminActionLog{{Id: 12, UserId: 7, Action: "settings.update", Object: "verify", Detail: "keys: TurnstileSecret",
			Source: "http", Timestamp: 1758000000000, CreatedAt: 1758000000000, ClientIP: "203.0.113.9", UserAgent: "AdminPanel/1.0"}}}},
	{name: "unmatched payment", target: "/v1/admin/log/payment/unmatched/list?user_id=7&" + window, call: "FilterUnmatchedPaymentLog",
		request: &dto.FilterUnmatchedPaymentLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer: &dto.FilterUnmatchedPaymentLogResponse{Total: 1, List: []dto.UnmatchedPaymentLog{{Id: 13, UserId: 7, OrderNo: "o-9", TradeNo: "t-9", Platform: "alipay",
			Amount: 1200, Currency: "CNY", Reason: "order already closed", Timestamp: 1758000000000, CreatedAt: 1758000000000}}}},
	{name: "balance", target: "/v1/admin/log/balance/list?user_id=7&" + window, call: "FilterBalanceLog",
		request: &dto.FilterBalanceLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterBalanceLogResponse{Total: 1, List: []dto.BalanceLog{{Type: 321, UserId: 7, Amount: -500, OrderNo: "o-1", Balance: 100, Timestamp: 1758000000000}}}},
	{name: "commission", target: "/v1/admin/log/commission/list?user_id=7&" + window, call: "FilterCommissionLog",
		request: &dto.FilterCommissionLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterCommissionLogResponse{Total: 1, List: []dto.CommissionLog{{Type: 331, UserId: 7, Amount: 80, OrderNo: "o-2"}}}},
	{name: "email", target: "/v1/admin/log/email/list?date=2026-09-02&" + window, call: "FilterEmailLog",
		request: &dto.FilterLogParams{Page: 2, Size: 20, Date: "2026-09-02", StartDate: "2026-09-01", EndDate: "2026-09-30", Search: "alice"},
		answer:  &dto.FilterEmailLogResponse{Total: 1, List: []dto.MessageLog{{Id: 3, Type: 10, Platform: "smtp", To: "alice@example.com", Subject: "code", Content: map[string]any{"code": "123456"}, Status: 1}}}},
	{name: "gift", target: "/v1/admin/log/gift/list?user_id=7&" + window, call: "FilterGiftLog",
		request: &dto.FilterGiftLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterGiftLogResponse{Total: 1, List: []dto.GiftLog{{Type: 341, UserId: 7, OrderNo: "o-3", SubscribeId: 2, Amount: 10, Remark: "welcome"}}}},
	{name: "login", target: "/v1/admin/log/login/list?user_id=7&" + window, call: "FilterLoginLog",
		request: &dto.FilterLoginLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterLoginLogResponse{Total: 1, List: []dto.LoginLog{{UserId: 7, Method: "email", LoginIP: "203.0.113.1", UserAgent: "app/1.0", Success: true}}}},
	{name: "message", target: "/v1/admin/log/message/list?page=1&size=10&type=11&search=%2B86", call: "GetMessageLogList",
		request: &dto.GetMessageLogListRequest{Page: 1, Size: 10, Type: 11, Search: "+86"},
		answer:  &dto.GetMessageLogListResponse{Total: 1, List: []dto.MessageLog{{Id: 4, Type: 11, Platform: "twilio", To: "+8613800000000", Status: 1}}}},
	{name: "mobile", target: "/v1/admin/log/mobile/list?" + window, call: "FilterMobileLog",
		request: &windowParams,
		answer:  &dto.FilterMobileLogResponse{Total: 1, List: []dto.MessageLog{{Id: 5, Type: 11, To: "+8613800000000"}}}},
	{name: "order", target: "/v1/admin/log/order/list?user_id=7&" + window, call: "FilterOrderLog",
		request: &dto.FilterOrderLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterOrderLogResponse{Total: 1, List: []dto.OrderLog{{Id: 6, UserId: 7, OrderNo: "o-4", OrderType: 1, Quantity: 1, Price: 1000, Amount: 1000, Method: "balance", Source: "web"}}}},
	{name: "register", target: "/v1/admin/log/register/list?user_id=7&" + window, call: "FilterRegisterLog",
		request: &dto.FilterRegisterLogRequest{FilterLogParams: windowParams, UserId: 7},
		answer:  &dto.FilterRegisterLogResponse{Total: 1, List: []dto.RegisterLog{{UserId: 7, AuthMethod: "email", Identifier: "alice@example.com", RegisterIP: "203.0.113.2"}}}},
	{name: "server traffic", target: "/v1/admin/log/server/traffic/list?server_id=4&" + window, call: "FilterServerTrafficLog",
		request: &dto.FilterServerTrafficLogRequest{FilterLogParams: windowParams, ServerId: 4},
		answer:  &dto.FilterServerTrafficLogResponse{Total: 1, List: []dto.ServerTrafficLog{{ServerId: 4, Upload: 1, Download: 2, Total: 3, Date: "2026-09-02", Details: true}}}},
	{name: "setting", target: "/v1/admin/log/setting", call: "GetLogSetting",
		answer: &dto.LogSetting{AutoClear: new(true), ClearDays: 30}},
	{name: "update setting", httpMethod: http.MethodPost, target: "/v1/admin/log/setting", body: `{"auto_clear":false,"clear_days":90}`, call: "UpdateLogSetting",
		request: &dto.LogSetting{AutoClear: new(false), ClearDays: 90}},
	{name: "subscribe", target: "/v1/admin/log/subscribe/list?user_id=7&user_subscribe_id=9&" + window, call: "FilterSubscribeLog",
		request: &dto.FilterSubscribeLogRequest{FilterLogParams: windowParams, UserId: 7, UserSubscribeId: 9},
		answer:  &dto.FilterSubscribeLogResponse{Total: 1, List: []dto.SubscribeLog{{UserId: 7, Token: "t-1", UserAgent: "clash", ClientIP: "203.0.113.3", UserSubscribeId: 9}}}},
	{name: "subscribe reset", target: "/v1/admin/log/subscribe/reset/list?user_subscribe_id=9&" + window, call: "FilterResetSubscribeLog",
		request: &dto.FilterResetSubscribeLogRequest{FilterLogParams: windowParams, UserSubscribeId: 9},
		answer:  &dto.FilterResetSubscribeLogResponse{Total: 1, List: []dto.ResetSubscribeLog{{Type: 232, UserId: 7, UserSubscribeId: 9}}}},
	{name: "subscribe traffic", target: "/v1/admin/log/subscribe/traffic/list?user_id=7&user_subscribe_id=9&" + window, call: "FilterUserSubscribeTrafficLog",
		request: &dto.FilterSubscribeTrafficRequest{FilterLogParams: windowParams, UserId: 7, UserSubscribeId: 9},
		answer:  &dto.FilterSubscribeTrafficResponse{Total: 1, List: []dto.UserSubscribeTrafficLog{{SubscribeId: 9, UserId: 7, Upload: 1, Download: 2, Total: 3, Date: "2026-09-02"}}}},
	{name: "traffic details", target: "/v1/admin/log/traffic/details?server_id=4&subscribe_id=9&user_id=7&" + window, call: "FilterTrafficLogDetails",
		request: &dto.FilterTrafficLogDetailsRequest{FilterLogParams: windowParams, ServerId: 4, SubscribeId: 9, UserId: 7},
		answer:  &dto.FilterTrafficLogDetailsResponse{Total: 1, List: []dto.TrafficLogDetails{{Id: 11, ServerId: 4, UserId: 7, SubscribeId: 9, Download: 2, Upload: 1}}}},
}

func (c logCase) method() string {
	if c.httpMethod == "" {
		return http.MethodGet
	}
	return c.httpMethod
}

// Each log route binds its query (or, for the setting, its JSON body) into
// the request the facade receives, and answers the facade's page as data.
func TestLogHandlersPassTheBoundFilterToTheFacade(t *testing.T) {
	for _, tc := range logCases {
		t.Run(tc.name, func(t *testing.T) {
			port := &fakeLogs{Recorder: handlertest.Recorder{Answers: map[string]any{tc.call: tc.answer}}}
			reply := handlertest.Serve(t, logRoutes(port), tc.method(), tc.target, tc.body)
			port.Called(t, tc.call, tc.request)
			reply.OK(t, tc.answer)
		})
	}
}

// A failure of the facade reaches the admin panel as its code and message.
func TestLogHandlersAnswerTheFacadeError(t *testing.T) {
	for _, tc := range logCases {
		t.Run(tc.name, func(t *testing.T) {
			port := &fakeLogs{Recorder: handlertest.Recorder{Err: xerr.Errorf(xerr.DatabaseQueryError, "logs unavailable")}}
			reply := handlertest.Serve(t, logRoutes(port), tc.method(), tc.target, tc.body)
			port.Called(t, tc.call, tc.request)
			reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
		})
	}
}

// A request that does not bind, or binds to an invalid filter or setting,
// is refused as a parameter error before the facade is asked anything.
func TestLogHandlersRefuseMalformedRequests(t *testing.T) {
	type refusal struct {
		name, httpMethod, target, body string
		msg                            string
	}
	var cases []refusal
	// Every list route binds the page from the query.
	for _, tc := range logCases {
		if tc.request != nil && tc.method() == http.MethodGet {
			path, _, _ := strings.Cut(tc.target, "?")
			cases = append(cases, refusal{tc.name + " page not a number", http.MethodGet, path + "?page=first&size=10", "", "bind Page"})
		}
	}
	cases = append(cases, []refusal{
		{"admin action user not a number", http.MethodGet, "/v1/admin/log/admin/list?page=1&size=10&user_id=me", "", "bind UserId"},
		{"admin action without size", http.MethodGet, "/v1/admin/log/admin/list?page=1", "", "Size is a required field"},
		{"unmatched payment size too large", http.MethodGet, "/v1/admin/log/payment/unmatched/list?page=1&size=101", "", "Size must be 100 or less"},
		{"setting keeps logs too short", http.MethodPost, "/v1/admin/log/setting", `{"auto_clear":true,"clear_days":6}`, "ClearDays must be 7 or greater"},
		{"balance user not a number", http.MethodGet, "/v1/admin/log/balance/list?page=1&size=10&user_id=me", "", "bind UserId"},
		{"balance without page", http.MethodGet, "/v1/admin/log/balance/list?size=10", "", "Page is a required field"},
		{"commission page zero", http.MethodGet, "/v1/admin/log/commission/list?page=0&size=10", "", "Page is a required field"},
		{"email size too large", http.MethodGet, "/v1/admin/log/email/list?page=1&size=101", "", "Size must be 100 or less"},
		{"gift without size", http.MethodGet, "/v1/admin/log/gift/list?page=1", "", "Size is a required field"},
		{"login start date not a date", http.MethodGet, "/v1/admin/log/login/list?page=1&size=10&start_date=2026-02-30", "", "StartDate"},
		{"message without type", http.MethodGet, "/v1/admin/log/message/list?page=1&size=10", "", "Type is a required field"},
		{"message type unknown", http.MethodGet, "/v1/admin/log/message/list?page=1&size=10&type=12", "", "Type must be one of"},
		{"mobile end date not a date", http.MethodGet, "/v1/admin/log/mobile/list?page=1&size=10&end_date=30.09.2026", "", "EndDate"},
		{"order size zero", http.MethodGet, "/v1/admin/log/order/list?page=1&size=0", "", "Size is a required field"},
		{"register negative page", http.MethodGet, "/v1/admin/log/register/list?page=-1&size=10", "", "Page must be greater than 0"},
		{"server traffic server not a number", http.MethodGet, "/v1/admin/log/server/traffic/list?page=1&size=10&server_id=tokyo", "", "bind ServerId"},
		{"server traffic without page", http.MethodGet, "/v1/admin/log/server/traffic/list?size=10", "", "Page is a required field"},
		{"setting not JSON", http.MethodPost, "/v1/admin/log/setting", `{"auto_clear":`, ""},
		{"setting days not a number", http.MethodPost, "/v1/admin/log/setting", `{"auto_clear":true,"clear_days":"week"}`, ""},
		{"setting without switch", http.MethodPost, "/v1/admin/log/setting", `{"clear_days":7}`, "AutoClear is a required field"},
		{"setting keeps logs too long", http.MethodPost, "/v1/admin/log/setting", `{"auto_clear":true,"clear_days":3651}`, "ClearDays must be 3,650 or less"},
		{"subscribe user subscription not a number", http.MethodGet, "/v1/admin/log/subscribe/list?page=1&size=10&user_subscribe_id=x", "", "bind UserSubscribeId"},
		{"subscribe size too large", http.MethodGet, "/v1/admin/log/subscribe/list?page=1&size=500", "", "Size must be 100 or less"},
		{"subscribe reset without page", http.MethodGet, "/v1/admin/log/subscribe/reset/list?size=10", "", "Page is a required field"},
		{"subscribe traffic end date not a date", http.MethodGet, "/v1/admin/log/subscribe/traffic/list?page=1&size=10&end_date=yesterday", "", "EndDate"},
		{"traffic details subscription not a number", http.MethodGet, "/v1/admin/log/traffic/details?page=1&size=10&subscribe_id=-", "", "bind SubscribeId"},
		{"traffic details without size", http.MethodGet, "/v1/admin/log/traffic/details?page=1", "", "Size is a required field"},
	}...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := &fakeLogs{}
			reply := handlertest.Serve(t, logRoutes(port), tc.httpMethod, tc.target, tc.body)
			reply.Refused(t, xerr.InvalidParams, tc.msg)
			if len(port.Calls) != 0 {
				t.Fatalf("calls = %+v, want the facade not asked", port.Calls)
			}
		})
	}
}
