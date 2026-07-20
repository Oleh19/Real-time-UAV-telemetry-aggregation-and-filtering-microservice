package geofence_test

import (
	"testing"
	"time"

	"uavmonitor/internal/geofence"
	"uavmonitor/internal/telemetry"
)

func positioned(id string, lat, lon float64) telemetry.Sample {
	return telemetry.Sample{
		DroneID:   telemetry.DroneID(id),
		Timestamp: time.Now(),
		Latitude:  lat,
		Longitude: lon,
	}
}

func newDetector(cfg geofence.SwarmConfig) *geofence.SwarmDetector {
	return geofence.NewSwarmDetector(cfg, discardLogger())
}

func TestSwarmDetectorFindsCompactGroup(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 2000, MinSize: 3})

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 50.010, 30.005))
	detector.Observe(positioned("target-003", 50.005, 30.010))
	detector.Observe(positioned("target-099", 49.000, 29.000))
	detector.Evaluate(time.Now())

	swarms := detector.Snapshot()
	if len(swarms) != 1 {
		t.Fatalf("Snapshot returned %d swarms, want 1", len(swarms))
	}
	swarm := swarms[0]
	if len(swarm.DroneIDs) != 3 {
		t.Fatalf("swarm has %d members, want 3 (loner excluded)", len(swarm.DroneIDs))
	}
	if swarm.Latitude < 50.0 || swarm.Latitude > 50.01 {
		t.Errorf("swarm center latitude = %f, want inside the group", swarm.Latitude)
	}
	if detector.DetectedTotal() != 1 {
		t.Errorf("DetectedTotal = %d, want 1", detector.DetectedTotal())
	}
}

func TestSwarmDetectorChainsClustersThroughIntermediates(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 1500, MinSize: 3})

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 50.012, 30.000))
	detector.Observe(positioned("target-003", 50.024, 30.000))
	detector.Evaluate(time.Now())

	swarms := detector.Snapshot()
	if len(swarms) != 1 || len(swarms[0].DroneIDs) != 3 {
		t.Fatalf("chained group = %v, want one swarm of 3", swarms)
	}
}

func TestSwarmDetectorIgnoresGroupsBelowMinSize(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 2000, MinSize: 3})

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 50.001, 30.001))
	detector.Evaluate(time.Now())

	if got := detector.ActiveSwarms(); got != 0 {
		t.Fatalf("ActiveSwarms = %d, want 0 for a pair", got)
	}
}

func TestSwarmDetectorKeepsStableIDWhileGroupMoves(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 2000, MinSize: 3})
	now := time.Now()

	for _, offset := range []float64{0, 0.01, 0.02} {
		detector.Observe(positioned("target-001", 50.000+offset, 30.000))
		detector.Observe(positioned("target-002", 50.005+offset, 30.005))
		detector.Observe(positioned("target-003", 50.010+offset, 30.002))
		detector.Evaluate(now)
		now = now.Add(5 * time.Second)
	}

	swarms := detector.Snapshot()
	if len(swarms) != 1 {
		t.Fatalf("Snapshot returned %d swarms, want 1", len(swarms))
	}
	if swarms[0].ID != "swarm-001" {
		t.Errorf("swarm id = %s, want the original swarm-001", swarms[0].ID)
	}
	if detector.DetectedTotal() != 1 {
		t.Errorf("DetectedTotal = %d, want 1 (same swarm tracked across moves)", detector.DetectedTotal())
	}
}

func TestSwarmDetectorDissolvesScatteredGroup(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 2000, MinSize: 3})
	now := time.Now()

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 50.005, 30.005))
	detector.Observe(positioned("target-003", 50.010, 30.002))
	detector.Evaluate(now)
	if detector.ActiveSwarms() != 1 {
		t.Fatal("swarm was not detected")
	}

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 51.000, 31.000))
	detector.Observe(positioned("target-003", 49.000, 29.000))
	detector.Evaluate(now.Add(5 * time.Second))

	if got := detector.ActiveSwarms(); got != 0 {
		t.Fatalf("ActiveSwarms after scatter = %d, want 0", got)
	}
}

func TestSwarmDetectorForgetsStalePositions(t *testing.T) {
	detector := newDetector(geofence.SwarmConfig{RadiusMeters: 2000, MinSize: 3, PositionTTL: 10 * time.Second})
	now := time.Now()

	detector.Observe(positioned("target-001", 50.000, 30.000))
	detector.Observe(positioned("target-002", 50.005, 30.005))
	detector.Observe(positioned("target-003", 50.010, 30.002))
	detector.Evaluate(now.Add(30 * time.Second))

	if got := detector.ActiveSwarms(); got != 0 {
		t.Fatalf("ActiveSwarms from stale positions = %d, want 0", got)
	}
}
