// Package platform is the facade of the platform module (shared-kernel
// concerns: audit/message logs and their retention settings; system
// configuration joins as migration proceeds). See
// docs/adr-001-modular-monolith.md.
package platform

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/module/platform/internal/auditlog"
	"github.com/perfect-panel/server/internal/repository"
)

// Service is the only surface other code may depend on; the implementation
// lives under internal/ where the compiler seals it off.
type Service interface {
	FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error)
	FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error)
	FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error)
	FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error)
	FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error)
	FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error)
	FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error)
	FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error)
	FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error)
	FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error)
	FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error)
	FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error)
	GetLogSetting(ctx context.Context) (*dto.LogSetting, error)
	// UpdateLogSetting persists the retention settings and propagates them to
	// the running configuration.
	UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error
	GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error)
}

// Deps declares everything the module needs; the composition root
// (internal/svc) provides them.
type Deps struct {
	Logs    repository.LogRepo
	System  repository.SystemRepo
	Traffic auditlog.TrafficReader
	Store   auditlog.PlatformTransactor
	// OnLogSettingChanged propagates a committed retention change to the
	// running configuration.
	OnLogSettingChanged func(autoClear bool, clearDays int64)
	// LogRetention reads the current (mutable) retention configuration.
	LogRetention func() (autoClear bool, clearDays int64)
}

func New(deps Deps) Service {
	return &service{
		logs: auditlog.NewService(auditlog.Deps{
			Logs:                deps.Logs,
			System:              deps.System,
			Traffic:             deps.Traffic,
			Store:               deps.Store,
			OnLogSettingChanged: deps.OnLogSettingChanged,
			LogRetention:        deps.LogRetention,
		}),
	}
}

type service struct {
	logs *auditlog.Service
}

func (s *service) FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error) {
	return s.logs.FilterBalanceLog(ctx, req)
}

func (s *service) FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error) {
	return s.logs.FilterCommissionLog(ctx, req)
}

func (s *service) FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error) {
	return s.logs.FilterEmailLog(ctx, req)
}

func (s *service) FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error) {
	return s.logs.FilterGiftLog(ctx, req)
}

func (s *service) FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error) {
	return s.logs.FilterLoginLog(ctx, req)
}

func (s *service) FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error) {
	return s.logs.FilterMobileLog(ctx, req)
}

func (s *service) FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error) {
	return s.logs.FilterRegisterLog(ctx, req)
}

func (s *service) FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error) {
	return s.logs.FilterResetSubscribeLog(ctx, req)
}

func (s *service) FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error) {
	return s.logs.FilterServerTrafficLog(ctx, req)
}

func (s *service) FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error) {
	return s.logs.FilterSubscribeLog(ctx, req)
}

func (s *service) FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error) {
	return s.logs.FilterTrafficLogDetails(ctx, req)
}

func (s *service) FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error) {
	return s.logs.FilterUserSubscribeTrafficLog(ctx, req)
}

func (s *service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	return s.logs.GetLogSetting(ctx)
}

func (s *service) UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error {
	return s.logs.UpdateLogSetting(ctx, req)
}

func (s *service) GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error) {
	return s.logs.GetMessageLogList(ctx, req)
}
