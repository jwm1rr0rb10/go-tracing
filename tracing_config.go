package tracing

import (
	"crypto/tls"
	"os"
	"time"

	"github.com/jwm1rr0rb10/go-errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Configuration errors returned by New.
var (
	ErrHostIsEmpty          = errors.New("host cannot be empty")
	ErrPortIsEmpty          = errors.New("port cannot be empty")
	ErrInvalidSampleRatio   = errors.New("sample ratio must be in range [0, 1]")
	ErrInvalidExportTimeout = errors.New("export timeout must be positive")
	ErrUnknownProtocol      = errors.New("unknown OTLP protocol")
)

// Protocol is the OTLP transport used to export spans.
type Protocol string

// Supported OTLP protocols.
const (
	ProtocolHTTP Protocol = "http/protobuf"
	ProtocolGRPC Protocol = "grpc"
)

const (
	defaultHost     = "localhost"
	defaultHTTPPort = "4318"
	defaultGRPCPort = "4317"

	// defaultAttributeValueLengthLimit caps string attribute values so that
	// large structs serialized to JSON do not bloat spans.
	defaultAttributeValueLengthLimit = 4096
)

// config holds tracing configuration parameters.
type config struct {
	host           string
	port           string
	protocol       Protocol
	endpointSet    bool // WithHost or WithPort was called
	portSet        bool
	serviceID      string
	serviceName    string
	serviceVersion string
	envName        string

	sampler     sdktrace.Sampler
	sampleRatio *float64

	tlsConfig     *tls.Config
	headers       map[string]string
	exportTimeout time.Duration
	compression   bool

	batchOptions []sdktrace.BatchSpanProcessorOption

	attributeValueLengthLimit    int
	attributeValueLengthLimitSet bool

	resourceAttrs  []attribute.KeyValue
	propagator     propagation.TextMapPropagator
	exporter       sdktrace.SpanExporter
	spanProcessors []sdktrace.SpanProcessor
	errorHandler   func(error)
}

func defaultConfig() *config {
	c := &config{
		host:                      defaultHost,
		protocol:                  ProtocolHTTP,
		compression:               true,
		attributeValueLengthLimit: defaultAttributeValueLengthLimit,
	}
	if p := firstEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"); p != "" {
		c.protocol = Protocol(p)
	}
	return c
}

// endpointFromEnv reports whether the OTLP endpoint is configured via environment.
func endpointFromEnv() bool {
	return firstEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

// attributeLimitFromEnv reports whether the attribute value length limit is configured via environment.
func attributeLimitFromEnv() bool {
	return firstEnv("OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", "OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT") != ""
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// applyDefaults fills values that depend on other options.
func (c *config) applyDefaults() {
	if c.portSet {
		return
	}
	c.port = defaultHTTPPort
	if c.protocol == ProtocolGRPC {
		c.port = defaultGRPCPort
	}
}

// Validate checks required fields.
func (c *config) Validate() error {
	if c.host == "" {
		return ErrHostIsEmpty
	}
	if c.port == "" {
		return ErrPortIsEmpty
	}
	if c.protocol != ProtocolHTTP && c.protocol != ProtocolGRPC {
		return ErrUnknownProtocol
	}
	if c.sampleRatio != nil && (*c.sampleRatio < 0 || *c.sampleRatio > 1) {
		return ErrInvalidSampleRatio
	}
	if c.exportTimeout < 0 {
		return ErrInvalidExportTimeout
	}
	return nil
}

// buildSampler returns the configured sampler. An explicit sampler wins over a ratio.
// nil means "not configured": the SDK then honors OTEL_TRACES_SAMPLER and
// defaults to ParentBased(AlwaysSample).
func (c *config) buildSampler() sdktrace.Sampler {
	if c.sampler != nil {
		return c.sampler
	}
	if c.sampleRatio != nil {
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(*c.sampleRatio))
	}
	return nil
}

// ConfigParam is a functional option for configuring tracing.
type ConfigParam func(*config)

// WithHost sets the OTLP collector host. When neither WithHost nor WithPort is set,
// OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_EXPORTER_OTLP_TRACES_ENDPOINT are honored.
func WithHost(host string) ConfigParam {
	return func(c *config) {
		c.host = host
		c.endpointSet = true
	}
}

// WithPort sets the OTLP collector port (default 4318 for HTTP, 4317 for gRPC).
func WithPort(port string) ConfigParam {
	return func(c *config) {
		c.port = port
		c.portSet = true
		c.endpointSet = true
	}
}

// WithProtocol selects the OTLP transport: ProtocolHTTP (default) or ProtocolGRPC.
func WithProtocol(protocol Protocol) ConfigParam {
	return func(c *config) { c.protocol = protocol }
}

// WithServiceID sets service.instance.id (e.g. pod name).
func WithServiceID(id string) ConfigParam {
	return func(c *config) { c.serviceID = id }
}

// WithServiceName sets service.name.
func WithServiceName(name string) ConfigParam {
	return func(c *config) { c.serviceName = name }
}

// WithServiceVersion sets service.version.
func WithServiceVersion(version string) ConfigParam {
	return func(c *config) { c.serviceVersion = version }
}

// WithEnvName sets deployment.environment.name.
func WithEnvName(env string) ConfigParam {
	return func(c *config) { c.envName = env }
}

// WithSampleRatio samples the given fraction of new traces (0..1) and
// respects the parent's sampling decision for incoming traces.
func WithSampleRatio(ratio float64) ConfigParam {
	return func(c *config) { c.sampleRatio = &ratio }
}

// WithSampler sets a custom sampler. Overrides WithSampleRatio.
func WithSampler(sampler sdktrace.Sampler) ConfigParam {
	return func(c *config) { c.sampler = sampler }
}

// WithTLS enables TLS for the OTLP exporter. Without it the connection is insecure.
func WithTLS(tlsConfig *tls.Config) ConfigParam {
	return func(c *config) { c.tlsConfig = tlsConfig }
}

// WithHeaders sets headers sent with every export request (e.g. auth tokens).
func WithHeaders(headers map[string]string) ConfigParam {
	return func(c *config) { c.headers = headers }
}

// WithExportTimeout sets the timeout of a single export request.
func WithExportTimeout(timeout time.Duration) ConfigParam {
	return func(c *config) { c.exportTimeout = timeout }
}

// WithCompression toggles gzip compression of export requests (enabled by default).
func WithCompression(enabled bool) ConfigParam {
	return func(c *config) { c.compression = enabled }
}

// WithBatchOptions tunes the batch span processor
// (sdktrace.WithMaxQueueSize, sdktrace.WithMaxExportBatchSize, sdktrace.WithBatchTimeout, ...).
func WithBatchOptions(opts ...sdktrace.BatchSpanProcessorOption) ConfigParam {
	return func(c *config) { c.batchOptions = append(c.batchOptions, opts...) }
}

// WithAttributeValueLengthLimit caps the length of string attribute values.
// Default is 4096 unless OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT is set; a negative value means unlimited.
func WithAttributeValueLengthLimit(limit int) ConfigParam {
	return func(c *config) {
		c.attributeValueLengthLimit = limit
		c.attributeValueLengthLimitSet = true
	}
}

// WithResourceAttributes adds extra resource attributes (team, region, cluster, ...).
// OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME are honored as well.
func WithResourceAttributes(attrs ...attribute.KeyValue) ConfigParam {
	return func(c *config) { c.resourceAttrs = append(c.resourceAttrs, attrs...) }
}

// WithPropagator replaces the default W3C TraceContext + Baggage propagator
// (e.g. to add B3 or Jaeger for legacy services).
func WithPropagator(propagator propagation.TextMapPropagator) ConfigParam {
	return func(c *config) { c.propagator = propagator }
}

// WithExporter replaces the OTLP exporter (e.g. stdouttrace or tracetest.InMemoryExporter).
// Endpoint, TLS, headers, timeout and compression options are ignored in this case.
func WithExporter(exporter sdktrace.SpanExporter) ConfigParam {
	return func(c *config) { c.exporter = exporter }
}

// WithSpanProcessor registers an additional span processor (e.g. for span enrichment).
func WithSpanProcessor(processor sdktrace.SpanProcessor) ConfigParam {
	return func(c *config) { c.spanProcessors = append(c.spanProcessors, processor) }
}

// WithErrorHandler receives internal OpenTelemetry errors (failed exports, dropped spans).
// By default they are written to the standard logger.
func WithErrorHandler(handler func(error)) ConfigParam {
	return func(c *config) { c.errorHandler = handler }
}
