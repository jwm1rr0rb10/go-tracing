package tracing

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

// Middleware adds OpenTelemetry tracing to HTTP handlers.
// Span name is the matched route ("GET /users/{id}") when the request is routed
// by http.ServeMux, otherwise the HTTP method. Raw paths are never used, which keeps
// span name cardinality bounded. Panics are recorded on the span and re-raised.
func Middleware(next http.Handler) http.Handler {
	return NewMiddleware()(next)
}

// NewMiddleware returns an HTTP tracing middleware with extra otelhttp options
// (e.g. otelhttp.WithSpanNameFormatter for routers that do not set http.Request.Pattern).
func NewMiddleware(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	opts = append([]otelhttp.Option{otelhttp.WithSpanNameFormatter(spanName)}, opts...)
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(recordPanics(next), "", opts...)
	}
}

// WithSkipPaths excludes requests with the given exact paths (health checks,
// metrics endpoints) from tracing.
func WithSkipPaths(paths ...string) otelhttp.Option {
	skip := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		skip[p] = struct{}{}
	}
	return otelhttp.WithFilter(func(r *http.Request) bool {
		_, ok := skip[r.URL.Path]
		return !ok
	})
}

// NewTransport wraps an outgoing HTTP transport with tracing and context propagation.
// A nil base means http.DefaultTransport.
func NewTransport(base http.RoundTripper, opts ...otelhttp.Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base, opts...)
}

// recordPanics marks the request span as failed when the handler panics.
// The SDK already records the panic as an exception event when the span ends,
// but leaves the status unset. It runs inside the otelhttp handler, so the span is still open.
func recordPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(error); !ok || !errors.Is(err, http.ErrAbortHandler) {
					trace.SpanFromContext(r.Context()).SetStatus(codes.Error, fmt.Sprintf("panic: %v", v))
				}
				panic(v)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func spanName(_ string, r *http.Request) string {
	if r.Pattern == "" {
		return r.Method
	}
	// Patterns registered without a method ("/users/{id}") get it prepended.
	if strings.HasPrefix(r.Pattern, "/") {
		return r.Method + " " + r.Pattern
	}
	return r.Pattern
}

// ====================== gRPC Tracing (new StatsHandler API) ======================

// WithServerTracing return option for gRPC-server with full OpenTelemetry tracing.
func WithServerTracing() grpc.ServerOption {
	return grpc.StatsHandler(otelgrpc.NewServerHandler())
}

// WithClientTracing return option for gRPC-client with full OpenTelemetry tracing.
func WithClientTracing() grpc.DialOption {
	return grpc.WithStatsHandler(otelgrpc.NewClientHandler())
}

// WithAllTracing — useful func for server(using recommendation).
func WithAllTracing() []grpc.ServerOption {
	return []grpc.ServerOption{WithServerTracing()}
}
