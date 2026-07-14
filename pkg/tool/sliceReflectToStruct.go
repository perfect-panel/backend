package tool

import (
	"reflect"
	"strconv"

	"github.com/goccy/go-json"
	"github.com/perfect-panel/server/internal/model/entity/system"
)

func SystemConfigSliceReflectToStruct(slice []*system.System, structType any) {
	v := reflect.ValueOf(structType).Elem()

	for _, config := range slice {
		field := v.FieldByName(config.Key)
		if field.Kind() == reflect.Ptr && field.Type().Elem().Kind() == reflect.Bool {
			if config.Value == "" {
				field.Set(reflect.Zero(field.Type()))
			} else {
				boolValue, _ := strconv.ParseBool(config.Value)
				field.Set(reflect.ValueOf(&boolValue))
			}
			continue
		}

		if field.IsValid() && field.CanSet() {
			switch config.Type {
			case "string":
				field.SetString(config.Value)
			case "bool":
				boolValue, _ := strconv.ParseBool(config.Value)
				field.SetBool(boolValue)
			case "int":
				intValue, _ := strconv.Atoi(config.Value)
				field.SetInt(int64(intValue))
			case "int64":
				intValue, _ := strconv.ParseInt(config.Value, 10, 64)
				field.SetInt(intValue)
			case "interface":
				_ = json.Unmarshal([]byte(config.Value), field.Addr().Interface())
			default:
				break
			}
		}
	}
}
