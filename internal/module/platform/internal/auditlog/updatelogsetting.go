package auditlog

import (
	"context"
	"fmt"
	"strconv"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/repository/kernel"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The retention an administrator may set, in days. The floor keeps the
// login, registration and subscription logs long enough to investigate an
// incident noticed days later: a retention of a day would let the 02:30
// cleanup erase the trail before anyone looked.
const (
	MinRetentionDays = 7
	MaxRetentionDays = 3650
)

// UpdateLogSetting stores the log retention settings and, once they are
// committed, propagates them to the running configuration. The change is
// recorded in the audit trail with its values, which are not secret and are
// what an investigation needs to know: who shortened the retention, and to
// what.
func (s *Service) UpdateLogSetting(ctx context.Context, req *dto.LogSetting) error {
	if err := validateLogSetting(req); err != nil {
		return err
	}
	err := s.deps.Store.InPlatformTx(ctx, func(store kernel.PlatformStore) error {
		systemStore := store.System()
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "AutoClear", strconv.FormatBool(*req.AutoClear), "bool"); err != nil {
			return err
		}
		if err := systemStore.UpdateValueByCategoryKey(ctx, "log", "ClearDays", strconv.FormatInt(req.ClearDays, 10), "int64"); err != nil {
			return err
		}
		row, err := log.NewAdminActionLog(log.AdminActionFrom(ctx, log.AdminAction{
			Action: "settings.update",
			Object: "log",
			Detail: fmt.Sprintf("keys: AutoClear, ClearDays; auto_clear=%t clear_days=%d", *req.AutoClear, req.ClearDays),
		}))
		if err != nil {
			return err
		}
		return store.Log().Insert(ctx, row)
	})
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateLogSetting] update log setting error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, " update log setting error: %v", err)
	}

	if s.deps.OnLogSettingChanged != nil {
		s.deps.OnLogSettingChanged(*req.AutoClear, req.ClearDays)
	}
	return nil
}

func validateLogSetting(req *dto.LogSetting) error {
	if req == nil || req.AutoClear == nil || req.ClearDays < MinRetentionDays || req.ClearDays > MaxRetentionDays {
		return xerr.Errorf(xerr.InvalidParams, "log retention requires auto_clear and clear_days between %d and %d", MinRetentionDays, MaxRetentionDays)
	}
	return nil
}
