# tracing

**A lightweight, clean, and production-ready OpenTelemetry tracing library for Go.**

This package provides a simple, opinionated wrapper around OpenTelemetry (OTel) to make distributed tracing effortless in Go services — with zero boilerplate for the most common use cases.

It includes:
- OTLP HTTP exporter setup with sensible defaults
- Automatic service metadata (name, version, environment, instance ID)
- High-performance HTTP middleware with handler caching
- Modern gRPC client & server tracing using the recommended `StatsHandler` API
- Rich attribute helpers (`TraceAny`, `TraceValue`, `Error`) that work with structs, maps, JSON, and custom types

Perfect for microservices, APIs, and any Go application that wants clean, observable traces without fighting the OTel SDK.

---

## Features

- **Simple initialization** via functional options
- **Zero-config defaults** (localhost:4318, sensible resource attributes)
- **Cached HTTP middleware** — no per-request overhead after first call
- **Modern gRPC support** (server & client) using the current `StatsHandler` API
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
| WithPort(port)          | OTLP collector port    | 4318      |
| WithServiceName(name)   | Service name           | (empty)   |
| WithServiceVersion(ver) | Service version        | (empty)   |
| WithServiceID(id)       | Unique instance ID     | (empty)   |
| WithEnvName(env)        | Deployment environment | (empty)   |

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

---

## HTTP Middleware

```go
mux := http.NewServeMux()
mux.HandleFunc("/api/users", handler)

http.ListenAndServe(":8080", tracing.Middleware(mux))
```

- Span name = `"METHOD /path"` (e.g. POST `/api/users`)
- Automatic propagation of trace context
- Cached — the `otelhttp` wrapper is created only once per route (high performance)

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

## Shutdown
```go
defer tracing.Shutdown(context.Background(), tp)
```
Gracefully shuts down the tracer provider and flushes remaining spans.

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


---

## Contributing
Pull requests are welcome! Feel free to open issues for bugs or feature requests.

---

Made with ❤️ for clean, observable Go services.

