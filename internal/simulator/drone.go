package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
)

const (
	ukraineMinLatitude  = 44.3
	ukraineMaxLatitude  = 52.4
	ukraineMinLongitude = 22.1
	ukraineMaxLongitude = 40.2
	spawnMinOffset      = 0.15
	spawnMaxOffset      = 0.7
	minStepDegrees      = 0.0008
	maxStepDegrees      = 0.002
	wanderJitterDegrees = 0.0002
	meanLifetime        = 4 * time.Minute
)

type Drone struct {
	id                string
	latitude          float64
	longitude         float64
	waypointLatitude  float64
	waypointLongitude float64
	altitude          float64
	speed             float32
	confidence        int32
	rng               *rand.Rand
}

func NewDrone(index int, rng *rand.Rand) *Drone {
	latitude, longitude := spawnOutsideUkraine(rng)
	drone := &Drone{
		id:         fmt.Sprintf("drone-%03d", index),
		latitude:   latitude,
		longitude:  longitude,
		altitude:   100 + rng.Float64()*300,
		speed:      10 + rng.Float32()*30,
		confidence: 60 + rng.Int32N(41),
		rng:        rng,
	}
	drone.pickWaypoint()
	return drone
}

func spawnOutsideUkraine(rng *rand.Rand) (latitude, longitude float64) {
	offset := spawnMinOffset + rng.Float64()*(spawnMaxOffset-spawnMinOffset)
	alongLatitude := ukraineMinLatitude + rng.Float64()*(ukraineMaxLatitude-ukraineMinLatitude)
	alongLongitude := ukraineMinLongitude + rng.Float64()*(ukraineMaxLongitude-ukraineMinLongitude)
	switch rng.IntN(4) {
	case 0:
		return ukraineMaxLatitude + offset, alongLongitude
	case 1:
		return ukraineMinLatitude - offset, alongLongitude
	case 2:
		return alongLatitude, ukraineMinLongitude - offset
	default:
		return alongLatitude, ukraineMaxLongitude + offset
	}
}

func (d *Drone) pickWaypoint() {
	d.waypointLatitude = ukraineMinLatitude + d.rng.Float64()*(ukraineMaxLatitude-ukraineMinLatitude)
	d.waypointLongitude = ukraineMinLongitude + d.rng.Float64()*(ukraineMaxLongitude-ukraineMinLongitude)
}

func (d *Drone) advance(interval time.Duration) *telemetryv1.DroneTelemetry {
	previousLatitude, previousLongitude := d.latitude, d.longitude
	step := minStepDegrees + d.rng.Float64()*(maxStepDegrees-minStepDegrees)
	deltaLatitude := d.waypointLatitude - d.latitude
	deltaLongitude := d.waypointLongitude - d.longitude
	distance := math.Hypot(deltaLatitude, deltaLongitude)
	if distance <= step {
		d.latitude = d.waypointLatitude
		d.longitude = d.waypointLongitude
		d.pickWaypoint()
	} else {
		d.latitude += deltaLatitude/distance*step + d.rng.Float64()*2*wanderJitterDegrees - wanderJitterDegrees
		d.longitude += deltaLongitude/distance*step + d.rng.Float64()*2*wanderJitterDegrees - wanderJitterDegrees
	}

	d.altitude += d.rng.Float64()*10 - 5
	if d.altitude < 50 {
		d.altitude = 50
	}
	movedMeters := math.Hypot(
		(d.latitude-previousLatitude)*metersPerDegree,
		(d.longitude-previousLongitude)*metersPerDegree*math.Cos(previousLatitude*math.Pi/180),
	)
	if seconds := interval.Seconds(); seconds > 0 {
		d.speed = float32(movedMeters / seconds)
	}
	d.confidence += d.rng.Int32N(9) - 4
	if d.confidence < 10 {
		d.confidence = 10
	}
	if d.confidence > 100 {
		d.confidence = 100
	}
	return &telemetryv1.DroneTelemetry{
		DroneId:    d.id,
		Timestamp:  timestamppb.Now(),
		Latitude:   d.latitude,
		Longitude:  d.longitude,
		Altitude:   d.altitude,
		Speed:      d.speed,
		Confidence: d.confidence,
	}
}

func (d *Drone) ID() string {
	return d.id
}

func (d *Drone) Fly(ctx context.Context, interval time.Duration, emit func(*telemetryv1.DroneTelemetry) error, logger *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lifetimeTicks := int(meanLifetime/interval) + 1
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if d.rng.IntN(lifetimeTicks) == 0 {
				logger.Info("drone shot down",
					"drone_id", d.id,
					"latitude", d.latitude,
					"longitude", d.longitude,
				)
				return nil
			}
			if err := emit(d.advance(interval)); err != nil {
				return fmt.Errorf("emit telemetry for %s: %w", d.id, err)
			}
		}
	}
}
