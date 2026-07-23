package fusion_test

import (
	"fmt"
	"math"
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
	second := fuser.Resolve(observation("station-02", "s2-t3", 50.0009, 30.0006, now.Add(100*time.Millisecond)))

	if first.DroneID != second.DroneID {
		t.Fatalf("observations ~110m apart got different tracks: %s vs %s", first.DroneID, second.DroneID)
	}
	if fuser.ActiveTracks() != 1 {
		t.Fatalf("ActiveTracks = %d, want 1", fuser.ActiveTracks())
	}
	if fuser.MergesTotal() != 1 {
		t.Fatalf("MergesTotal = %d, want 1", fuser.MergesTotal())
	}
	if second.Latitude <= 50.0000 || second.Latitude >= 50.0009 {
		t.Errorf("fused latitude = %f, want the filter estimate between the two observations", second.Latitude)
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

func TestFuserMahalanobisGateRejectsDistantObservation(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.000, 30.000, now))
	second := fuser.Resolve(observation("station-02", "s2-t1", 50.006, 30.000, now.Add(100*time.Millisecond)))

	if first.DroneID == second.DroneID {
		t.Fatal("observation ~670m away associated despite the Mahalanobis gate")
	}
	if fuser.ActiveTracks() != 2 {
		t.Fatalf("ActiveTracks = %d, want 2 separate tracks", fuser.ActiveTracks())
	}
}

func TestFuserRespectsCoarseGateRadius(t *testing.T) {
	fuser := fusion.NewFuser(fusion.Config{GateRadiusMeters: 1000})
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.00, 30.00, now))
	second := fuser.Resolve(observation("station-02", "s2-t1", 50.05, 30.00, now))

	if first.DroneID == second.DroneID {
		t.Fatal("observations ~5.5km apart fused despite a 1km coarse gate")
	}
}

func TestFuserKeepsStableIDAcrossUpdates(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	first := fuser.Resolve(observation("station-01", "s1-t1", 50.00, 30.00, now))
	for n := 1; n <= 5; n++ {
		next := fuser.Resolve(observation("station-01", "s1-t1", 50.00+float64(n)*0.0002, 30.00, now.Add(time.Duration(n)*time.Second)))
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
	fuser.Sweep()

	if got := fuser.ActiveTracks(); got != 1 {
		t.Fatalf("ActiveTracks after TTL = %d, want only the fresh track", got)
	}
}

func TestFuserSpreadsLoadAcrossShards(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()
	for n := range 200 {
		lat := 45.0 + float64(n%40)*0.15
		lon := 23.0 + float64(n%30)*0.4
		drone := telemetry.DroneID(fmt.Sprintf("s1-t%d", n))
		fuser.Resolve(observation("station-01", string(drone), lat, lon, now))
	}
	if got := fuser.ActiveTracks(); got != 200 {
		t.Fatalf("ActiveTracks = %d, want 200 distinct tracks across shards", got)
	}
}

func TestFuserMergedConfidenceIsMax(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	now := time.Now()

	low := observation("station-01", "s1-t1", 50.0, 30.0, now)
	low.Confidence = 40
	high := observation("station-02", "s2-t1", 50.0008, 30.0005, now)
	high.Confidence = 95

	fuser.Resolve(low)
	merged := fuser.Resolve(high)
	if merged.Confidence != 95 {
		t.Fatalf("merged confidence = %d, want max 95", merged.Confidence)
	}
}

func TestKalmanFilterSmoothsNoisyStraightTrack(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	start := time.Now().Add(-time.Minute)

	const (
		ticks        = 40
		stepSeconds  = 0.5
		trueSpeedMps = 60.0
		noiseMeters  = 90.0
	)
	stepDegrees := trueSpeedMps * stepSeconds / 111320.0
	noiseDegrees := noiseMeters / 111320.0

	var rawErrorSum, fusedErrorSum float64
	var lastFused telemetry.Sample
	for n := range ticks {
		trueLat := 50.0 + float64(n)*stepDegrees
		noise := noiseDegrees
		if n%2 == 1 {
			noise = -noiseDegrees
		}
		obs := observation("station-01", "s1-t1", trueLat+noise, 30.0, start.Add(time.Duration(float64(n)*stepSeconds*float64(time.Second))))
		lastFused = fuser.Resolve(obs)
		if n >= ticks/2 {
			rawErrorSum += math.Abs(obs.Latitude - trueLat)
			fusedErrorSum += math.Abs(lastFused.Latitude - trueLat)
		}
	}

	if fusedErrorSum >= rawErrorSum {
		t.Fatalf("fused error %.6f >= raw error %.6f, filter did not smooth the track", fusedErrorSum, rawErrorSum)
	}
	if fusedErrorSum > rawErrorSum/2 {
		t.Errorf("fused error %.6f is more than half the raw error %.6f, expected stronger smoothing", fusedErrorSum, rawErrorSum)
	}
	if lastFused.Speed < 35 || lastFused.Speed > 85 {
		t.Errorf("estimated speed = %.1f m/s, want near the true 60 m/s", lastFused.Speed)
	}
}

func TestKalmanFilterPredictsThroughMissedFrames(t *testing.T) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	start := time.Now().Add(-time.Minute)

	stepDegrees := 30.0 / 111320.0
	var track telemetry.DroneID
	for n := range 10 {
		fused := fuser.Resolve(observation("station-01", "s1-t1", 50.0+float64(n)*stepDegrees, 30.0, start.Add(time.Duration(n)*time.Second)))
		track = fused.DroneID
	}

	resumed := fuser.Resolve(observation("station-01", "s1-t1", 50.0+14*stepDegrees, 30.0, start.Add(14*time.Second)))
	if resumed.DroneID != track {
		t.Fatalf("track id changed after a 4s gap: %s vs %s", resumed.DroneID, track)
	}
	wantLat := 50.0 + 14*stepDegrees
	if math.Abs(resumed.Latitude-wantLat)*111320 > 60 {
		t.Errorf("post-gap estimate is %.1fm from truth, want the filter to have predicted through the gap", math.Abs(resumed.Latitude-wantLat)*111320)
	}
}
