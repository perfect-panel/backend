package auditlog

import (
	"context"
	"reflect"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateLogSettingLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// NewUpdateLogSettingLogic Update log setting
func newUpdateLogSettingLogic(ctx context.Context, deps Deps) *UpdateLogSettingLogic {
	return &UpdateLogSettingLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *UpdateLogSettingLogic) UpdateLogSetting(req *dto.LogSetting) error {
	v := reflect.ValueOf(*req)
	// Get the reflection type of the structure
	t := v.Type()
	err := l.deps.Store.InPlatformTx(l.ctx, func(store repository.PlatformStore) error {
		systemStore := store.System()
		for i := 0; i < v.NumField(); i++ {
			// Get the field name
			fieldName := t.Field(i).Name
			// Get the field value to string
			field := v.Field(i)
			fieldValue := tool.ConvertValueToString(field)
			fieldType := "string"
			if field.Kind() == reflect.Ptr {
				field = reflect.New(field.Type().Elem()).Elem()
			}
			switch field.Kind() {
			case reflect.Bool:
				fieldType = "bool"
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				fieldType = "int64"
			}
			if err := systemStore.UpdateValueByCategoryKey(l.ctx, "log", fieldName, fieldValue, fieldType); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		l.Errorw("[UpdateLogSetting] update log setting error", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), " update log setting error: %v", err)
	}

	if l.deps.OnLogSettingChanged != nil {
		l.deps.OnLogSettingChanged(*req.AutoClear, req.ClearDays)
	}

	return nil
}
