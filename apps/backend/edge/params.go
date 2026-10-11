package edge

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/valyala/bytebufferpool"
)

const (
	placeholderPrefix = "%("
	placeholderSuffix = ")s"
)

// Params are the values that explain an [Error], keyed by name.
type Params map[string]any

// paramsFrom returns source as [Params]: a [Params] value as it is, and a struct, or a pointer to one, as the exported
// fields under their json names. A field with the json tag "-" is left out, and so is a zero field with omitempty.
// Anything else gives nil.
func paramsFrom(source any) Params {
	if params, ok := source.(Params); ok {
		return params
	}
	value := reflect.ValueOf(source)
	for value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	params := Params{}
	for i := range value.NumField() {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "" {
			name = field.Name
		}
		if strings.Contains(","+options+",", ",omitempty,") && value.Field(i).IsZero() {
			continue
		}
		params[name] = value.Field(i).Interface()
	}
	return params
}

// String returns the params as key=value pairs sorted by key.
func (p Params) String() string {
	bp := bytebufferpool.Get()
	defer bytebufferpool.Put(bp)
	for i, key := range slices.Sorted(maps.Keys(p)) {
		if i > 0 {
			_ = bp.WriteByte(' ')
		}
		_, _ = fmt.Fprintf(bp, "%s=%v", key, p[key])
	}
	return string(bp.B)
}

// interpolate renders template with params. The second result is false when template is empty or params lacks a
// value for one of its placeholders.
func interpolate(template string, params Params) (string, bool) {
	if template == "" {
		return "", false
	}
	bp := bytebufferpool.Get()
	defer bytebufferpool.Put(bp)
	rest := template
	for {
		start := strings.Index(rest, placeholderPrefix)
		if start < 0 {
			break
		}
		end := strings.Index(rest[start:], placeholderSuffix)
		if end < 0 {
			break
		}
		value, ok := params[rest[start+len(placeholderPrefix):start+end]]
		if !ok {
			return "", false
		}
		_, _ = bp.WriteString(rest[:start])
		_, _ = fmt.Fprint(bp, value)
		rest = rest[start+end+len(placeholderSuffix):]
	}
	_, _ = bp.WriteString(rest)
	return string(bp.B), true
}
