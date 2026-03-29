package tracing

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

var ErrNewExporter = errors.New("failed to create OTLP exporter")

// New initializes OpenTelemetry tracing with an OTLP HTTP exporter.
func New(params ...ConfigParam) (trace.TracerProvider, error) {
	cfg := &config{
		host: defaultHost,
		port: defaultPort,
	}
	for _, param := range params {
		param(cfg)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint(cfg.host+":"+cfg.port),
		otlptracehttp.WithInsecure(),
	)

	exporter, err := otlptrace.New(context.Background(), client)
	if err != nil {
		return nil, errors.Join(ErrNewExporter, err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceInstanceID(cfg.serviceID),
			semconv.ServiceName(cfg.serviceName),
			semconv.ServiceVersion(cfg.serviceVersion),
			semconv.DeploymentEnvironment(cfg.envName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to merge resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return provider, nil
}

// Shutdown gracefully shuts down the tracer provider (noop if not an SDK provider).
func Shutdown(ctx context.Context, tp trace.TracerProvider) error {
	if p, ok := tp.(*sdktrace.TracerProvider); ok {
		return p.Shutdown(ctx)
	}
	return nil
}

// Start creates a new span.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer("").Start(ctx, name, opts...)
}

// Continue creates a child span only if the parent span is recording.
func Continue(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx, span
	}
	return Start(ctx, name, opts...)
}
