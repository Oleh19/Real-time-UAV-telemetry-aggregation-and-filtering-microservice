package simulator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
)

const (
	ukraineMinLatitude  = 44.3
	ukraineMaxLatitude  = 52.4
	ukraineMinLongitude = 22.1
	ukraineMaxLongitude = 40.2
	spawnMinOffset      = 0.3
	spawnMaxOffset      = 1.5
	minStepDegrees      = 0.006
	maxStepDegrees      = 0.016
	meanLifetime        = 3 * time.Minute
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
		confidence: 60 + rng.Int31n(41),
		rng:        rng,
	}
	drone.pickWaypoint()
	return drone
}

func spawnOutsideUkraine(rng *rand.Rand) (latitude, longitude float64) {
	offset := spawnMinOffset + rng.Float64()*(spawnMaxOffset-spawnMinOffset)
	alongLatitude := ukraineMinLatitude + rng.Float64()*(ukraineMaxLatitude-ukraineMinLatitude)
	alongLongitude := ukraineMinLongitude + rng.Float64()*(ukraineMaxLongitude-ukraineMinLongitude)
	switch rng.Intn(4) {
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

func (d *Drone) advance() *telemetryv1.DroneTelemetry {
	step := minStepDegrees + d.rng.Float64()*(maxStepDegrees-minStepDegrees)
	deltaLatitude := d.waypointLatitude - d.latitude
	deltaLongitude := d.waypointLongitude - d.longitude
	distance := math.Hypot(deltaLatitude, deltaLongitude)
	if distance <= step {
		d.latitude = d.waypointLatitude
		d.longitude = d.waypointLongitude
		d.pickWaypoint()
	} else {
		d.latitude += deltaLatitude/distance*step + d.rng.Float64()*0.002 - 0.001
		d.longitude += deltaLongitude/distance*step + d.rng.Float64()*0.002 - 0.001
	}

	d.altitude += d.rng.Float64()*10 - 5
	if d.altitude < 50 {
		d.altitude = 50
	}
	d.speed += d.rng.Float32()*2 - 1
	if d.speed < 0 {
		d.speed = 0
	}
	d.confidence += d.rng.Int31n(9) - 4
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

func (d *Drone) Fly(ctx context.Context, client telemetryv1.TelemetryServiceClient, interval time.Duration, logger *slog.Logger) error {
	stream, err := client.StreamTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("open stream for %s: %w", d.id, err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lifetimeTicks := int(meanLifetime/interval) + 1
	for {
		select {
		case <-ctx.Done():
			return d.closeStream(stream, logger)
		case <-ticker.C:
			if d.rng.Intn(lifetimeTicks) == 0 {
				logger.Info("drone shot down",
					"drone_id", d.id,
					"latitude", d.latitude,
					"longitude", d.longitude,
				)
				return d.closeStream(stream, logger)
			}
			if err := stream.Send(d.advance()); err != nil {
				return fmt.Errorf("send telemetry for %s: %w", d.id, err)
			}
		}
	}
}

func (d *Drone) closeStream(stream telemetryv1.TelemetryService_StreamTelemetryClient, logger *slog.Logger) error {
	summary, err := stream.CloseAndRecv()
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("close stream for %s: %w", d.id, err)
	}
	if summary != nil {
		logger.Info("stream closed",
			"drone_id", d.id,
			"received_by_server", summary.GetReceivedCount(),
			"dropped_by_server", summary.GetDroppedCount(),
			"rejected_by_server", summary.GetRejectedCount(),
		)
	}
	return nil
}
