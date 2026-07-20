package fusion_test

import (
	"testing"
	"time"

	"uavmonitor/internal/fusion"
	"uavmonitor/internal/telemetry"
)

func observation(station, drone string, lat, lon float64, ts time.Time) telemetry.Sample {
	return telemetry.Sample{
		DroneID:    telemetry.DroneID(drone),
		StationID:  telemetry.StationID(station),
		Timestamp:  ts,
		Latitude:   lat,
		Longitude:  lon,
		Altitude:   100,
		Speed:      20,
		Confidence: 80,
	}
}

func TestFuserPassesThroughSamplesWithoutStation(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	sample := telemetry.Sample{DroneID: "drone-001", Latitude: 50, Longitude: 30}
	if got := fuser.Resolve(sample); got.DroneID != "drone-001" {
		t.Fatalf("DroneID = %s, want passthrough drone-001", got.DroneID)
	}
	if fuser.ActiveTracks() != 0 {
		t.Fatal("passthrough sample must not create a track")
	}
}

func TestFuserMergesObservationsFromDifferentStations(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t7", 50.0000, 30.0000, now))
	second := fuser.Resolve(observation("station-02", "s2-t3", 50.0050, 30.0050, now.Add(100*time.Millisecond)))

	if first.DroneID != second.DroneID {
		t.Fatalf("observations 600m apart got different tracks: %s vs %s", first.DroneID, second.DroneID)
	}
	if fuser.ActiveTracks() != 1 {
		t.Fatalf("ActiveTracks = %d, want 1", fuser.ActiveTracks())
	}
	if fuser.MergesTotal() != 1 {
		t.Fatalf("MergesTotal = %d, want 1", fuser.MergesTotal())
	}
	if second.Latitude <= 50.0000 || second.Latitude >= 50.0050 {
		t.Errorf("merged latitude = %f, want between the two observations", second.Latitude)
	}
}

func TestFuserKeepsTracksFromSameStationSeparate(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.0000, 30.0000, now))
	second := fuser.Resolve(observation("station-01", "s1-t2", 50.0001, 30.0001, now))

	if first.DroneID == second.DroneID {
		t.Fatal("two local tracks from one station must never fuse together")
	}
	if fuser.ActiveTracks() != 2 {
		t.Fatalf("ActiveTracks = %d, want 2", fuser.ActiveTracks())
	}
}

func TestFuserRespectsGateRadius(t *testing.T) {
	fuser := fusion.NewFuser(fusion.Config{GateRadiusMeters: 1000})
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.00, 30.00, now))
	second := fuser.Resolve(observation("station-02", "s2-t1", 50.05, 30.00, now))

	if first.DroneID == second.DroneID {
		t.Fatal("observations ~5.5km apart fused despite a 1km gate")
	}
}

func TestFuserKeepsStableIDAcrossUpdates(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.00, 30.00, now))
	for n := 1; n <= 5; n++ {
		next := fuser.Resolve(observation("station-01", "s1-t1", 50.00+float64(n)*0.001, 30.00, now.Add(time.Duration(n)*time.Second)))
		if next.DroneID != first.DroneID {
			t.Fatalf("update %d changed track id from %s to %s", n, first.DroneID, next.DroneID)
		}
	}
	if fuser.ActiveTracks() != 1 {
		t.Fatalf("ActiveTracks = %d, want 1", fuser.ActiveTracks())
	}
}

func TestFuserExpiresStaleTracks(t *testing.T) {
	fuser := fusion.NewFuser(fusion.Config{TrackTTL: 20 * time.Millisecond})
	now := time.Now()

	fuser.Resolve(observation("station-01", "s1-t1", 50.00, 30.00, now))
	time.Sleep(40 * time.Millisecond)
	fuser.Resolve(observation("station-02", "s2-t9", 55.00, 35.00, time.Now()))

	if got := fuser.ActiveTracks(); got != 1 {
		t.Fatalf("ActiveTracks after TTL = %d, want only the fresh track", got)
	}
}

func TestFuserMergedConfidenceIsMax(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	low := observation("station-01", "s1-t1", 50.0, 30.0, now)
	low.Confidence = 40
	high := observation("station-02", "s2-t1", 50.001, 30.001, now)
	high.Confidence = 95

	fuser.Resolve(low)
	merged := fuser.Resolve(high)
	if merged.Confidence != 95 {
		t.Fatalf("merged confidence = %d, want max 95", merged.Confidence)
	}
}
