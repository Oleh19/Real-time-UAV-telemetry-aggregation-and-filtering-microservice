package classify

import (
	"math"
	"sync"
	"time"

	"uavmonitor/internal/telemetry"
)

const (
	windowSize           = 20
	minSamplesToClassify = 6
	trackTTL             = time.Minute
	pruneEvery           = 15 * time.Second
	shards               = 16

	fastCruiseSpeedMps  = 280.0
	slowRotorSpeedMps   = 110.0
	erraticHeadingBound = 0.5
)

type point struct {
	latitude  float64
	longitude float64
	speed     float64
	seenAt    time.Time
}

type trackWindow struct {
	points   []point
	lastSeen time.Time
}

type classifierShard struct {
	mu        sync.Mutex
	tracks    map[telemetry.DroneID]*trackWindow
	lastPrune time.Time
}

type Classifier struct {
	shards [shards]*classifierShard
}

func NewClassifier() *Classifier {
	c := &Classifier{}
	for n := range c.shards {
		c.shards[n] = &classifierShard{tracks: make(map[telemetry.DroneID]*trackWindow)}
	}
	return c
}

func (c *Classifier) shardFor(id telemetry.DroneID) *classifierShard {
	var h uint32 = 2166136261
	for i := range len(id) {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return c.shards[h%shards]
}

func (c *Classifier) Classify(sample telemetry.Sample) telemetry.TargetClass {
	now := time.Now()
	s := c.shardFor(sample.DroneID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)

	track, ok := s.tracks[sample.DroneID]
	if !ok {
		track = &trackWindow{points: make([]point, 0, windowSize)}
		s.tracks[sample.DroneID] = track
	}
	track.lastSeen = now
	track.points = append(track.points, point{
		latitude:  sample.Latitude,
		longitude: sample.Longitude,
		speed:     float64(sample.Speed),
		seenAt:    now,
	})
	if len(track.points) > windowSize {
		track.points = track.points[1:]
	}
	if len(track.points) < minSamplesToClassify {
		return telemetry.ClassUnknown
	}
	return classify(meanSpeed(track.points), headingVariance(track.points))
}

func classify(speedMps, headingVar float64) telemetry.TargetClass {
	switch {
	case speedMps >= fastCruiseSpeedMps && headingVar < erraticHeadingBound:
		return telemetry.ClassLoiteringMunition
	case speedMps <= slowRotorSpeedMps:
		return telemetry.ClassMultirotor
	default:
		return telemetry.ClassReconUAV
	}
}

func meanSpeed(points []point) float64 {
	var sum float64
	for _, p := range points {
		sum += p.speed
	}
	return sum / float64(len(points))
}

func headingVariance(points []point) float64 {
	var sinSum, cosSum float64
	headings := 0
	for n := 1; n < len(points); n++ {
		dLat := points[n].latitude - points[n-1].latitude
		dLon := (points[n].longitude - points[n-1].longitude) * math.Cos(points[n-1].latitude*math.Pi/180)
		if dLat == 0 && dLon == 0 {
			continue
		}
		heading := math.Atan2(dLon, dLat)
		sinSum += math.Sin(heading)
		cosSum += math.Cos(heading)
		headings++
	}
	if headings == 0 {
		return 1
	}
	resultant := math.Hypot(sinSum, cosSum) / float64(headings)
	return 1 - resultant
}

func (c *Classifier) TrackedByClass() map[telemetry.TargetClass]int {
	counts := make(map[telemetry.TargetClass]int, 4)
	for _, s := range c.shards {
		s.mu.Lock()
		for _, track := range s.tracks {
			if len(track.points) < minSamplesToClassify {
				counts[telemetry.ClassUnknown]++
				continue
			}
			counts[classify(meanSpeed(track.points), headingVariance(track.points))]++
		}
		s.mu.Unlock()
	}
	return counts
}

func (s *classifierShard) pruneLocked(now time.Time) {
	if now.Sub(s.lastPrune) < pruneEvery {
		return
	}
	s.lastPrune = now
	cutoff := now.Add(-trackTTL)
	for id, track := range s.tracks {
		if track.lastSeen.Before(cutoff) {
			delete(s.tracks, id)
		}
	}
}
