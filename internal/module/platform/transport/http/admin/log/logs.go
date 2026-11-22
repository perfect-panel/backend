package log

import (
	"context"

	"github.com/perfect-panel/backend/internal/module/platform"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
)

// Logs is the part of the platform facade the log handlers call: the audit
// and message log views and the log retention settings.
type Logs interface {
	FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error)
	FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error)
	FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error)
	FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error)
	FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error)
	FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error)
	FilterOrderLog(ctx context.Context, req *dto.FilterOrderLogRequest) (*dto.FilterOrderLogResponse, error)
	FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error)
	FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error)
	FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error)
	FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error)
	FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error)
	FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error)
	GetLogSetting(ctx context.Context) (*dto.LogSetting, error)
	UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error
	GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error)
	FilterAdminActionLog(ctx context.Context, req *dto.FilterAdminActionLogRequest) (*dto.FilterAdminActionLogResponse, error)
	FilterUnmatchedPaymentLog(ctx context.Context, req *dto.FilterUnmatchedPaymentLogRequest) (*dto.FilterUnmatchedPaymentLogResponse, error)
}

// The platform facade serves the log handlers.
var _ Logs = platform.Service(nil)
