package tracing

import (
	"context"
	"encoding/json"
	"math"
	"path"
	"reflect"
	"strconv"
	"sync"

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

// structField is the precomputed metadata of a traced struct field.
type structField struct {
	index int
	key   string // type prefix + snake_case field name
}

// structFields caches []structField per reflect.Type. The set of types is finite,
// so the cache is bounded.
var structFields sync.Map

func fieldsOf(rt reflect.Type) []structField {
	if cached, ok := structFields.Load(rt); ok {
		return cached.([]structField)
	}

	typePrefix := getNamePrefix(rt)
	fields := make([]structField, 0, rt.NumField())
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
		fields = append(fields, structField{index: i, key: typePrefix + strcase.ToSnake(fieldName)})
	}

	cached, _ := structFields.LoadOrStore(rt, fields)
	return cached.([]structField)
}

func attributesFrom(prefix string, obj any) []attribute.KeyValue {
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return nil // only structs are reflected into fields
	}

	fields := fieldsOf(rv.Type())
	attrs := make([]attribute.KeyValue, 0, len(fields))
	for _, f := range fields {
		if av, ok := attributeValue(rv.Field(f.index)); ok {
			attrs = append(attrs, attribute.KeyValue{Key: attribute.Key(prefix + f.key), Value: av})
		}
	}

	return attrs
}

func attributeValue(v reflect.Value) (attribute.Value, bool) {
	if v.Kind() == reflect.Pointer {
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
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := v.Uint()
		if u > math.MaxInt64 {
			// Does not fit into int64 — keep the exact value as a string.
			return attribute.StringValue(strconv.FormatUint(u, 10)), true
		}
		return attribute.Int64Value(int64(u)), true
	case reflect.Float32, reflect.Float64:
		return attribute.Float64Value(v.Float()), true
	case reflect.Bool:
		return attribute.BoolValue(v.Bool()), true
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		if !v.CanInterface() {
			return attribute.Value{}, false
		}
		data, err := json.Marshal(v.Interface())
		if err != nil {
			return attribute.Value{}, false
		}
		return attribute.StringValue(string(data)), true
	default:
		return attribute.Value{}, false
	}
}

func getNamePrefix(rt reflect.Type) string {
	// Allow custom prefix via anonymous _ field with trace tag
	if sf, ok := rt.FieldByName("_"); ok {
		if tag := sf.Tag.Get(traceTag); tag != "" {
			return tag + "."
		}
	}

	// Default: package.structname.
	pkg := path.Base(rt.PkgPath())
	if pkg == "." || pkg == "" {
		pkg = "struct"
	}
	if rt.Name() == "" {
		return pkg + "."
	}
	return pkg + "." + strcase.ToSnake(rt.Name()) + "."
}
