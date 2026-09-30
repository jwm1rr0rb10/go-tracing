# tracing

**A lightweight, clean, and production-ready OpenTelemetry tracing library for Go.**

This package provides a simple, opinionated wrapper around OpenTelemetry (OTel) to make distributed tracing effortless in Go services — with zero boilerplate for the most common use cases.

It includes:
- OTLP HTTP exporter setup with sensible defaults
- Automatic service metadata (name, version, environment, instance ID)
- HTTP middleware with route-based span names
- Modern gRPC client & server tracing using the recommended `StatsHandler` API
- Rich attribute helpers (`TraceAny`, `TraceValue`, `Error`) that work with structs, maps, JSON, and custom types

Perfect for microservices, APIs, and any Go application that wants clean, observable traces without fighting the OTel SDK.

---

## Features

- **Simple initialization** via functional options
- **Zero-config defaults** (localhost:4318, gzip, sensible resource attributes)
- **High-load knobs**: sampling ratio, batch processor tuning, TLS, auth headers, attribute size limit
- **Bounded-cardinality HTTP middleware** — span names from route patterns, no per-path state
- **Modern gRPC support** (server & client) using the current `StatsHandler` API
- **OTLP over HTTP or gRPC**, standard `OTEL_*` environment variables, auto-detected host/process/container resource
- **Full propagation**: incoming/outgoing HTTP and gRPC, W3C TraceContext + Baggage
- **Log correlation** via a `slog.Handler` that adds `trace_id` / `span_id`
- **Beautiful attribute injection** from structs with `trace` tags and custom prefixes
- **Graceful shutdown** helper
- **Tiny & idiomatic** — no magic, just clean Go

---

## Installation

```bash
go get github.com/jwm1rr0rb10/go-tracing
```

## Quick Start
### 1. Initialize tracing


```go
package main

import (
	"context"
	"log"

	"github.com/jwm1rr0rb10/go-tracing"
)

func main() {
	tp, err := tracing.New(
		tracing.WithServiceName("my-awesome-service"),
		tracing.WithServiceVersion("v1.2.3"),
		tracing.WithEnvName("production"),
		tracing.WithHost("otel-collector"),
		tracing.WithPort("4318"),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer tracing.Shutdown(context.Background(), tp)

	// Your app code here...
}
```

---

## 2. Start spans

```go
func handleRequest(ctx context.Context, req MyRequest) error {
    ctx, span := tracing.Start(ctx, "handleRequest")
    defer span.End()

    tracing.TraceAny(ctx, "request", req) // automatically adds all fields

    // ... your logic
    return nil
}
```

---

## Configuration
All configuration is done via functional options passed to `tracing.New():`

| Option                  | Description            | Default   |
|:------------------------|:-----------------------|:----------|
| WithHost(host)          | OTLP collector host    | localhost |
| WithPort(port)          | OTLP collector port    | 4318 (HTTP) / 4317 (gRPC) |
| WithServiceName(name)   | Service name           | (empty)   |
| WithServiceVersion(ver) | Service version        | (empty)   |
| WithServiceID(id)       | Unique instance ID     | (empty)   |
| WithEnvName(env)        | Deployment environment | (empty)   |
| WithSampleRatio(r)                  | Fraction of new traces to sample (0..1), respects parent decision | 1.0 (ParentBased) |
| WithSampler(s)                      | Custom sampler (overrides ratio) | — |
| WithTLS(cfg)                        | Enable TLS for the exporter | insecure |
| WithHeaders(map)                    | Headers for export requests (auth) | — |
| WithExportTimeout(d)                | Export request timeout | 10s |
| WithCompression(bool)               | Gzip compression of exports | true |
| WithBatchOptions(opts...)           | Batch processor tuning (queue size, batch size, timeout) | SDK defaults |
| WithAttributeValueLengthLimit(n)    | Max length of string attribute values (<0 = unlimited) | 4096 |
| WithProtocol(p)                     | `ProtocolHTTP` or `ProtocolGRPC` | HTTP (or `OTEL_EXPORTER_OTLP_PROTOCOL`) |
| WithResourceAttributes(kv...)       | Extra resource attributes (team, region, ...) | — |
| WithPropagator(p)                   | Replace propagator (e.g. add B3 for legacy services) | TraceContext + Baggage |
| WithExporter(e)                     | Custom exporter instead of OTLP (stdout, in-memory for tests) | OTLP |
| WithSpanProcessor(sp)               | Additional span processor | — |
| WithErrorHandler(fn)                | Receive internal OTel errors (failed exports, dropped spans) | std logger |

Example with full config:

```go
tp, err := tracing.New(
	tracing.WithHost("collector.prod.example.com"),
	tracing.WithServiceName("payments-api"),
	tracing.WithServiceVersion("2.4.1"),
	tracing.WithEnvName("production"),
	tracing.WithServiceID(os.Getenv("POD_NAME")),
)
```

### Environment variables

Standard OpenTelemetry variables are honored, explicit options win:

| Variable | Used when |
|:--|:--|
| `OTEL_EXPORTER_OTLP_(TRACES_)ENDPOINT` | neither `WithHost` nor `WithPort` is set |
| `OTEL_EXPORTER_OTLP_(TRACES_)PROTOCOL` | `WithProtocol` is not set |
| `OTEL_EXPORTER_OTLP_(TRACES_)HEADERS` | `WithHeaders` is not set |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | neither `WithSampler` nor `WithSampleRatio` is set |
| `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | the corresponding option is empty |
| `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT`, `OTEL_SPAN_*_COUNT_LIMIT` | `WithAttributeValueLengthLimit` is not set |

Resource also includes auto-detected `host.*`, `os.type`, `process.pid`, `process.runtime.*` and `container.id`.

### High-load recommendations

```go
tp, err := tracing.New(
	tracing.WithServiceName("payments-api"),
	tracing.WithSampleRatio(0.05), // sample 5% of new traces
	tracing.WithBatchOptions(
		sdktrace.WithMaxQueueSize(8192),
		sdktrace.WithMaxExportBatchSize(1024),
	),
)
```

- By default **100% of traces are sampled** — under high load set `WithSampleRatio`.
- When the batch queue is full, spans are dropped (not blocked) — size the queue for your peak RPS.

---

## HTTP Middleware

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/users", handler)

http.ListenAndServe(":8080", tracing.Middleware(mux))
```

- Span name = matched `http.ServeMux` route (e.g. `GET /users/{id}`), or just the HTTP method when no route matched — raw paths are never used, so span cardinality and memory stay bounded
- Automatic propagation of trace context (W3C TraceContext + Baggage)
- For other routers pass your own formatter: `tracing.NewMiddleware(otelhttp.WithSpanNameFormatter(f))`
- Panics are marked as span errors and re-raised
- Skip noisy endpoints: `tracing.NewMiddleware(tracing.WithSkipPaths("/healthz", "/metrics"))`
- For internet-facing edges add `otelhttp.WithPublicEndpoint()` so that untrusted incoming `traceparent` headers start a new trace (linked to the incoming one) instead of forcing your sampling decision

### HTTP client

```go
client := &http.Client{Transport: tracing.NewTransport(nil)} // nil = http.DefaultTransport
```
Outgoing requests get client spans and propagate the trace context.


---

## gRPC Interceptors
### Server

```go
server := grpc.NewServer(tracing.WithServerTracing())

// or using the convenience function:
server := grpc.NewServer(tracing.WithAllTracing()...)
```

### Client
```go
conn, err := grpc.NewClient(
target,
tracing.WithClientTracing(),
// other options...
)
```
```text
Note: This package uses the current recommended otelgrpc.NewServerHandler / otelgrpc.NewClientHandler API (the old Unary*Interceptor functions have been removed from the official contrib package).
```


---

## Attribute Helpers
### `TraceAny` – the star of the show
```go
type User struct {
    ID    int    `trace:"id"`
    Name  string `trace:"name"`
    Email string `trace:"-"` // skipped
    _     struct{} `trace:"user"` // custom prefix
}

func doSomething(ctx context.Context, u User) {
    tracing.TraceAny(ctx, "", u) // attributes: user.id, user.name
}
```

- Works with structs (exported fields only)
- Supports `trace:"-"` to skip fields
- Supports `trace:"custom_name"` for renaming
- Anonymous `_` field with `trace` tag sets a custom prefix
- Falls back to JSON for maps/slices/arrays/structs
- Implements `Attributed` interface for custom types

---

## Other helpers
```go
tracing.TraceValue(ctx, "key", value)           // single value
tracing.Error(ctx, err)                         // record error + set status
attrs := tracing.AttributesFrom("prefix", obj)  // get []attribute.KeyValue
```

---

## Log correlation

```go
slog.SetDefault(slog.New(tracing.NewSlogHandler(slog.NewJSONHandler(os.Stdout, nil))))

slog.InfoContext(ctx, "payment processed") // adds trace_id and span_id
```

`tracing.TraceIDFromContext(ctx)` / `tracing.SpanIDFromContext(ctx)` return the IDs for other loggers or response headers.

---

## Testing

```go
exp := tracetest.NewInMemoryExporter()
tp, _ := tracing.New(tracing.WithExporter(exp))
// ... code under test ...
_ = tracing.ForceFlush(ctx, tp)
spans := exp.GetSpans()
```

---

## Shutdown
```go
defer tracing.Shutdown(context.Background(), tp)
```
Gracefully shuts down the tracer provider and flushes remaining spans. Use a context with timeout so shutdown cannot hang.
`tracing.ForceFlush(ctx, tp)` exports buffered spans without shutting down (e.g. in serverless handlers).

---

## Advanced Usage
### Manual span creation

```go
ctx, span := tracing.Start(ctx, "expensive-operation", trace.WithAttributes(...))
defer span.End()
```

### Continue existing trace
```go
ctx, childSpan := tracing.Continue(ctx, "sub-operation")
```

---

## License

[MIT License](https://github.com/jwm1rr0rb10/go-tracing/blob/main/LICENSE) – © Raman Zaitsau [@jwm1rrr0rb10](https://github.com/jwm1rr0rb10)


---

## Contributing
Pull requests are welcome! Feel free to open issues for bugs or feature requests.

---

Made with ❤️ for clean, observable Go services.

