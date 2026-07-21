package fusion

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

type Config struct {
	GateRadiusMeters  float64
	GateChiSquared    float64
	MeasurementNoiseM float64
	ProcessAccelMps2  float64
	TrackTTL          time.Duration
	MergeWindow       time.Duration
}

func DefaultConfig() Config {
	return Config{
		GateRadiusMeters:  3000,
		GateChiSquared:    13.82,
		MeasurementNoiseM: 100,
		ProcessAccelMps2:  4,
		TrackTTL:          30 * time.Second,
		MergeWindow:       3 * time.Second,
	}
}

type observationKey struct {
	station telemetry.StationID
	drone   telemetry.DroneID
}

type observation struct {
	sample telemetry.Sample
	seenAt time.Time
}

type fusedTrack struct {
	id           telemetry.DroneID
	filter       *kalmanFilter
	observations map[observationKey]observation
	lastUpdate   time.Time
}

type Fuser struct {
	cfg           Config
	mu            sync.Mutex
	tracks        map[telemetry.DroneID]*fusedTrack
	byObservation map[observationKey]telemetry.DroneID
	nextTrack     int64
	lastPrune     time.Time
	mergesTotal   atomic.Int64
	gatedTotal    atomic.Int64
}

func NewFuser(cfg Config) *Fuser {
	defaults := DefaultConfig()
	if cfg.GateRadiusMeters <= 0 {
		cfg.GateRadiusMeters = defaults.GateRadiusMeters
	}
	if cfg.GateChiSquared <= 0 {
		cfg.GateChiSquared = defaults.GateChiSquared
	}
	if cfg.MeasurementNoiseM <= 0 {
		cfg.MeasurementNoiseM = defaults.MeasurementNoiseM
	}
	if cfg.ProcessAccelMps2 <= 0 {
		cfg.ProcessAccelMps2 = defaults.ProcessAccelMps2
	}
	if cfg.TrackTTL <= 0 {
		cfg.TrackTTL = defaults.TrackTTL
	}
	if cfg.MergeWindow <= 0 {
		cfg.MergeWindow = defaults.MergeWindow
	}
	return &Fuser{
		cfg:           cfg,
		tracks:        make(map[telemetry.DroneID]*fusedTrack),
		byObservation: make(map[observationKey]telemetry.DroneID),
	}
}

func (f *Fuser) Resolve(sample telemetry.Sample) telemetry.Sample {
	if sample.StationID == "" {
		return sample
	}
	key := observationKey{station: sample.StationID, drone: sample.DroneID}
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLocked(now)

	track, isNew := f.trackForLocked(key, sample, now)
	if !isNew {
		track.filter.update(sample.Latitude, sample.Longitude, sample.Timestamp)
	}
	track.observations[key] = observation{sample: sample, seenAt: now}
	track.lastUpdate = now
	return f.fusedSampleLocked(track, sample.Timestamp, now)
}

func (f *Fuser) trackForLocked(key observationKey, sample telemetry.Sample, now time.Time) (*fusedTrack, bool) {
	if id, ok := f.byObservation[key]; ok {
		if track, alive := f.tracks[id]; alive {
			return track, false
		}
		delete(f.byObservation, key)
	}
	if track := f.bestCandidateLocked(key, sample); track != nil {
		f.byObservation[key] = track.id
		f.mergesTotal.Add(1)
		return track, false
	}
	f.nextTrack++
	track := &fusedTrack{
		id:           telemetry.DroneID(fmt.Sprintf("target-%03d", f.nextTrack)),
		filter:       newKalmanFilter(sample.Latitude, sample.Longitude, sample.Timestamp, f.cfg.MeasurementNoiseM, f.cfg.ProcessAccelMps2),
		observations: make(map[observationKey]observation),
		lastUpdate:   now,
	}
	f.tracks[track.id] = track
	f.byObservation[key] = track.id
	return track, true
}

func (f *Fuser) bestCandidateLocked(key observationKey, sample telemetry.Sample) *fusedTrack {
	var best *fusedTrack
	bestDistance := f.cfg.GateChiSquared
	for _, track := range f.tracks {
		if f.stationSeesTrackLocked(track, key.station) {
			continue
		}
		latitude, longitude := track.filter.position()
		if coarseDistanceMeters(latitude, longitude, sample.Latitude, sample.Longitude) > f.cfg.GateRadiusMeters {
			continue
		}
		distance := track.filter.mahalanobisSquared(sample.Latitude, sample.Longitude, sample.Timestamp)
		if distance <= bestDistance {
			best = track
			bestDistance = distance
		} else if distance > f.cfg.GateChiSquared {
			f.gatedTotal.Add(1)
		}
	}
	return best
}

func (f *Fuser) stationSeesTrackLocked(track *fusedTrack, station telemetry.StationID) bool {
	for key := range track.observations {
		if key.station == station {
			return true
		}
	}
	return false
}

func (f *Fuser) fusedSampleLocked(track *fusedTrack, timestamp time.Time, now time.Time) telemetry.Sample {
	latitude, longitude := track.filter.position()
	fused := telemetry.Sample{
		DroneID:   track.id,
		Timestamp: timestamp,
		Latitude:  latitude,
		Longitude: longitude,
		Speed:     float32(track.filter.speed()),
	}
	cutoff := now.Add(-f.cfg.MergeWindow)
	var altitudeSum float64
	fresh := 0
	for _, obs := range track.observations {
		if obs.seenAt.Before(cutoff) {
			continue
		}
		altitudeSum += obs.sample.Altitude
		if obs.sample.Confidence > fused.Confidence {
			fused.Confidence = obs.sample.Confidence
		}
		fresh++
	}
	if fresh > 0 {
		fused.Altitude = altitudeSum / float64(fresh)
	}
	return fused
}

func (f *Fuser) pruneLocked(now time.Time) {
	if now.Sub(f.lastPrune) < f.cfg.TrackTTL/4 {
		return
	}
	f.lastPrune = now
	cutoff := now.Add(-f.cfg.TrackTTL)
	for id, track := range f.tracks {
		if track.lastUpdate.After(cutoff) {
			continue
		}
		delete(f.tracks, id)
		for key, fusedID := range f.byObservation {
			if fusedID == id {
				delete(f.byObservation, key)
			}
		}
	}
}

func (f *Fuser) ActiveTracks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tracks)
}

func (f *Fuser) MergesTotal() int64 {
	return f.mergesTotal.Load()
}

func (f *Fuser) GatedTotal() int64 {
	return f.gatedTotal.Load()
}

func coarseDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * metersPerDegreeEquator
	dLon := (lon2 - lon1) * metersPerDegreeEquator * math.Cos(lat1*math.Pi/180)
	return math.Hypot(dLat, dLon)
}
