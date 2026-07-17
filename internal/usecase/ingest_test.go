package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

type recordingPublisher struct {
	mu      sync.Mutex
	samples []telemetry.Sample
	block   chan struct{}
}

func (p *recordingPublisher) Publish(_ context.Context, sample telemetry.Sample) error {
	if p.block != nil {
		<-p.block
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, sample)
	return nil
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.samples)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sampleFor(id string) telemetry.Sample {
	return telemetry.Sample{DroneID: telemetry.DroneID(id), Timestamp: time.Now()}
}

func sampleAt(id string, ts time.Time) telemetry.Sample {
	return telemetry.Sample{DroneID: telemetry.DroneID(id), Timestamp: ts}
}

func TestIngestorPublishesAndCaches(t *testing.T) {
	tests := []struct {
		name     string
		droneIDs []string
		wantLast map[string]bool
	}{
		{
			name:     "single drone",
			droneIDs: []string{"drone-001"},
			wantLast: map[string]bool{"drone-001": true, "drone-999": false},
		},
		{
			name:     "multiple drones",
			droneIDs: []string{"drone-001", "drone-002", "drone-001"},
			wantLast: map[string]bool{"drone-001": true, "drone-002": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &recordingPublisher{}
			ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, time.Minute)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ingestor.Start(ctx, 2)

			for _, id := range tt.droneIDs {
				if err := ingestor.Submit(ctx, sampleFor(id)); err != nil {
					t.Fatalf("Submit(%s): %v", id, err)
				}
			}
			ingestor.Shutdown()

			if got := publisher.count(); got != len(tt.droneIDs) {
				t.Errorf("published %d samples, want %d", got, len(tt.droneIDs))
			}
			for id, want := range tt.wantLast {
				if _, ok := ingestor.LastKnown(telemetry.DroneID(id)); ok != want {
					t.Errorf("LastKnown(%s) = %v, want %v", id, ok, want)
				}
			}
		})
	}
}

func TestIngestorBackpressure(t *testing.T) {
	publisher := &recordingPublisher{block: make(chan struct{})}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 1, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ingestor.Start(ctx, 1)

	var full int
	for i := 0; i < 10; i++ {
		if err := ingestor.Submit(ctx, sampleFor("drone-001")); err != nil {
			if !errors.Is(err, usecase.ErrQueueFull) {
				t.Fatalf("Submit: unexpected error %v", err)
			}
			full++
		}
	}
	if full == 0 {
		t.Error("expected at least one ErrQueueFull with blocked publisher")
	}

	stats := ingestor.Stats()
	if stats.Dropped != int64(full) {
		t.Errorf("Stats().Dropped = %d, want %d", stats.Dropped, full)
	}

	close(publisher.block)
	ingestor.Shutdown()
}

func TestIngestorSubmitAfterContextCancel(t *testing.T) {
	publisher := &recordingPublisher{}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ingestor.Submit(ctx, sampleFor("drone-001")); !errors.Is(err, context.Canceled) {
		t.Errorf("Submit after cancel = %v, want context.Canceled", err)
	}
}

func TestIngestorRejectsInvalidSamples(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name   string
		sample telemetry.Sample
	}{
		{name: "empty drone id", sample: telemetry.Sample{Timestamp: now}},
		{name: "latitude out of range", sample: telemetry.Sample{DroneID: "d", Timestamp: now, Latitude: 200}},
		{name: "longitude out of range", sample: telemetry.Sample{DroneID: "d", Timestamp: now, Longitude: -300}},
		{name: "latitude not a number", sample: telemetry.Sample{DroneID: "d", Timestamp: now, Latitude: math.NaN()}},
		{name: "negative speed", sample: telemetry.Sample{DroneID: "d", Timestamp: now, Speed: -5}},
		{name: "confidence above maximum", sample: telemetry.Sample{DroneID: "d", Timestamp: now, Confidence: 150}},
		{name: "timestamp in the future", sample: telemetry.Sample{DroneID: "d", Timestamp: now.Add(time.Hour)}},
		{name: "timestamp too old", sample: telemetry.Sample{DroneID: "d", Timestamp: now.Add(-48 * time.Hour)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publisher := &recordingPublisher{}
			ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, time.Minute)
			err := ingestor.Submit(context.Background(), tt.sample)
			if !errors.Is(err, usecase.ErrInvalidSample) {
				t.Fatalf("Submit() = %v, want ErrInvalidSample", err)
			}
			if got := ingestor.Stats().Rejected; got != 1 {
				t.Errorf("Stats().Rejected = %d, want 1", got)
			}
			if _, ok := ingestor.LastKnown(tt.sample.DroneID); ok {
				t.Error("rejected sample must not be cached as last known state")
			}
		})
	}
}

func TestIngestorKeepsNewestState(t *testing.T) {
	publisher := &recordingPublisher{}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, time.Minute)
	ctx := context.Background()
	newest := time.Now()

	if err := ingestor.Submit(ctx, sampleAt("drone-001", newest)); err != nil {
		t.Fatalf("Submit(newest): %v", err)
	}
	if err := ingestor.Submit(ctx, sampleAt("drone-001", newest.Add(-time.Minute))); err != nil {
		t.Fatalf("Submit(stale): %v", err)
	}

	got, ok := ingestor.LastKnown("drone-001")
	if !ok {
		t.Fatal("LastKnown = false, want true")
	}
	if !got.Timestamp.Equal(newest) {
		t.Errorf("LastKnown timestamp = %s, want newest %s", got.Timestamp, newest)
	}
}

func TestIngestorSubmitAfterShutdown(t *testing.T) {
	publisher := &recordingPublisher{}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ingestor.Start(ctx, 1)
	ingestor.Shutdown()

	if err := ingestor.Submit(ctx, sampleFor("drone-001")); !errors.Is(err, usecase.ErrShutdown) {
		t.Errorf("Submit after Shutdown = %v, want ErrShutdown", err)
	}
}

func TestIngestorConcurrentSubmitAndShutdown(t *testing.T) {
	publisher := &recordingPublisher{}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 4, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ingestor.Start(ctx, 2)

	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				err := ingestor.Submit(ctx, sampleFor("drone-001"))
				if errors.Is(err, usecase.ErrShutdown) {
					return
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)
	ingestor.Shutdown()
	wg.Wait()
}

func TestIngestorEvictsStaleState(t *testing.T) {
	publisher := &recordingPublisher{}
	ingestor := usecase.NewIngestor(publisher, discardLogger(), 16, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ingestor.Start(ctx, 1)
	defer ingestor.Shutdown()

	if err := ingestor.Submit(ctx, sampleFor("drone-001")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, ok := ingestor.LastKnown("drone-001"); !ok {
		t.Fatal("LastKnown right after Submit = false, want true")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ingestor.LastKnown("drone-001"); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("stale drone state was not evicted within 2s")
}
