// Package tracing is a thin, production-oriented wrapper around OpenTelemetry tracing.
//
// It configures an OTLP (HTTP or gRPC) exporter with batching, sampling and resource
// detection, installs global W3C TraceContext + Baggage propagation, and provides
// HTTP/gRPC instrumentation, span attribute helpers and slog correlation.
//
// Standard OTEL_* environment variables (endpoint, protocol, headers, sampler,
// resource attributes, span limits) are honored unless overridden by options.
package tracing
