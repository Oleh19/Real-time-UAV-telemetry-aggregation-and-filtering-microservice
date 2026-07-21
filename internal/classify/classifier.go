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

type Classifier struct {
	mu        sync.Mutex
	tracks    map[telemetry.DroneID]*trackWindow
	lastPrune time.Time
}

func NewClassifier() *Classifier {
	return &Classifier{tracks: make(map[telemetry.DroneID]*trackWindow)}
}

func (c *Classifier) Classify(sample telemetry.Sample) telemetry.TargetClass {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)

	track, ok := c.tracks[sample.DroneID]
	if !ok {
		track = &trackWindow{points: make([]point, 0, windowSize)}
		c.tracks[sample.DroneID] = track
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
	c.mu.Lock()
	defer c.mu.Unlock()
	counts := make(map[telemetry.TargetClass]int, 4)
	for _, track := range c.tracks {
		if len(track.points) < minSamplesToClassify {
			counts[telemetry.ClassUnknown]++
			continue
		}
		counts[classify(meanSpeed(track.points), headingVariance(track.points))]++
	}
	return counts
}

func (c *Classifier) pruneLocked(now time.Time) {
	if now.Sub(c.lastPrune) < pruneEvery {
		return
	}
	c.lastPrune = now
	cutoff := now.Add(-trackTTL)
	for id, track := range c.tracks {
		if track.lastSeen.Before(cutoff) {
			delete(c.tracks, id)
		}
	}
}
