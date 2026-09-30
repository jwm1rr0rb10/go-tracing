package tracing

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

// ErrNewExporter is returned by New when the OTLP exporter cannot be created.
var ErrNewExporter = errors.New("failed to create OTLP exporter")

const instrumentationName = "github.com/jwm1rr0rb10/go-tracing"

// cachedTracer is the tracer of the current global provider. The SDK takes a mutex
// in TracerProvider.Tracer, so it is resolved once per provider, not once per span.
type cachedTracer struct {
	provider trace.TracerProvider
	tracer   trace.Tracer
}

var currentTracer atomic.Pointer[cachedTracer]

func getTracer() trace.Tracer {
	provider := otel.GetTracerProvider()
	if c := currentTracer.Load(); c != nil && c.provider == provider {
		return c.tracer
	}
	t := provider.Tracer(instrumentationName)
	currentTracer.Store(&cachedTracer{provider: provider, tracer: t})
	return t
}

// New initializes OpenTelemetry tracing with an OTLP exporter (HTTP by default)
// and installs it as the global tracer provider and propagator.
func New(params ...ConfigParam) (trace.TracerProvider, error) {
	cfg := defaultConfig()
	for _, param := range params {
		param(cfg)
	}
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.errorHandler != nil {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(cfg.errorHandler))
	}

	ctx := context.Background()

	exporter := cfg.exporter
	if exporter == nil {
		var err error
		if exporter, err = newExporter(ctx, cfg); err != nil {
			return nil, errors.Join(ErrNewExporter, err)
		}
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	limits := sdktrace.NewSpanLimits() // honors OTEL_SPAN_* env vars
	if cfg.attributeValueLengthLimitSet || !attributeLimitFromEnv() {
		limits.AttributeValueLengthLimit = cfg.attributeValueLengthLimit
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter, cfg.batchOptions...),
		sdktrace.WithResource(res),
		sdktrace.WithRawSpanLimits(limits),
	}
	if sampler := cfg.buildSampler(); sampler != nil {
		opts = append(opts, sdktrace.WithSampler(sampler))
	}
	for _, sp := range cfg.spanProcessors {
		opts = append(opts, sdktrace.WithSpanProcessor(sp))
	}
	provider := sdktrace.NewTracerProvider(opts...)

	propagator := cfg.propagator
	if propagator == nil {
		propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagator)

	return provider, nil
}

func newExporter(ctx context.Context, cfg *config) (sdktrace.SpanExporter, error) {
	// When the endpoint comes from OTEL_EXPORTER_OTLP_*_ENDPOINT, the SDK reads it
	// (and derives TLS from its scheme) itself.
	useEnvEndpoint := !cfg.endpointSet && endpointFromEnv()
	endpoint := cfg.host + ":" + cfg.port

	if cfg.protocol == ProtocolGRPC {
		var opts []otlptracegrpc.Option
		if !useEnvEndpoint {
			opts = append(opts, otlptracegrpc.WithEndpoint(endpoint))
			if cfg.tlsConfig == nil {
				opts = append(opts, otlptracegrpc.WithInsecure())
			}
		}
		if cfg.tlsConfig != nil {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(cfg.tlsConfig)))
		}
		if len(cfg.headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.headers))
		}
		if cfg.exportTimeout > 0 {
			opts = append(opts, otlptracegrpc.WithTimeout(cfg.exportTimeout))
		}
		if cfg.compression {
			opts = append(opts, otlptracegrpc.WithCompressor("gzip"))
		}
		return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	}

	var opts []otlptracehttp.Option
	if !useEnvEndpoint {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		if cfg.tlsConfig == nil {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
	}
	if cfg.tlsConfig != nil {
		opts = append(opts, otlptracehttp.WithTLSClientConfig(cfg.tlsConfig))
	}
	if len(cfg.headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.headers))
	}
	if cfg.exportTimeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(cfg.exportTimeout))
	}
	if cfg.compression {
		opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
	}
	return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
}

// newResource builds the resource: SDK defaults (incl. OTEL_SERVICE_NAME and
// OTEL_RESOURCE_ATTRIBUTES), host/OS/process/container detectors, then explicit options.
func newResource(ctx context.Context, cfg *config) (*resource.Resource, error) {
	attrs := make([]attribute.KeyValue, 0, 4+len(cfg.resourceAttrs))
	// Empty values are skipped so they do not override environment variables.
	if cfg.serviceName != "" {
		attrs = append(attrs, semconv.ServiceName(cfg.serviceName))
	}
	if cfg.serviceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.serviceVersion))
	}
	if cfg.serviceID != "" {
		attrs = append(attrs, semconv.ServiceInstanceID(cfg.serviceID))
	}
	if cfg.envName != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(cfg.envName))
	}
	attrs = append(attrs, cfg.resourceAttrs...)

	detected, err := resource.New(ctx,
		resource.WithSchemaURL(semconv.SchemaURL),
		resource.WithHost(),
		resource.WithOSType(),
		resource.WithProcessPID(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithContainer(),
		resource.WithFromEnv(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		if !errors.Is(err, resource.ErrPartialResource) {
			return nil, fmt.Errorf("failed to detect resource: %w", err)
		}
		otel.Handle(err) // some detectors failed; keep what was detected
	}

	res, err := resource.Merge(resource.Default(), detected)
	if err != nil {
		return nil, fmt.Errorf("failed to merge resource: %w", err)
	}
	return res, nil
}

// Shutdown gracefully shuts down the tracer provider, flushing buffered spans
// (noop if not an SDK provider).
func Shutdown(ctx context.Context, tp trace.TracerProvider) error {
	if p, ok := tp.(*sdktrace.TracerProvider); ok {
		return p.Shutdown(ctx)
	}
	return nil
}

// ForceFlush exports all buffered spans without shutting the provider down
// (e.g. before a serverless function freezes). Noop if not an SDK provider.
func ForceFlush(ctx context.Context, tp trace.TracerProvider) error {
	if p, ok := tp.(*sdktrace.TracerProvider); ok {
		return p.ForceFlush(ctx)
	}
	return nil
}

// Start creates a new span.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return getTracer().Start(ctx, name, opts...)
}

// Continue creates a child span only if the parent span is recording.
func Continue(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return ctx, span
	}
	return Start(ctx, name, opts...)
}

// TraceIDFromContext returns the trace ID of the current span, or "" if there is none.
func TraceIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

// SpanIDFromContext returns the span ID of the current span, or "" if there is none.
func SpanIDFromContext(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasSpanID() {
		return sc.SpanID().String()
	}
	return ""
}
