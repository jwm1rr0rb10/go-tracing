package tracing_test

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"

	"github.com/jwm1rr0rb10/go-tracing"
)

func Example() {
	tp, err := tracing.New(
		tracing.WithServiceName("payments-api"),
		tracing.WithServiceVersion("2.4.1"),
		tracing.WithEnvName("production"),
		tracing.WithSampleRatio(0.05),
		tracing.WithBatchOptions(sdktrace.WithMaxQueueSize(8192)),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracing.Shutdown(ctx, tp)
	}()

	// Logs get trace_id / span_id.
	slog.SetDefault(slog.New(tracing.NewSlogHandler(slog.NewJSONHandler(os.Stdout, nil))))

	// Incoming HTTP.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(_ http.ResponseWriter, r *http.Request) {
		ctx, span := tracing.Start(r.Context(), "load-user")
		defer span.End()
		slog.InfoContext(ctx, "loading user", "id", r.PathValue("id"))
	})
	handler := tracing.NewMiddleware(tracing.WithSkipPaths("/healthz", "/metrics"))(mux)

	// Outgoing HTTP and gRPC.
	_ = &http.Client{Transport: tracing.NewTransport(nil)}
	_ = grpc.NewServer(tracing.WithServerTracing())

	_ = http.ListenAndServe(":8080", handler)
}
