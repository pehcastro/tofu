package state

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
)

func deriveVersion(point string, envelope any) string {
	sum := sha256.Sum256([]byte(shapeOf(reflect.TypeOf(envelope))))
	return point + "." + hex.EncodeToString(sum[:])[:8]
}

func shapeOf(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + shapeOf(t.Elem())
	case reflect.Slice:
		return "[]" + shapeOf(t.Elem())
	case reflect.Map:
		return "map[" + shapeOf(t.Key()) + "]" + shapeOf(t.Elem())
	case reflect.Interface:
		return "any"
	case reflect.Struct:
		var b strings.Builder
		b.WriteByte('{')
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name := field.Tag.Get("json")
			if name == "" {
				name = field.Name
			}
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(name)
			b.WriteByte(':')
			b.WriteString(shapeOf(field.Type))
		}
		b.WriteByte('}')
		return b.String()
	default:
		return t.Kind().String()
	}
}
