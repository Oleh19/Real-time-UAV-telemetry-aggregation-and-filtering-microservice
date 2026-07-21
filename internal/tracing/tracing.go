package tracing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

const (
	shutdownTimeout    = 5 * time.Second
	defaultSampleRatio = 0.1
)

func sampleRatio() (float64, error) {
	raw, ok := os.LookupEnv("OTEL_TRACE_SAMPLE_RATIO")
	if !ok || raw == "" {
		return defaultSampleRatio, nil
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return 0, fmt.Errorf("parse OTEL_TRACE_SAMPLE_RATIO: must be a number within [0, 1], got %q", raw)
	}
	return ratio, nil
}

func Setup(ctx context.Context, serviceName, endpoint string) (func(), error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	if endpoint == "" {
		return func() {}, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpointURL(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	ratio, err := sampleRatio()
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		)),
	)
	otel.SetTracerProvider(provider)

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
	}, nil
}
