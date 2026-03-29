package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"reflect"

	"github.com/iancoleman/strcase"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Attributed lets a type provide its own OpenTelemetry attributes.
type Attributed interface {
	Attributes() []attribute.KeyValue
}

// TraceValue records a single attribute on the current span.
func TraceValue(ctx context.Context, name string, val any) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if av, ok := attributeValue(reflect.ValueOf(val)); ok {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key(name), Value: av})
	}
}

// TraceAny adds attributes from a struct (or Attributed type) to the current span.
func TraceAny(ctx context.Context, prefix string, obj any) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() || obj == nil {
		return
	}

	if attributed, ok := obj.(Attributed); ok {
		span.SetAttributes(attributed.Attributes()...)
		return
	}

	span.SetAttributes(AttributesFrom(prefix, obj)...)
}

// Error records an error and sets the span status to Error.
func Error(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// AttributesFrom converts a struct's exported fields into OpenTelemetry attributes.
// Supports `trace:"-"` to skip a field and `trace:"customname"` to rename it.
// The `_` field with a `trace` tag can be used for a custom prefix.
func AttributesFrom(prefix string, obj any) []attribute.KeyValue {
	if obj == nil {
		return nil
	}
	if prefix != "" {
		prefix += "_"
	}
	return attributesFrom(prefix, obj)
}

// --- internal ---

const traceTag = "trace"

func attributesFrom(prefix string, obj any) []attribute.KeyValue {
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil // only structs are reflected into fields
	}

	rt := rv.Type()
	attrs := make([]attribute.KeyValue, 0, rt.NumField())

	prefixBytes := getNamePrefix(rt)

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		tag := field.Tag.Get(traceTag)
		if tag == "-" {
			continue
		}

		fieldName := field.Name
		if tag != "" {
			fieldName = tag
		}

		if av, ok := attributeValue(rv.Field(i)); ok {
			key := attribute.Key(prefix + string(prefixBytes) + strcase.ToSnake(fieldName))
			attrs = append(attrs, attribute.KeyValue{Key: key, Value: av})
		}
	}

	return attrs
}

func attributeValue(v reflect.Value) (attribute.Value, bool) {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return attribute.Value{}, false
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.String:
		return attribute.StringValue(v.String()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return attribute.Int64Value(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return attribute.Int64Value(int64(v.Uint())), true
	case reflect.Float32, reflect.Float64:
		return attribute.Float64Value(v.Float()), true
	case reflect.Bool:
		return attribute.BoolValue(v.Bool()), true
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		data, _ := json.Marshal(v.Interface())
		return attribute.StringValue(string(data)), true
	default:
		return attribute.Value{}, false
	}
}

func getNamePrefix(rt reflect.Type) []byte {
	var buf bytes.Buffer

	// Allow custom prefix via anonymous _ field with trace tag
	if sf, ok := rt.FieldByName("_"); ok {
		if tag := sf.Tag.Get(traceTag); tag != "" {
			buf.WriteString(tag)
			buf.WriteByte('.')
			return buf.Bytes()
		}
	}

	// Default: package.structname.
	pkg := path.Base(rt.PkgPath())
	if pkg == "." || pkg == "" {
		pkg = "struct"
	}
	buf.WriteString(pkg)
	buf.WriteByte('.')
	buf.WriteString(strcase.ToSnake(rt.Name()))
	buf.WriteByte('.')

	return buf.Bytes()
}
