package config

import (
	"reflect"
	"strings"
	"time"

	"github.com/knadh/koanf/maps"
)

// structsProvider 把默认值结构体喂进 koanf。
//
// 不用 koanf 的 structs provider（它依赖 fatih/structs，多一个依赖且不认 time.Duration），
// 这里用反射直接展平成 map。只在启动期跑一次，性能无关紧要。
type structsProviderImpl struct{ v any }

func structsProvider(v any) *structsProviderImpl { return &structsProviderImpl{v: v} }

func (s *structsProviderImpl) ReadBytes() ([]byte, error) { return nil, nil }

func (s *structsProviderImpl) Read() (map[string]any, error) {
	out := map[string]any{}
	flatten(reflect.ValueOf(s.v), "", out)
	return maps.Unflatten(out, "."), nil
}

func flatten(v reflect.Value, prefix string, out map[string]any) {
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("koanf")
		if tag == "" || tag == "-" {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		fv := v.Field(i)
		// time.Duration 是 int64 的别名，必须在 struct 分支之前拦下来，
		// 否则会被当成普通整数丢掉单位语义。
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			out[key] = fv.Interface().(time.Duration).String()
			continue
		}
		if fv.Kind() == reflect.Struct {
			flatten(fv, key, out)
			continue
		}
		out[key] = fv.Interface()
	}
}

// NormalizeKey 供测试与调试使用：把环境变量名翻译成配置键。
func NormalizeKey(envName string) string {
	k := strings.ToLower(strings.TrimPrefix(envName, "UMAAS_"))
	return strings.ReplaceAll(k, "__", ".")
}
