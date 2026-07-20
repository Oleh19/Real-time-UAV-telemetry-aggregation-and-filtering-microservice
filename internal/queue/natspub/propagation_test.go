package natspub_test

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"uavmonitor/internal/queue/natspub"
)

func TestTraceContextRoundTripsThroughNATSHeaders(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("build trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("build span id: %v", err)
	}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	header := nats.Header{}
	natspub.InjectTraceContext(ctx, header)
	if header.Get("traceparent") == "" {
		t.Fatal("traceparent header was not injected")
	}

	extracted := trace.SpanContextFromContext(natspub.ExtractTraceContext(context.Background(), header))
	if extracted.TraceID() != traceID {
		t.Errorf("extracted trace id = %s, want %s", extracted.TraceID(), traceID)
	}
	if !extracted.IsRemote() {
		t.Error("extracted span context should be marked remote")
	}
}

func TestExtractTraceContextToleratesNilHeaders(t *testing.T) {
	ctx := context.Background()
	if got := natspub.ExtractTraceContext(ctx, nil); got != ctx {
		t.Error("nil headers should return the original context")
	}
}
