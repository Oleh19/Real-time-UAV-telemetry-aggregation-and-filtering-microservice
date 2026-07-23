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
	meanLifetime        = 4 * time.Minute
)

type flightProfile struct {
	name           string
	minStepDegrees float64
	maxStepDegrees float64
	jitterDegrees  float64
	minAltitude    float64
	maxAltitude    float64
}

var flightProfiles = []flightProfile{
	{name: "strike", minStepDegrees: 0.0015, maxStepDegrees: 0.002, jitterDegrees: 0.00005, minAltitude: 150, maxAltitude: 350},
	{name: "recon", minStepDegrees: 0.0007, maxStepDegrees: 0.001, jitterDegrees: 0.0002, minAltitude: 300, maxAltitude: 500},
	{name: "multirotor", minStepDegrees: 0.0002, maxStepDegrees: 0.0004, jitterDegrees: 0.0004, minAltitude: 50, maxAltitude: 150},
}

type Drone struct {
	id                string
	profile           flightProfile
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
	profile := flightProfiles[rng.IntN(len(flightProfiles))]
	drone := &Drone{
		id:         fmt.Sprintf("drone-%03d", index),
		profile:    profile,
		latitude:   latitude,
		longitude:  longitude,
		altitude:   profile.minAltitude + rng.Float64()*(profile.maxAltitude-profile.minAltitude),
		confidence: 60 + rng.Int32N(41),
		rng:        rng,
	}
	drone.pickWaypoint()
	return drone
}

func (d *Drone) Profile() string {
	return d.profile.name
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
	step := d.profile.minStepDegrees + d.rng.Float64()*(d.profile.maxStepDegrees-d.profile.minStepDegrees)
	jitter := d.profile.jitterDegrees
	deltaLatitude := d.waypointLatitude - d.latitude
	deltaLongitude := d.waypointLongitude - d.longitude
	distance := math.Hypot(deltaLatitude, deltaLongitude)
	if distance <= step {
		d.latitude = d.waypointLatitude
		d.longitude = d.waypointLongitude
		d.pickWaypoint()
	} else {
		d.latitude += deltaLatitude/distance*step + d.rng.Float64()*2*jitter - jitter
		d.longitude += deltaLongitude/distance*step + d.rng.Float64()*2*jitter - jitter
	}

	d.altitude += d.rng.Float64()*10 - 5
	if d.altitude < d.profile.minAltitude {
		d.altitude = d.profile.minAltitude
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

func (d *Drone) Position() (latitude, longitude float64) {
	return d.latitude, d.longitude
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
