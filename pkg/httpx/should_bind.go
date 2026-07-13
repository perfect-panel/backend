package httpx

import (
	"fmt"
	"mime"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

// ShouldBind preserves endpoint request-binding behavior for native Hertz contexts.
func ShouldBind(ctx *app.RequestContext, destination any) error {
	if isJSONRequest(ctx) {
		return ctx.BindJSON(destination)
	}
	if err := bindValues(destination, queryValues(ctx)); err != nil {
		return err
	}
	if len(ctx.Request.Body()) == 0 {
		return nil
	}
	return ctx.Bind(destination)
}

func isJSONRequest(ctx *app.RequestContext) bool {
	contentType := ctx.Request.Header.Get("Content-Type")
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(contentType, "json")
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func queryValues(ctx *app.RequestContext) url.Values {
	return (&url.URL{RawQuery: string(ctx.URI().QueryString())}).Query()
}

func bindValues(destination any, values url.Values) error {
	if destination == nil {
		return nil
	}
	value := reflect.ValueOf(destination)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return fmt.Errorf("bind target must be a non-nil pointer")
	}
	return bindValue(value.Elem(), values)
}

func bindValue(value reflect.Value, values url.Values) error {
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return bindValue(value.Elem(), values)
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		structField := valueType.Field(index)
		if structField.PkgPath != "" && !structField.Anonymous {
			continue
		}
		if structField.Anonymous {
			if err := bindValue(field, values); err != nil {
				return err
			}
			continue
		}
		name := fieldName(structField)
		if name == "" {
			continue
		}
		raw, found := values[name]
		if !found && field.Kind() == reflect.Slice {
			raw, found = values[name+"[]"]
		}
		if !found || len(raw) == 0 {
			continue
		}
		if err := setField(field, raw); err != nil {
			return fmt.Errorf("bind %s: %w", structField.Name, err)
		}
	}
	return nil
}

func fieldName(structField reflect.StructField) string {
	for _, key := range []string{"form", "query", "uri", "path", "json"} {
		tag := structField.Tag.Get(key)
		if tag == "-" {
			return ""
		}
		if tag != "" {
			return strings.Split(tag, ",")[0]
		}
	}
	return structField.Name
}

func setField(field reflect.Value, raw []string) error {
	if !field.CanSet() {
		return nil
	}
	if field.Kind() == reflect.Ptr {
		if len(raw) == 0 || raw[0] == "" {
			return nil
		}
		field.Set(reflect.New(field.Type().Elem()))
		return setField(field.Elem(), raw)
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw[0])
	case reflect.Bool:
		value, err := strconv.ParseBool(raw[0])
		if err != nil {
			return err
		}
		field.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(raw[0], 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(raw[0], 10, field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetUint(value)
	case reflect.Float32, reflect.Float64:
		value, err := strconv.ParseFloat(raw[0], field.Type().Bits())
		if err != nil {
			return err
		}
		field.SetFloat(value)
	case reflect.Slice:
		slice := reflect.MakeSlice(field.Type(), 0, len(raw))
		for _, item := range raw {
			element := reflect.New(field.Type().Elem()).Elem()
			if err := setField(element, []string{item}); err != nil {
				return err
			}
			slice = reflect.Append(slice, element)
		}
		field.Set(slice)
	case reflect.Struct:
		return nil
	}
	return nil
}
