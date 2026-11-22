package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/perfect-panel/backend/pkg/logger"
)

// SystemConfigEntry is the narrow data contract needed to apply persisted
// configuration to a Go struct. It keeps this reflection helper independent
// from the database entity that happens to supply the values.
type SystemConfigEntry interface {
	ConfigKey() string
	ConfigValue() string
	ConfigType() string
}

// SettingError reports a stored setting whose value could not be applied to
// the field its key names. It carries the key and the stored type but never
// the value, which may be a credential.
type SettingError struct {
	Key  string
	Type string
	Err  error
}

func (e *SettingError) Error() string {
	return fmt.Sprintf("setting %s (type %q): %v", e.Key, e.Type, e.Err)
}

func (e *SettingError) Unwrap() error { return e.Err }

// DecodeSystemConfig applies persisted settings to the struct target points
// to. Each entry is matched to the exported field named like its key; keys
// without such a field are skipped, because one category can hold settings
// that another reader owns. The stored type selects the conversion:
//
//   - "string": the value as is;
//   - "bool", "int", "int64": the parsed value, an empty value meaning zero;
//   - "interface": the value decoded as JSON, an empty value leaving the field
//     untouched.
//
// A *bool field takes a boolean of any stored type, an empty value meaning
// unset (nil).
//
// Every setting that cannot be applied is reported in the returned error, one
// *SettingError per key joined with errors.Join, and the remaining settings
// are still applied. A value that does not parse leaves its field at the zero
// value, which is what the previous, silent implementation produced, and a
// stored type the field cannot hold leaves the field untouched instead of
// panicking.
func DecodeSystemConfig[T SystemConfigEntry](entries []T, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("decode settings: target must be a non-nil pointer to a struct, got %T", target)
	}
	v = v.Elem()
	var errs []error
	for _, entry := range entries {
		field := v.FieldByName(entry.ConfigKey())
		if !field.IsValid() || !field.CanSet() {
			continue
		}
		if err := applySetting(field, entry.ConfigType(), entry.ConfigValue()); err != nil {
			errs = append(errs, &SettingError{Key: entry.ConfigKey(), Type: entry.ConfigType(), Err: err})
		}
	}
	return errors.Join(errs...)
}

// SystemConfigSliceReflectToStruct applies settings like DecodeSystemConfig
// for callers that cannot act on a malformed setting: every setting it could
// not apply is logged with its key instead of being returned.
func SystemConfigSliceReflectToStruct[T SystemConfigEntry](slice []T, structType any) {
	if err := DecodeSystemConfig(slice, structType); err != nil {
		logger.Errorw("[SystemConfig] stored settings could not be applied", logger.Field("error", err.Error()))
	}
}

func applySetting(field reflect.Value, valueType, value string) error {
	if field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.Bool {
		if value == "" {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		parsed, err := strconv.ParseBool(value)
		field.Set(reflect.ValueOf(&parsed))
		return err
	}

	switch valueType {
	case "string":
		if field.Kind() != reflect.String {
			return storedTypeMismatch(field)
		}
		field.SetString(value)
		return nil
	case "bool":
		if field.Kind() != reflect.Bool {
			return storedTypeMismatch(field)
		}
		if value == "" {
			field.SetBool(false)
			return nil
		}
		parsed, err := strconv.ParseBool(value)
		field.SetBool(parsed)
		return err
	case "int", "int64":
		if !isSignedInt(field.Kind()) {
			return storedTypeMismatch(field)
		}
		if value == "" {
			field.SetInt(0)
			return nil
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil && field.OverflowInt(parsed) {
			parsed, err = 0, fmt.Errorf("%d overflows %s", parsed, field.Type())
		}
		field.SetInt(parsed)
		return err
	case "interface":
		if value == "" {
			return nil
		}
		// An "interface" row holds a JSON document. A string field keeps
		// it as text (the node DNS, Block and Outbound settings are stored
		// that way and decoded by their readers); other fields decode it.
		if field.Kind() == reflect.String {
			field.SetString(value)
			return nil
		}
		return json.Unmarshal([]byte(value), field.Addr().Interface())
	default:
		return errors.New("unsupported stored type")
	}
}

func isSignedInt(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	}
	return false
}

func storedTypeMismatch(field reflect.Value) error {
	return fmt.Errorf("stored type does not fit a field of type %s", field.Type())
}
