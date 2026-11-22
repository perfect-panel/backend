package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/transport/http/internal/handlertest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// fakeDashboard is the dashboard port: it records every call and answers
// the recorder's answer for the method.
type fakeDashboard struct{ handlertest.Recorder }

var _ Dashboard = (*fakeDashboard)(nil)

func (f *fakeDashboard) QueryRevenueStatistics(context.Context) (*dto.RevenueStatisticsResponse, error) {
	return handlertest.Answer[dto.RevenueStatisticsResponse](&f.Recorder, "QueryRevenueStatistics", nil)
}

func (f *fakeDashboard) QueryServerTotalData(context.Context) (*dto.ServerTotalDataResponse, error) {
	return handlertest.Answer[dto.ServerTotalDataResponse](&f.Recorder, "QueryServerTotalData", nil)
}

func (f *fakeDashboard) QueryTicketWaitReply(context.Context) (*dto.TicketWaitRelpyResponse, error) {
	return handlertest.Answer[dto.TicketWaitRelpyResponse](&f.Recorder, "QueryTicketWaitReply", nil)
}

func (f *fakeDashboard) QueryUserStatistics(context.Context) (*dto.UserStatisticsResponse, error) {
	return handlertest.Answer[dto.UserStatisticsResponse](&f.Recorder, "QueryUserStatistics", nil)
}

// consoleRoutes registers the console handlers on their production routes.
func consoleRoutes(port Dashboard) *server.Hertz {
	h := server.New()
	h.GET("/v1/admin/console/revenue", QueryRevenueStatisticsHandler(port))
	h.GET("/v1/admin/console/server", QueryServerTotalDataHandler(port))
	h.GET("/v1/admin/console/ticket", QueryTicketWaitReplyHandler(port))
	h.GET("/v1/admin/console/user", QueryUserStatisticsHandler(port))
	return h
}

var consoleCases = []struct {
	target, method string
	answer         any
}{
	{"/v1/admin/console/revenue", "QueryRevenueStatistics", &dto.RevenueStatisticsResponse{
		Today: dto.OrdersStatistics{AmountTotal: 1200, NewOrderAmount: 1000, RenewalOrderAmount: 200},
		All:   dto.OrdersStatistics{AmountTotal: 9900, List: []dto.OrdersStatistics{{Date: "2026-09", AmountTotal: 9900}}},
	}},
	{"/v1/admin/console/server", "QueryServerTotalData", &dto.ServerTotalDataResponse{
		OnlineUsers: 3, OnlineServers: 2, TodayUpload: 10,
		ServerTrafficRankingToday: []dto.ServerTrafficData{{ServerId: 4, Name: "tokyo", Upload: 10}},
		UserTrafficRankingToday:   []dto.UserTrafficData{{SID: 8, UID: 5, Download: 20}},
	}},
	{"/v1/admin/console/ticket", "QueryTicketWaitReply", &dto.TicketWaitRelpyResponse{Count: 6}},
	{"/v1/admin/console/user", "QueryUserStatistics", &dto.UserStatisticsResponse{
		Today: dto.UserStatistics{Register: 2, NewOrderUsers: 1}, Monthly: dto.UserStatistics{Register: 30},
	}},
}

// Each console route asks the dashboard for its figures once and answers
// them as the envelope's data.
func TestConsoleHandlersAnswerTheDashboardFigures(t *testing.T) {
	for _, tc := range consoleCases {
		t.Run(tc.method, func(t *testing.T) {
			port := &fakeDashboard{Recorder: handlertest.Recorder{Answers: map[string]any{tc.method: tc.answer}}}
			reply := handlertest.Serve(t, consoleRoutes(port), http.MethodGet, tc.target, "")
			port.Called(t, tc.method, nil)
			reply.OK(t, tc.answer)
		})
	}
}

// A failed figure reaches the console as the failure's code and message,
// still with HTTP 200, as every API error does.
func TestConsoleHandlersAnswerTheDashboardError(t *testing.T) {
	for _, tc := range consoleCases {
		t.Run(tc.method, func(t *testing.T) {
			port := &fakeDashboard{Recorder: handlertest.Recorder{Err: xerr.Errorf(xerr.DatabaseQueryError, "orders unavailable")}}
			reply := handlertest.Serve(t, consoleRoutes(port), http.MethodGet, tc.target, "")
			port.Called(t, tc.method, nil)
			reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
		})
	}
}
