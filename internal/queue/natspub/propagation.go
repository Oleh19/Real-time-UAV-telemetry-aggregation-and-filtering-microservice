package natspub

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type HeaderCarrier nats.Header

func (c HeaderCarrier) Get(key string) string {
	return nats.Header(c).Get(key)
}

func (c HeaderCarrier) Set(key, value string) {
	nats.Header(c).Set(key, value)
}

func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	return keys
}

func InjectTraceContext(ctx context.Context, header nats.Header) {
	otel.GetTextMapPropagator().Inject(ctx, HeaderCarrier(header))
}

func ExtractTraceContext(ctx context.Context, header nats.Header) context.Context {
	if header == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, HeaderCarrier(header))
}

var _ propagation.TextMapCarrier = HeaderCarrier{}
