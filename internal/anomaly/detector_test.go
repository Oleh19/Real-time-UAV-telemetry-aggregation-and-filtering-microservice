package anomaly_test

import (
	"testing"
	"time"

	"uavmonitor/internal/anomaly"
	"uavmonitor/internal/telemetry"
)

func at(id string, lat, lon float64, ts time.Time) telemetry.Sample {
	return telemetry.Sample{DroneID: telemetry.DroneID(id), Latitude: lat, Longitude: lon, Timestamp: ts}
}

func TestFirstSampleIsNeverAnomalous(t *testing.T) {
	d := anomaly.NewDetector()
	if d.Check(at("target-001", 50, 30, time.Now())) {
		t.Fatal("first sample flagged as anomalous")
	}
}

func TestPlausibleMovementIsClean(t *testing.T) {
	d := anomaly.NewDetector()
	base := time.Now()
	d.Check(at("target-001", 50.0, 30.0, base))
	if d.Check(at("target-001", 50.001, 30.0, base.Add(time.Second))) {
		t.Fatal("~111 m/s movement flagged, should be plausible")
	}
	if d.Total() != 0 {
		t.Fatalf("Total = %d, want 0", d.Total())
	}
}

func TestTeleportIsFlagged(t *testing.T) {
	d := anomaly.NewDetector()
	base := time.Now()
	d.Check(at("target-001", 50.0, 30.0, base))
	if !d.Check(at("target-001", 55.0, 35.0, base.Add(time.Second))) {
		t.Fatal("a ~600km jump in one second was not flagged")
	}
	if d.Total() != 1 {
		t.Fatalf("Total = %d, want 1", d.Total())
	}
}

func TestOutOfOrderIsNotFlagged(t *testing.T) {
	d := anomaly.NewDetector()
	base := time.Now()
	d.Check(at("target-001", 50.0, 30.0, base))
	if d.Check(at("target-001", 55.0, 35.0, base.Add(-time.Second))) {
		t.Fatal("out-of-order sample flagged instead of ignored")
	}
}

func TestPerTrackIsolation(t *testing.T) {
	d := anomaly.NewDetector()
	base := time.Now()
	d.Check(at("target-001", 50.0, 30.0, base))
	d.Check(at("target-002", 46.0, 34.0, base))
	if d.Check(at("target-002", 46.001, 34.0, base.Add(time.Second))) {
		t.Fatal("target-002 movement judged against target-001 position")
	}
}
