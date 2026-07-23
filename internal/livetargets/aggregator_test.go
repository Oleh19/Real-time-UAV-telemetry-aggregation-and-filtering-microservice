package livetargets_test

import (
	"testing"
	"time"

	"uavmonitor/internal/livetargets"
	"uavmonitor/internal/telemetry"
)

func sampleAt(id string, ts time.Time, lat float64) telemetry.Sample {
	return telemetry.Sample{DroneID: telemetry.DroneID(id), Timestamp: ts, Latitude: lat, Longitude: 30}
}

func TestStoreKeepsNewestPerTarget(t *testing.T) {
	store := livetargets.NewStore(time.Minute)
	base := time.Now()

	store.Observe(sampleAt("s0-001", base, 50.0))
	store.Observe(sampleAt("s1-001", base, 51.0))
	store.Observe(sampleAt("s0-001", base.Add(time.Second), 50.5))

	if store.Count() != 2 {
		t.Fatalf("Count = %d, want 2 distinct targets", store.Count())
	}
	for _, s := range store.Snapshot() {
		if s.DroneID == "s0-001" && s.Latitude != 50.5 {
			t.Errorf("s0-001 latitude = %f, want the newest 50.5", s.Latitude)
		}
	}
}

func TestStoreIgnoresOutOfOrder(t *testing.T) {
	store := livetargets.NewStore(time.Minute)
	base := time.Now()

	store.Observe(sampleAt("s0-001", base, 50.0))
	store.Observe(sampleAt("s0-001", base.Add(-time.Second), 49.0))

	got := store.Snapshot()
	if len(got) != 1 || got[0].Latitude != 50.0 {
		t.Fatalf("snapshot = %+v, want the newer 50.0 retained", got)
	}
}

func TestStoreAggregatesAcrossInstances(t *testing.T) {
	store := livetargets.NewStore(time.Minute)
	now := time.Now()
	for _, id := range []string{"s0-001", "s0-002", "s1-001", "s1-002", "s1-003"} {
		store.Observe(sampleAt(id, now, 50.0))
	}
	if store.Count() != 5 {
		t.Fatalf("Count = %d, want 5 targets fused across two instances", store.Count())
	}
}
