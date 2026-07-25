package replay_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"uavmonitor/internal/replay"
	"uavmonitor/internal/telemetry"
)

type fakeSource struct {
	samples []telemetry.Sample
}

func (f *fakeSource) ListHistoryRange(_ context.Context, _, _ time.Time, droneID telemetry.DroneID, _ int) ([]telemetry.Sample, error) {
	if droneID == "" {
		return f.samples, nil
	}
	var filtered []telemetry.Sample
	for _, s := range f.samples {
		if s.DroneID == droneID {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

type capturingPublisher struct {
	mu      sync.Mutex
	samples []telemetry.Sample
	err     error
}

func (p *capturingPublisher) Publish(_ context.Context, sample telemetry.Sample) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.samples = append(p.samples, sample)
	return nil
}

func (p *capturingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.samples)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func recordedTrack(base time.Time, stepMillis, count int) []telemetry.Sample {
	samples := make([]telemetry.Sample, 0, count)
	for n := range count {
		samples = append(samples, telemetry.Sample{
			DroneID:    "target-001",
			Timestamp:  base.Add(time.Duration(n*stepMillis) * time.Millisecond),
			Latitude:   50 + float64(n)*0.01,
			Longitude:  30,
			Altitude:   100,
			Speed:      20,
			Confidence: 90,
		})
	}
	return samples
}

func waitState(manager *replay.Manager, id string, want replay.State) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range manager.List() {
			if status.ID == id && status.State == want {
				return true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestReplayPublishesRebasedSamplesInOrder(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 300, 3)}
	publisher := &capturingPublisher{}
	manager := replay.NewManager(source, publisher, discardLogger(), 4, 0)
	defer manager.Close()

	started := time.Now()
	status, err := manager.Start(context.Background(), replay.Request{
		From:  base,
		To:    base.Add(time.Minute),
		Speed: 3,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if status.Total != 3 || status.State != replay.StateRunning {
		t.Fatalf("status = %+v, want 3 samples running", status)
	}

	if !waitState(manager, status.ID, replay.StateCompleted) {
		t.Fatal("replay never completed")
	}
	elapsed := time.Since(started)
	if elapsed < 150*time.Millisecond {
		t.Errorf("replay finished in %s, want pacing of roughly 200ms (600ms of history at 3x)", elapsed)
	}

	if publisher.count() != 3 {
		t.Fatalf("published %d samples, want 3", publisher.count())
	}
	first := publisher.samples[0]
	if !strings.HasPrefix(string(first.DroneID), status.ID+"/") {
		t.Errorf("DroneID = %s, want prefix %s/", first.DroneID, status.ID)
	}
	if time.Since(first.Timestamp) > time.Minute {
		t.Errorf("timestamp was not rebased to now: %s", first.Timestamp)
	}
	for n := 1; n < len(publisher.samples); n++ {
		if publisher.samples[n].Latitude <= publisher.samples[n-1].Latitude {
			t.Errorf("samples out of order at %d", n)
		}
	}
	if manager.SamplesReplayed() != 3 {
		t.Errorf("SamplesReplayed = %d, want 3", manager.SamplesReplayed())
	}
}

func TestReplayCancelStopsPublishing(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 200, 100)}
	publisher := &capturingPublisher{}
	manager := replay.NewManager(source, publisher, discardLogger(), 4, 0)
	defer manager.Close()

	status, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 1})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := manager.Cancel(status.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !waitState(manager, status.ID, replay.StateCancelled) {
		t.Fatal("replay never reported cancelled")
	}
	if publisher.count() >= 100 {
		t.Error("cancel did not stop the replay early")
	}
	if err := manager.Cancel("replay-999"); !errors.Is(err, replay.ErrNotFound) {
		t.Errorf("Cancel unknown = %v, want ErrNotFound", err)
	}
}

func TestReplayPauseFreezesThenResumes(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 1000, 10)}
	publisher := &capturingPublisher{}
	manager := replay.NewManager(source, publisher, discardLogger(), 4, 0)
	defer manager.Close()

	status, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Hour), Speed: 10})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := manager.SetPaused(status.ID, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	c1 := publisher.count()
	time.Sleep(200 * time.Millisecond)
	if c2 := publisher.count(); c2 != c1 {
		t.Fatalf("paused replay kept publishing: %d -> %d", c1, c2)
	}
	if c1 == 10 {
		t.Fatal("replay completed before it could be paused; test is not exercising pause")
	}
	for _, s := range manager.List() {
		if s.ID == status.ID && !s.Paused {
			t.Fatal("status does not report the replay as paused")
		}
	}

	if _, err := manager.SetPaused(status.ID, false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !waitState(manager, status.ID, replay.StateCompleted) {
		t.Fatal("resumed replay never completed")
	}
	if publisher.count() != 10 {
		t.Fatalf("published %d samples after resume, want 10", publisher.count())
	}
}

func TestReplayControlValidation(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 1000, 10)}
	manager := replay.NewManager(source, &capturingPublisher{}, discardLogger(), 4, 0)
	defer manager.Close()

	status, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Hour), Speed: 10})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := manager.SetSpeed("replay-999", 5); !errors.Is(err, replay.ErrNotFound) {
		t.Errorf("SetSpeed unknown = %v, want ErrNotFound", err)
	}
	if _, err := manager.SetSpeed(status.ID, 5000); !errors.Is(err, replay.ErrInvalidSpeed) {
		t.Errorf("SetSpeed 5000 = %v, want ErrInvalidSpeed", err)
	}
	updated, err := manager.SetSpeed(status.ID, 25)
	if err != nil || updated.Speed != 25 {
		t.Fatalf("SetSpeed 25 = (%+v, %v), want speed 25", updated, err)
	}

	if err := manager.Cancel(status.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !waitState(manager, status.ID, replay.StateCancelled) {
		t.Fatal("replay never cancelled")
	}
	if _, err := manager.SetPaused(status.ID, true); !errors.Is(err, replay.ErrNotRunning) {
		t.Errorf("SetPaused on finished replay = %v, want ErrNotRunning", err)
	}
}

func TestReplayLimitsConcurrentRuns(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 500, 50)}
	manager := replay.NewManager(source, &capturingPublisher{}, discardLogger(), 1, 0)
	defer manager.Close()

	if _, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 1}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if _, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 1}); !errors.Is(err, replay.ErrTooManyReplays) {
		t.Fatalf("second Start = %v, want ErrTooManyReplays", err)
	}
}

func TestReplayValidatesRequests(t *testing.T) {
	base := time.Now()
	source := &fakeSource{}
	manager := replay.NewManager(source, &capturingPublisher{}, discardLogger(), 4, 0)
	defer manager.Close()

	if _, err := manager.Start(context.Background(), replay.Request{From: base, To: base}); !errors.Is(err, replay.ErrInvalidRange) {
		t.Errorf("equal range = %v, want ErrInvalidRange", err)
	}
	if _, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 5000}); !errors.Is(err, replay.ErrInvalidSpeed) {
		t.Errorf("speed 5000 = %v, want ErrInvalidSpeed", err)
	}
	if _, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute)}); !errors.Is(err, replay.ErrNoHistory) {
		t.Errorf("empty history = %v, want ErrNoHistory", err)
	}
}

func TestReplayPrunesFinishedRunsBeyondCap(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 1, 2)}
	manager := replay.NewManager(source, &capturingPublisher{}, discardLogger(), 4, 0)
	defer manager.Close()

	var lastID string
	for range 30 {
		status, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 1000})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		lastID = status.ID
		if !waitState(manager, status.ID, replay.StateCompleted) {
			t.Fatalf("replay %s never completed", status.ID)
		}
	}

	statuses := manager.List()
	if len(statuses) > 21 {
		t.Fatalf("List returned %d runs, want finished runs pruned to at most 20 plus running", len(statuses))
	}
	found := false
	for _, status := range statuses {
		if status.ID == lastID {
			found = true
		}
	}
	if !found {
		t.Fatal("most recent replay was pruned instead of the oldest")
	}
}

func TestReplayCloseCancelsEverything(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	source := &fakeSource{samples: recordedTrack(base, 500, 100)}
	manager := replay.NewManager(source, &capturingPublisher{}, discardLogger(), 4, 0)

	status, err := manager.Start(context.Background(), replay.Request{From: base, To: base.Add(time.Minute), Speed: 1})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	manager.Close()
	if !waitState(manager, status.ID, replay.StateCancelled) {
		t.Fatal("Close did not cancel the running replay")
	}
}
