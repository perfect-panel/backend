package log

import (
	"context"
	"reflect"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateLogSettingLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateLogSettingLogic Update log setting
func NewUpdateLogSettingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateLogSettingLogic {
	return &UpdateLogSettingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateLogSettingLogic) UpdateLogSetting(req *dto.LogSetting) error {
	v := reflect.ValueOf(*req)
	// Get the reflection type of the structure
	t := v.Type()
	err := l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		systemStore := store.System()
		for i := 0; i < v.NumField(); i++ {
			// Get the field name
			fieldName := t.Field(i).Name
			// Get the field value to string
			fieldValue := tool.ConvertValueToString(v.Field(i))
			if err := systemStore.UpdateValueByCategoryKey(l.ctx, "log", fieldName, fieldValue); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		l.Errorw("[UpdateLogSetting] update log setting error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " update log setting error: %v", err)
	}

	l.svcCtx.Config.Log = config.Log{
		AutoClear: *req.AutoClear,
		ClearDays: req.ClearDays,
	}

	return nil
}
