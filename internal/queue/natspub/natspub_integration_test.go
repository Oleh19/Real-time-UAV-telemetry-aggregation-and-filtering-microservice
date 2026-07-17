//go:build integration

package natspub_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/telemetry"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func requireNATS(t *testing.T) string {
	t.Helper()
	url := os.Getenv("NATS_URL")
	if url == "" {
		t.Skip("NATS_URL not set")
	}
	return url
}

func TestAsyncPublishAndConsumeRoundtrip(t *testing.T) {
	url := requireNATS(t)
	ctx := context.Background()

	conn, err := natspub.Connect(url, discardLogger())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	publisher, err := natspub.NewAsyncPublisher(ctx, conn, discardLogger())
	if err != nil {
		t.Fatalf("NewAsyncPublisher: %v", err)
	}

	droneID := telemetry.DroneID("itest-nats-001")
	sample := telemetry.Sample{
		DroneID:    droneID,
		Timestamp:  time.Now().UTC().Truncate(time.Millisecond),
		Latitude:   50.45,
		Longitude:  30.52,
		Altitude:   200,
		Speed:      25,
		Confidence: 77,
	}
	if err := publisher.Publish(ctx, sample); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := publisher.Flush(flushCtx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	js, err := natspub.NewJetStream(ctx, conn)
	if err != nil {
		t.Fatalf("NewJetStream: %v", err)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, natspub.StreamName, jetstream.ConsumerConfig{
		Durable:       "itest-telemetry-reader",
		FilterSubject: natspub.SubjectTelemetry,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConsumer: %v", err)
	}

	got := fetchDroneID(t, consumer)
	if got != string(droneID) {
		t.Errorf("consumed drone id = %q, want %q", got, droneID)
	}
}

func fetchDroneID(t *testing.T, consumer jetstream.Consumer) string {
	t.Helper()
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for msg := range batch.Messages() {
		if err := msg.Ack(); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		var pb telemetryv1.DroneTelemetry
		if err := proto.Unmarshal(msg.Data(), &pb); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		return pb.GetDroneId()
	}
	if err := batch.Error(); err != nil {
		t.Fatalf("Fetch batch error: %v", err)
	}
	t.Fatal("no telemetry message received within timeout")
	return ""
}
