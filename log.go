package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// NewSlogHandler wraps a slog.Handler so that every record logged with a context
// carrying a span gets trace_id and span_id attributes, linking logs to traces.
func NewSlogHandler(h slog.Handler) slog.Handler {
	return slogHandler{Handler: h}
}

type slogHandler struct {
	slog.Handler
}

func (h slogHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return slogHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h slogHandler) WithGroup(name string) slog.Handler {
	return slogHandler{Handler: h.Handler.WithGroup(name)}
}
