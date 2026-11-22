package systemsetting

import (
	"context"
	"reflect"
	"strings"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/pkg/logger"
)

type configFieldValue struct {
	key       string
	value     string
	valueType string
}

func convertedConfigFields(data any) []configFieldValue {
	return configFields(data, config.ConvertValueToString)
}

func stringConfigFields(data any) []configFieldValue {
	return configFields(data, func(value reflect.Value) string {
		return value.String()
	})
}

func configFields(data any, valueFn func(reflect.Value) string) []configFieldValue {
	v := reflect.ValueOf(data)
	t := v.Type()
	fields := make([]configFieldValue, 0, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fields = append(fields, configFieldValue{
			key:       t.Field(i).Name,
			value:     valueFn(field),
			valueType: configFieldType(field),
		})
	}
	return fields
}

func configFieldType(value reflect.Value) string {
	if value.Kind() == reflect.Pointer {
		value = reflect.New(value.Type().Elem()).Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int64"
	case reflect.Interface, reflect.Map, reflect.Slice, reflect.Struct:
		return "interface"
	default:
		return "string"
	}
}

// settingsChange is what one settings update stores: the fields of the
// category as they are to be stored, and the same fields as they were, when
// known. previous is nil when the stored settings could not be read, and
// then every key written is recorded as changed.
type settingsChange struct {
	category string
	next     []configFieldValue
	previous []configFieldValue
}

// changedKeys names the fields whose stored value the update changes; with
// no previous values every field counts.
func (c settingsChange) changedKeys() []string {
	if c.previous == nil {
		keys := make([]string, 0, len(c.next))
		for _, field := range c.next {
			keys = append(keys, field.key)
		}
		return keys
	}
	before := make(map[string]string, len(c.previous))
	for _, field := range c.previous {
		before[field.key] = field.value
	}
	var keys []string
	for _, field := range c.next {
		if value, ok := before[field.key]; !ok || value != field.value {
			keys = append(keys, field.key)
		}
	}
	return keys
}

// updateConfigFields stores the fields of one settings category in a single
// transaction, with the audit row naming the administrator and the keys the
// update changed. Values are never recorded: several categories hold
// secrets.
func updateConfigFields(ctx context.Context, deps Deps, change settingsChange) error {
	changed := change.changedKeys()
	return deps.Store.InSettingsTx(ctx, func(settings SettingsStore) error {
		for _, field := range change.next {
			if err := settings.UpdateValueByCategoryKey(ctx, change.category, field.key, field.value, field.valueType); err != nil {
				return err
			}
		}
		if len(changed) == 0 {
			return nil
		}
		return recordSettingsAction(ctx, settings, "settings.update", change.category, "keys: "+strings.Join(changed, ", "))
	})
}

// recordSettingsAction writes the audit row of an administrator's settings
// action on object (the settings category or subsystem).
func recordSettingsAction(ctx context.Context, audit AuditWriter, action, object, detail string) error {
	row, err := log.NewAdminActionLog(log.AdminActionFrom(ctx, log.AdminAction{Action: action, Object: object, Detail: detail}))
	if err != nil {
		return err
	}
	return audit.Insert(ctx, row)
}

// previousFields reads the stored settings of a category through read, as
// the fields the update compares against. A read that fails is logged and
// leaves the previous values unknown: the update still goes through, and
// its audit row then names every key written.
func previousFields[T any](ctx context.Context, category string, read func(context.Context) (*T, error), fields func(any) []configFieldValue) []configFieldValue {
	stored, err := read(ctx)
	if err != nil || stored == nil {
		if err != nil {
			logger.WithContext(ctx).Errorw("[Settings] stored "+category+" settings could not be read before the update",
				logger.Field("error", err.Error()))
		}
		return nil
	}
	return fields(*stored)
}
