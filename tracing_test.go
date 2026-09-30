package tracing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupRecorder(t testing.TB) *tracetest.SpanRecorder {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prev)
	})
	return sr
}

func TestNew(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tp, err := New(
		WithServiceName("svc"),
		WithServiceVersion("1.0.0"),
		WithEnvName("test"),
		WithServiceID("id-1"),
		WithSampleRatio(0.1),
		WithHeaders(map[string]string{"Authorization": "Bearer x"}),
		WithBatchOptions(sdktrace.WithMaxQueueSize(4096)),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := Shutdown(context.Background(), tp); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name   string
		params []ConfigParam
		want   error
	}{
		{"empty host", []ConfigParam{WithHost("")}, ErrHostIsEmpty},
		{"empty port", []ConfigParam{WithPort("")}, ErrPortIsEmpty},
		{"ratio above 1", []ConfigParam{WithSampleRatio(1.5)}, ErrInvalidSampleRatio},
		{"negative ratio", []ConfigParam{WithSampleRatio(-0.1)}, ErrInvalidSampleRatio},
		{"negative timeout", []ConfigParam{WithExportTimeout(-1)}, ErrInvalidExportTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.params...); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestContinue(t *testing.T) {
	sr := setupRecorder(t)

	// No parent: Continue must not create a span.
	ctx, span := Continue(context.Background(), "orphan")
	span.End()
	if ctx != context.Background() || len(sr.Ended()) != 0 {
		t.Fatalf("Continue without parent created a span")
	}

	ctx, parent := Start(context.Background(), "parent")
	_, child := Continue(ctx, "child")
	child.End()
	parent.End()
	if n := len(sr.Ended()); n != 2 {
		t.Fatalf("got %d spans, want 2", n)
	}
}

func TestError(t *testing.T) {
	sr := setupRecorder(t)

	ctx, span := Start(context.Background(), "op")
	Error(ctx, nil)
	Error(ctx, errors.New("boom"))
	span.End()

	s := sr.Ended()[0]
	if s.Status().Code != codes.Error || s.Status().Description != "boom" {
		t.Fatalf("unexpected status: %+v", s.Status())
	}
	if len(s.Events()) != 1 {
		t.Fatalf("got %d events, want 1", len(s.Events()))
	}
}

func TestMiddlewareSpanNames(t *testing.T) {
	sr := setupRecorder(t)

	mux := http.NewServeMux()
	noop := func(http.ResponseWriter, *http.Request) {}
	mux.HandleFunc("GET /users/{id}", noop)
	mux.HandleFunc("/orders/{id}", noop)
	h := Middleware(mux)

	for i := range 100 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, fmt.Sprintf("/users/%d", i), nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/orders/1", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/unknown/42", nil))

	names := map[string]int{}
	for _, s := range sr.Ended() {
		names[s.Name()]++
	}
	want := map[string]int{"GET /users/{id}": 100, "POST /orders/{id}": 1, "GET": 1}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("span names = %v, want %v", names, want)
	}
}

func TestMiddlewarePropagation(t *testing.T) {
	sr := setupRecorder(t)
	prev := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
	_, _ = New() // sets propagators; provider is replaced below
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))

	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	s := sr.Ended()[0]
	if got := s.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id not propagated: %s", got)
	}
}

type testUser struct {
	ID      int    `trace:"id"`
	Name    string `trace:"name"`
	Email   string `trace:"-"`
	Big     uint64
	Tags    []string
	private string
	_       struct{} `trace:"user"`
}

func TestAttributesFrom(t *testing.T) {
	u := &testUser{ID: 7, Name: "bob", Email: "secret", Big: math.MaxUint64, Tags: []string{"a"}, private: "x"}

	got := map[attribute.Key]attribute.Value{}
	for _, kv := range AttributesFrom("req", u) {
		got[kv.Key] = kv.Value
	}

	want := map[attribute.Key]string{
		"req_user.id":   "7",
		"req_user.name": "bob",
		"req_user.big":  "18446744073709551615",
		"req_user.tags": `["a"]`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k].String() != v {
			t.Errorf("%s = %q, want %q", k, got[k].String(), v)
		}
	}
}

func TestAttributesFromAnonymousStruct(t *testing.T) {
	attrs := AttributesFrom("", struct{ A int }{A: 1})
	if len(attrs) != 1 || attrs[0].Key != "struct.a" {
		t.Fatalf("unexpected attrs: %v", attrs)
	}
}

func TestTraceAnyNotRecording(_ *testing.T) {
	// Must be a no-op without a recording span.
	TraceAny(context.Background(), "", testUser{})
	TraceValue(context.Background(), "k", 1)
}

func BenchmarkAttributesFrom(b *testing.B) {
	u := testUser{ID: 7, Name: "bob", Big: 42}
	b.ReportAllocs()
	for b.Loop() {
		_ = AttributesFrom("", u)
	}
}

func BenchmarkMiddleware(b *testing.B) {
	setupRecorder(b)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(http.ResponseWriter, *http.Request) {})
	h := Middleware(mux)
	req := httptest.NewRequest(http.MethodGet, "/users/1", nil)
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkStartParallel(b *testing.B) {
	setupRecorder(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := Start(ctx, "op")
			span.End()
		}
	})
}
