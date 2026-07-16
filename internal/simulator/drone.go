package simulator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
)

type Drone struct {
	id        string
	latitude  float64
	longitude float64
	altitude  float64
	speed     float32
	battery   int32
	rng       *rand.Rand
}

func NewDrone(index int, startLat, startLon float64, rng *rand.Rand) *Drone {
	return &Drone{
		id:        fmt.Sprintf("drone-%03d", index),
		latitude:  startLat + rng.Float64()*0.06 - 0.03,
		longitude: startLon + rng.Float64()*0.06 - 0.03,
		altitude:  100 + rng.Float64()*300,
		speed:     10 + rng.Float32()*30,
		battery:   100,
		rng:       rng,
	}
}

func (d *Drone) advance(interval time.Duration) *telemetryv1.DroneTelemetry {
	d.latitude += d.rng.Float64()*0.002 - 0.001
	d.longitude += d.rng.Float64()*0.002 - 0.001
	d.altitude += d.rng.Float64()*10 - 5
	if d.altitude < 50 {
		d.altitude = 50
	}
	d.speed += d.rng.Float32()*2 - 1
	if d.speed < 0 {
		d.speed = 0
	}
	if d.rng.Intn(int(time.Minute/interval)+1) == 0 && d.battery > 0 {
		d.battery--
	}
	return &telemetryv1.DroneTelemetry{
		DroneId:           d.id,
		Timestamp:         timestamppb.Now(),
		Latitude:          d.latitude,
		Longitude:         d.longitude,
		Altitude:          d.altitude,
		Speed:             d.speed,
		BatteryPercentage: d.battery,
	}
}

func (d *Drone) Fly(ctx context.Context, client telemetryv1.TelemetryServiceClient, interval time.Duration, logger *slog.Logger) error {
	stream, err := client.StreamTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("open stream for %s: %w", d.id, err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
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
		case <-ticker.C:
			if err := stream.Send(d.advance(interval)); err != nil {
				return fmt.Errorf("send telemetry for %s: %w", d.id, err)
			}
		}
	}
}
