package anomaly

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

const (
	shards               = 16
	maxPlausibleSpeedMps = 600.0
	trackTTL             = time.Minute
	pruneEvery           = 15 * time.Second
	metersPerDegree      = 111320.0
)

type lastPosition struct {
	latitude  float64
	longitude float64
	timestamp time.Time
	seenAt    time.Time
}

type detectorShard struct {
	mu        sync.Mutex
	tracks    map[telemetry.DroneID]lastPosition
	lastPrune time.Time
}

type Detector struct {
	shards [shards]*detectorShard
	total  atomic.Int64
}

func NewDetector() *Detector {
	d := &Detector{}
	for n := range d.shards {
		d.shards[n] = &detectorShard{tracks: make(map[telemetry.DroneID]lastPosition)}
	}
	return d
}

func (d *Detector) shardFor(id telemetry.DroneID) *detectorShard {
	var h uint32 = 2166136261
	for i := range len(id) {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return d.shards[h%shards]
}

func (d *Detector) Check(sample telemetry.Sample) bool {
	now := time.Now()
	s := d.shardFor(sample.DroneID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	current := lastPosition{
		latitude:  sample.Latitude,
		longitude: sample.Longitude,
		timestamp: sample.Timestamp,
		seenAt:    now,
	}
	previous, known := s.tracks[sample.DroneID]
	s.tracks[sample.DroneID] = current
	if !known {
		return false
	}
	dt := sample.Timestamp.Sub(previous.timestamp).Seconds()
	if dt <= 0 {
		return false
	}
	if impliedSpeed(previous, current)/dt > maxPlausibleSpeedMps {
		d.total.Add(1)
		return true
	}
	return false
}

func (d *Detector) Total() int64 {
	return d.total.Load()
}

func impliedSpeed(a, b lastPosition) float64 {
	dLat := (b.latitude - a.latitude) * metersPerDegree
	dLon := (b.longitude - a.longitude) * metersPerDegree * math.Cos(a.latitude*math.Pi/180)
	return math.Hypot(dLat, dLon)
}

func (s *detectorShard) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	s.lastPrune = now
	cutoff := now.Add(-trackTTL)
	for id, pos := range s.tracks {
		if pos.seenAt.Before(cutoff) {
			delete(s.tracks, id)
		}
	}
}
