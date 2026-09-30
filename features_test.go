package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

// newWithMemoryExporter runs New with an in-memory exporter and restores globals afterwards.
func newWithMemoryExporter(t *testing.T, params ...ConfigParam) (*tracetest.InMemoryExporter, func() []tracetest.SpanStub) {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	exp := tracetest.NewInMemoryExporter()
	tp, err := New(append([]ConfigParam{WithExporter(exp)}, params...)...)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = Shutdown(context.Background(), tp)
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return exp, func() []tracetest.SpanStub {
		if err := ForceFlush(context.Background(), tp); err != nil {
			t.Fatalf("ForceFlush() error: %v", err)
		}
		return exp.GetSpans()
	}
}

func TestNewEndToEnd(t *testing.T) {
	_, flush := newWithMemoryExporter(t,
		WithServiceName("svc"),
		WithEnvName("prod"),
		WithResourceAttributes(attribute.String("team", "payments")),
	)

	_, span := Start(context.Background(), "op")
	span.End()

	spans := flush()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	res := spans[0].Resource
	for _, want := range []attribute.KeyValue{
		semconv.ServiceName("svc"),
		semconv.DeploymentEnvironmentNameKey.String("prod"),
		attribute.String("team", "payments"),
	} {
		if v, ok := res.Set().Value(want.Key); !ok || v != want.Value {
			t.Errorf("resource %s = %v, want %v", want.Key, v.String(), want.Value.String())
		}
	}
	if _, ok := res.Set().Value(semconv.HostNameKey); !ok {
		t.Errorf("host.name not detected")
	}
}

func TestNewGRPCProtocol(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tp, err := New(WithProtocol(ProtocolGRPC))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	_ = Shutdown(context.Background(), tp)

	if _, err := New(WithProtocol("thrift")); !errors.Is(err, ErrUnknownProtocol) {
		t.Fatalf("got %v, want ErrUnknownProtocol", err)
	}
}

func TestProtocolFromEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	cfg := defaultConfig()
	cfg.applyDefaults()
	if cfg.protocol != ProtocolGRPC || cfg.port != defaultGRPCPort {
		t.Fatalf("protocol=%s port=%s, want grpc/4317", cfg.protocol, cfg.port)
	}
}

func TestSamplingRatioZero(t *testing.T) {
	_, flush := newWithMemoryExporter(t, WithSampleRatio(0))
	_, span := Start(context.Background(), "op")
	span.End()
	if n := len(flush()); n != 0 {
		t.Fatalf("got %d spans, want 0", n)
	}
}

func TestHTTPClientServerPropagation(t *testing.T) {
	_, flush := newWithMemoryExporter(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(http.ResponseWriter, *http.Request) {})
	srv := httptest.NewServer(Middleware(mux))
	defer srv.Close()

	client := &http.Client{Transport: NewTransport(nil)}
	ctx, parent := Start(context.Background(), "caller")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/ping", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	parent.End()

	spans := flush()
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3 (caller, client, server)", len(spans))
	}
	traceID := parent.SpanContext().TraceID()
	for _, s := range spans {
		if s.SpanContext.TraceID() != traceID {
			t.Errorf("span %q is in another trace", s.Name)
		}
	}
}

func TestMiddlewareSkipPaths(t *testing.T) {
	sr := setupRecorder(t)
	h := NewMiddleware(WithSkipPaths("/healthz"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api", nil))

	if n := len(sr.Ended()); n != 1 {
		t.Fatalf("got %d spans, want 1", n)
	}
}

func TestMiddlewareRecordsPanic(t *testing.T) {
	sr := setupRecorder(t)
	h := Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()

	s := sr.Ended()[0]
	if s.Status().Code != codes.Error || s.Status().Description != "panic: boom" || len(s.Events()) != 1 {
		t.Fatalf("panic not recorded: status=%+v events=%d", s.Status(), len(s.Events()))
	}
}

func TestGRPCClientServerPropagation(t *testing.T) {
	_, flush := newWithMemoryExporter(t)

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(WithServerTracing())
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		WithClientTracing(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := healthpb.NewHealthClient(conn).Check(context.Background(), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}

	spans := flush()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 (client, server)", len(spans))
	}
	if spans[0].SpanContext.TraceID() != spans[1].SpanContext.TraceID() {
		t.Fatalf("client and server spans are in different traces")
	}
}

func TestSlogHandler(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	var buf bytes.Buffer
	logger := slog.New(NewSlogHandler(slog.NewJSONHandler(&buf, nil))).With("k", "v")

	logger.InfoContext(ctx, "hello")
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["trace_id"] != span.SpanContext().TraceID().String() || rec["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("trace ids missing: %v", rec)
	}
	if TraceIDFromContext(ctx) != rec["trace_id"] || SpanIDFromContext(ctx) != rec["span_id"] {
		t.Fatalf("ID helpers mismatch")
	}

	buf.Reset()
	logger.Info("no span")
	if bytes.Contains(buf.Bytes(), []byte("trace_id")) {
		t.Fatalf("trace_id added without a span: %s", buf.String())
	}
}
