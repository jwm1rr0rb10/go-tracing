package tracing

import (
	"net/http"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
)

// instrumentedHandlers caches the wrapped otelhttp handlers per unique "METHOD /path".
var instrumentedHandlers sync.Map

// Middleware adds OpenTelemetry tracing to HTTP handlers.
// It uses "METHOD /path" as the span name and caches the instrumented handler for performance.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Path
		if uri == "" {
			uri = r.RequestURI
		}
		operation := r.Method + " " + uri

		// Fast path: reuse cached handler
		if h, ok := instrumentedHandlers.Load(operation); ok {
			h.(http.Handler).ServeHTTP(w, r)
			return
		}

		// First time for this route → wrap and cache
		h := otelhttp.NewHandler(
			next,
			operation,
			otelhttp.WithPropagators(otel.GetTextMapPropagator()),
		)

		instrumentedHandlers.Store(operation, h)
		h.ServeHTTP(w, r)
	})
}

// === gRPC Server Interceptors ===

// UnaryServerInterceptor returns a gRPC unary server interceptor with OpenTelemetry tracing.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return otelgrpc.UnaryServerInterceptor(
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
	)
}

// StreamServerInterceptor returns a gRPC stream server interceptor with OpenTelemetry tracing.
func StreamServerInterceptor() grpc.StreamServerInterceptor {
	return otelgrpc.StreamServerInterceptor(
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
	)
}

// WithAllTracing is a convenience for servers that want both unary and stream tracing.
func WithAllTracing() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(UnaryServerInterceptor()),
		grpc.StreamInterceptor(StreamServerInterceptor()),
	}
}

// === gRPC Client Interceptors ===

// UnaryClientInterceptor returns a gRPC unary client interceptor with OpenTelemetry tracing.
func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return otelgrpc.UnaryClientInterceptor(
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
	)
}

// StreamClientInterceptor returns a gRPC stream client interceptor with OpenTelemetry tracing.
func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return otelgrpc.StreamClientInterceptor(
		otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
	)
}
