package console

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// Dashboard is the part of the platform facade the console handlers call.
type Dashboard interface {
	QueryRevenueStatistics(ctx context.Context) (*dto.RevenueStatisticsResponse, error)
	QueryServerTotalData(ctx context.Context) (*dto.ServerTotalDataResponse, error)
	QueryTicketWaitReply(ctx context.Context) (*dto.TicketWaitRelpyResponse, error)
	QueryUserStatistics(ctx context.Context) (*dto.UserStatisticsResponse, error)
}

// The platform facade serves the console handlers.
var _ Dashboard = platform.Service(nil)
