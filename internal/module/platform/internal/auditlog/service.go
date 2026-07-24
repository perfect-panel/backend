// Package auditlog implements the audit/message log subdomain of the
// platform module: filtered views over the system log, message log listing
// and the log retention settings. Only the module facade may reach it.
package auditlog

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
)

// TrafficReader is the subdomain's port onto the network domain's traffic
// statistics; the legacy traffic repository satisfies it structurally.
type TrafficReader = repository.TrafficRepo

// PlatformTransactor mirrors the store's platform-scoped transaction.
type PlatformTransactor interface {
	InPlatformTx(ctx context.Context, fn func(repository.PlatformStore) error) error
}

type Deps struct {
	Logs    repository.LogRepo
	System  repository.SystemRepo
	Traffic TrafficReader
	Store   PlatformTransactor
	// OnLogSettingChanged propagates a committed retention change to the
	// running configuration.
	OnLogSettingChanged func(autoClear bool, clearDays int64)
	// LogRetention reads the current (mutable) retention configuration.
	LogRetention func() (autoClear bool, clearDays int64)
}

func (d Deps) logRetention() (bool, int64) {
	if d.LogRetention == nil {
		return false, 0
	}
	return d.LogRetention()
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

func (s *Service) FilterBalanceLog(ctx context.Context, req *dto.FilterBalanceLogRequest) (*dto.FilterBalanceLogResponse, error) {
	return newFilterBalanceLogLogic(ctx, s.deps).FilterBalanceLog(req)
}

func (s *Service) FilterCommissionLog(ctx context.Context, req *dto.FilterCommissionLogRequest) (*dto.FilterCommissionLogResponse, error) {
	return newFilterCommissionLogLogic(ctx, s.deps).FilterCommissionLog(req)
}

func (s *Service) FilterEmailLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterEmailLogResponse, error) {
	return newFilterEmailLogLogic(ctx, s.deps).FilterEmailLog(req)
}

func (s *Service) FilterGiftLog(ctx context.Context, req *dto.FilterGiftLogRequest) (*dto.FilterGiftLogResponse, error) {
	return newFilterGiftLogLogic(ctx, s.deps).FilterGiftLog(req)
}

func (s *Service) FilterLoginLog(ctx context.Context, req *dto.FilterLoginLogRequest) (*dto.FilterLoginLogResponse, error) {
	return newFilterLoginLogLogic(ctx, s.deps).FilterLoginLog(req)
}

func (s *Service) FilterMobileLog(ctx context.Context, req *dto.FilterLogParams) (*dto.FilterMobileLogResponse, error) {
	return newFilterMobileLogLogic(ctx, s.deps).FilterMobileLog(req)
}

func (s *Service) FilterRegisterLog(ctx context.Context, req *dto.FilterRegisterLogRequest) (*dto.FilterRegisterLogResponse, error) {
	return newFilterRegisterLogLogic(ctx, s.deps).FilterRegisterLog(req)
}

func (s *Service) FilterResetSubscribeLog(ctx context.Context, req *dto.FilterResetSubscribeLogRequest) (*dto.FilterResetSubscribeLogResponse, error) {
	return newFilterResetSubscribeLogLogic(ctx, s.deps).FilterResetSubscribeLog(req)
}

func (s *Service) FilterServerTrafficLog(ctx context.Context, req *dto.FilterServerTrafficLogRequest) (*dto.FilterServerTrafficLogResponse, error) {
	return newFilterServerTrafficLogLogic(ctx, s.deps).FilterServerTrafficLog(req)
}

func (s *Service) FilterSubscribeLog(ctx context.Context, req *dto.FilterSubscribeLogRequest) (*dto.FilterSubscribeLogResponse, error) {
	return newFilterSubscribeLogLogic(ctx, s.deps).FilterSubscribeLog(req)
}

func (s *Service) FilterTrafficLogDetails(ctx context.Context, req *dto.FilterTrafficLogDetailsRequest) (*dto.FilterTrafficLogDetailsResponse, error) {
	return newFilterTrafficLogDetailsLogic(ctx, s.deps).FilterTrafficLogDetails(req)
}

func (s *Service) FilterUserSubscribeTrafficLog(ctx context.Context, req *dto.FilterSubscribeTrafficRequest) (*dto.FilterSubscribeTrafficResponse, error) {
	return newFilterUserSubscribeTrafficLogLogic(ctx, s.deps).FilterUserSubscribeTrafficLog(req)
}

func (s *Service) GetLogSetting(ctx context.Context) (*dto.LogSetting, error) {
	return newGetLogSettingLogic(ctx, s.deps).GetLogSetting()
}

func (s *Service) UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error {
	return newUpdateLogSettingLogic(ctx, s.deps).UpdateLogSetting(req)
}

func (s *Service) GetMessageLogList(ctx context.Context, req *dto.GetMessageLogListRequest) (*dto.GetMessageLogListResponse, error) {
	return newGetMessageLogListLogic(ctx, s.deps).GetMessageLogList(req)
}
