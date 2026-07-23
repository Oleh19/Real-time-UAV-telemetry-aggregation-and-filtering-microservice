package fusion

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

const (
	defaultShards      = 16
	coarseCellDegrees  = 1.0
	metersPerDegreeLat = 111320.0
)

type Config struct {
	GateRadiusMeters  float64
	GateChiSquared    float64
	MeasurementNoiseM float64
	ProcessAccelMps2  float64
	TrackTTL          time.Duration
	MergeWindow       time.Duration
	Shards            int
}

func DefaultConfig() Config {
	return Config{
		GateRadiusMeters:  3000,
		GateChiSquared:    13.82,
		MeasurementNoiseM: 100,
		ProcessAccelMps2:  4,
		TrackTTL:          30 * time.Second,
		MergeWindow:       3 * time.Second,
		Shards:            defaultShards,
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

type gridCell struct {
	row int
	col int
}

type fusedTrack struct {
	id           telemetry.DroneID
	filter       *kalmanFilter
	observations map[observationKey]observation
	lastUpdate   time.Time
	cell         gridCell
}

type Fuser struct {
	cfg         Config
	shards      []*fuserShard
	nextTrack   atomic.Int64
	mergesTotal atomic.Int64
	gatedTotal  atomic.Int64
}

type fuserShard struct {
	fuser         *Fuser
	cellDegrees   float64
	mu            sync.Mutex
	tracks        map[telemetry.DroneID]*fusedTrack
	byObservation map[observationKey]telemetry.DroneID
	grid          map[gridCell]map[telemetry.DroneID]struct{}
	lastPrune     time.Time
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
	if cfg.Shards <= 0 {
		cfg.Shards = defaults.Shards
	}
	f := &Fuser{cfg: cfg}
	fineCellDegrees := cfg.GateRadiusMeters / metersPerDegreeLat
	f.shards = make([]*fuserShard, cfg.Shards)
	for n := range f.shards {
		f.shards[n] = &fuserShard{
			fuser:         f,
			cellDegrees:   fineCellDegrees,
			tracks:        make(map[telemetry.DroneID]*fusedTrack),
			byObservation: make(map[observationKey]telemetry.DroneID),
			grid:          make(map[gridCell]map[telemetry.DroneID]struct{}),
		}
	}
	return f
}

func (f *Fuser) Resolve(sample telemetry.Sample) telemetry.Sample {
	if sample.StationID == "" {
		return sample
	}
	return f.shardFor(sample.Latitude, sample.Longitude).resolve(sample)
}

func (f *Fuser) shardFor(latitude, longitude float64) *fuserShard {
	cell := cellOf(latitude, longitude, coarseCellDegrees)
	h := cell.row*73856093 ^ cell.col*19349663
	if h < 0 {
		h = -h
	}
	return f.shards[h%len(f.shards)]
}

func (s *fuserShard) resolve(sample telemetry.Sample) telemetry.Sample {
	key := observationKey{station: sample.StationID, drone: sample.DroneID}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	track, isNew := s.trackForLocked(key, sample, now)
	if !isNew {
		track.filter.update(sample.Latitude, sample.Longitude, sample.Timestamp)
		s.reindexLocked(track)
	}
	track.observations[key] = observation{sample: sample, seenAt: now}
	track.lastUpdate = now
	return s.fusedSampleLocked(track, sample.Timestamp, now)
}

func (s *fuserShard) trackForLocked(key observationKey, sample telemetry.Sample, now time.Time) (*fusedTrack, bool) {
	if id, ok := s.byObservation[key]; ok {
		if track, alive := s.tracks[id]; alive {
			return track, false
		}
		delete(s.byObservation, key)
	}
	if track := s.bestCandidateLocked(key, sample); track != nil {
		s.byObservation[key] = track.id
		s.fuser.mergesTotal.Add(1)
		return track, false
	}
	id := telemetry.DroneID(fmt.Sprintf("target-%03d", s.fuser.nextTrack.Add(1)))
	track := &fusedTrack{
		id:           id,
		filter:       newKalmanFilter(sample.Latitude, sample.Longitude, sample.Timestamp, s.fuser.cfg.MeasurementNoiseM, s.fuser.cfg.ProcessAccelMps2),
		observations: make(map[observationKey]observation),
		lastUpdate:   now,
		cell:         cellOf(sample.Latitude, sample.Longitude, s.cellDegrees),
	}
	s.tracks[id] = track
	s.addToGridLocked(track)
	s.byObservation[key] = id
	return track, true
}

func (s *fuserShard) bestCandidateLocked(key observationKey, sample telemetry.Sample) *fusedTrack {
	origin := cellOf(sample.Latitude, sample.Longitude, s.cellDegrees)
	var best *fusedTrack
	bestDistance := s.fuser.cfg.GateChiSquared
	for dRow := -1; dRow <= 1; dRow++ {
		for dCol := -1; dCol <= 1; dCol++ {
			cell := gridCell{row: origin.row + dRow, col: origin.col + dCol}
			for id := range s.grid[cell] {
				track := s.tracks[id]
				if track == nil || s.stationSeesTrackLocked(track, key.station) {
					continue
				}
				latitude, longitude := track.filter.position()
				if coarseDistanceMeters(latitude, longitude, sample.Latitude, sample.Longitude) > s.fuser.cfg.GateRadiusMeters {
					continue
				}
				distance := track.filter.mahalanobisSquared(sample.Latitude, sample.Longitude, sample.Timestamp)
				if distance <= bestDistance {
					best = track
					bestDistance = distance
				} else if distance > s.fuser.cfg.GateChiSquared {
					s.fuser.gatedTotal.Add(1)
				}
			}
		}
	}
	return best
}

func (s *fuserShard) reindexLocked(track *fusedTrack) {
	latitude, longitude := track.filter.position()
	newCell := cellOf(latitude, longitude, s.cellDegrees)
	if newCell == track.cell {
		return
	}
	s.removeFromGridLocked(track)
	track.cell = newCell
	s.addToGridLocked(track)
}

func (s *fuserShard) addToGridLocked(track *fusedTrack) {
	bucket := s.grid[track.cell]
	if bucket == nil {
		bucket = make(map[telemetry.DroneID]struct{})
		s.grid[track.cell] = bucket
	}
	bucket[track.id] = struct{}{}
}

func (s *fuserShard) removeFromGridLocked(track *fusedTrack) {
	bucket := s.grid[track.cell]
	if bucket == nil {
		return
	}
	delete(bucket, track.id)
	if len(bucket) == 0 {
		delete(s.grid, track.cell)
	}
}

func (s *fuserShard) stationSeesTrackLocked(track *fusedTrack, station telemetry.StationID) bool {
	for key := range track.observations {
		if key.station == station {
			return true
		}
	}
	return false
}

func (s *fuserShard) fusedSampleLocked(track *fusedTrack, timestamp time.Time, now time.Time) telemetry.Sample {
	latitude, longitude := track.filter.position()
	fused := telemetry.Sample{
		DroneID:   track.id,
		Timestamp: timestamp,
		Latitude:  latitude,
		Longitude: longitude,
		Speed:     float32(track.filter.speed()),
	}
	cutoff := now.Add(-s.fuser.cfg.MergeWindow)
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

func (s *fuserShard) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < s.fuser.cfg.TrackTTL/4 {
		return
	}
	s.lastPrune = now
	cutoff := now.Add(-s.fuser.cfg.TrackTTL)
	for id, track := range s.tracks {
		if track.lastUpdate.After(cutoff) {
			continue
		}
		s.removeFromGridLocked(track)
		delete(s.tracks, id)
		for key, fusedID := range s.byObservation {
			if fusedID == id {
				delete(s.byObservation, key)
			}
		}
	}
}

func (f *Fuser) Run(ctx context.Context) {
	ticker := time.NewTicker(f.cfg.TrackTTL / 4)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.Sweep()
		}
	}
}

func (f *Fuser) Sweep() {
	now := time.Now()
	for _, shard := range f.shards {
		shard.mu.Lock()
		shard.lastPrune = time.Time{}
		shard.pruneLocked(now)
		shard.mu.Unlock()
	}
}

func (f *Fuser) ActiveTracks() int {
	total := 0
	for _, shard := range f.shards {
		shard.mu.Lock()
		total += len(shard.tracks)
		shard.mu.Unlock()
	}
	return total
}

func (f *Fuser) MergesTotal() int64 {
	return f.mergesTotal.Load()
}

func (f *Fuser) GatedTotal() int64 {
	return f.gatedTotal.Load()
}

func cellOf(latitude, longitude, sizeDegrees float64) gridCell {
	return gridCell{
		row: int(math.Floor((latitude + 90) / sizeDegrees)),
		col: int(math.Floor((longitude + 180) / sizeDegrees)),
	}
}

func coarseDistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * metersPerDegreeEquator
	dLon := (lon2 - lon1) * metersPerDegreeEquator * math.Cos(lat1*math.Pi/180)
	return math.Hypot(dLat, dLon)
}
