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
// Span name = "METHOD /path". Handler is cached for performance.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Path
		if uri == "" {
			uri = r.RequestURI
		}
		operation := r.Method + " " + uri

		if h, ok := instrumentedHandlers.Load(operation); ok {
			h.(http.Handler).ServeHTTP(w, r)
			return
		}

		h := otelhttp.NewHandler(
			next,
			operation,
			otelhttp.WithPropagators(otel.GetTextMapPropagator()),
		)

		instrumentedHandlers.Store(operation, h)
		h.ServeHTTP(w, r)
	})
}

// ====================== gRPC Tracing (new StatsHandler API) ======================

// WithServerTracing return option for gRPC-server with full OpenTelemetry tracing.
func WithServerTracing() grpc.ServerOption {
	return grpc.StatsHandler(
		otelgrpc.NewServerHandler(
			otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
		),
	)
}

// WithClientTracing return option for gRPC-клиента with full OpenTelemetry tracing.
func WithClientTracing() grpc.DialOption {
	return grpc.WithStatsHandler(
		otelgrpc.NewClientHandler(
			otelgrpc.WithPropagators(otel.GetTextMapPropagator()),
		),
	)
}

// WithAllTracing — useful func for server(using recommendation).
func WithAllTracing() []grpc.ServerOption {
	return []grpc.ServerOption{WithServerTracing()}
}
