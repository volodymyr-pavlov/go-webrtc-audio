package config

import (
	"fmt"
	"reflect"
)

// validateFields returns an error naming the first mandatory field of the environment, a pointer to a struct, that is a
// nil pointer, interface, function or channel. A field tagged env:"optional" may be nil.
func validateFields(env any) error {
	value := reflect.ValueOf(env).Elem()
	for i := range value.NumField() {
		field := value.Field(i)
		switch field.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Chan:
			if field.IsNil() && value.Type().Field(i).Tag.Get("env") != "optional" {
				return fmt.Errorf("%s %w", value.Type().Field(i).Name, ErrMissingFromEnv)
			}
		}
	}
	return nil
}
