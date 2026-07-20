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
	GateRadiusMeters float64
	TrackTTL         time.Duration
	MergeWindow      time.Duration
}

func DefaultConfig() Config {
	return Config{
		GateRadiusMeters: 3000,
		TrackTTL:         30 * time.Second,
		MergeWindow:      3 * time.Second,
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
	observations map[observationKey]observation
	lastSample   telemetry.Sample
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
}

func NewFuser(cfg Config) *Fuser {
	if cfg.GateRadiusMeters <= 0 {
		cfg.GateRadiusMeters = DefaultConfig().GateRadiusMeters
	}
	if cfg.TrackTTL <= 0 {
		cfg.TrackTTL = DefaultConfig().TrackTTL
	}
	if cfg.MergeWindow <= 0 {
		cfg.MergeWindow = DefaultConfig().MergeWindow
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

	track := f.trackForLocked(key, sample, now)
	track.observations[key] = observation{sample: sample, seenAt: now}
	track.lastUpdate = now
	track.lastSample = f.mergeLocked(track, now)
	return track.lastSample
}

func (f *Fuser) trackForLocked(key observationKey, sample telemetry.Sample, now time.Time) *fusedTrack {
	if id, ok := f.byObservation[key]; ok {
		if track, alive := f.tracks[id]; alive {
			return track
		}
		delete(f.byObservation, key)
	}
	if track := f.nearestCandidateLocked(key, sample); track != nil {
		f.byObservation[key] = track.id
		f.mergesTotal.Add(1)
		return track
	}
	f.nextTrack++
	track := &fusedTrack{
		id:           telemetry.DroneID(fmt.Sprintf("target-%03d", f.nextTrack)),
		observations: make(map[observationKey]observation),
		lastUpdate:   now,
	}
	f.tracks[track.id] = track
	f.byObservation[key] = track.id
	return track
}

func (f *Fuser) nearestCandidateLocked(key observationKey, sample telemetry.Sample) *fusedTrack {
	var best *fusedTrack
	bestDistance := f.cfg.GateRadiusMeters
	for _, track := range f.tracks {
		if f.stationSeesTrackLocked(track, key.station) {
			continue
		}
		distance := distanceMeters(
			track.lastSample.Latitude, track.lastSample.Longitude,
			sample.Latitude, sample.Longitude,
		)
		if distance <= bestDistance {
			best = track
			bestDistance = distance
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

func (f *Fuser) mergeLocked(track *fusedTrack, now time.Time) telemetry.Sample {
	cutoff := now.Add(-f.cfg.MergeWindow)
	merged := telemetry.Sample{DroneID: track.id}
	var latSum, lonSum, altSum, speedSum float64
	count := 0
	for _, obs := range track.observations {
		if obs.seenAt.Before(cutoff) {
			continue
		}
		latSum += obs.sample.Latitude
		lonSum += obs.sample.Longitude
		altSum += obs.sample.Altitude
		speedSum += float64(obs.sample.Speed)
		if obs.sample.Confidence > merged.Confidence {
			merged.Confidence = obs.sample.Confidence
		}
		if obs.sample.Timestamp.After(merged.Timestamp) {
			merged.Timestamp = obs.sample.Timestamp
		}
		count++
	}
	if count == 0 {
		merged = track.lastSample
		merged.DroneID = track.id
		return merged
	}
	merged.Latitude = latSum / float64(count)
	merged.Longitude = lonSum / float64(count)
	merged.Altitude = altSum / float64(count)
	merged.Speed = float32(speedSum / float64(count))
	return merged
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

func distanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const metersPerDegree = 111320.0
	dLat := (lat2 - lat1) * metersPerDegree
	dLon := (lon2 - lon1) * metersPerDegree * math.Cos(lat1*math.Pi/180)
	return math.Hypot(dLat, dLon)
}
