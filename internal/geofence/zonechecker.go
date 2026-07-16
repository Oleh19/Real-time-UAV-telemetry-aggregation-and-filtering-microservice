package geofence

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type ZoneRepository interface {
	BreachedZones(ctx context.Context, longitude, latitude float64) ([]telemetry.NoFlyZone, error)
}

type AlertPublisher interface {
	PublishAlert(ctx context.Context, breach telemetry.ZoneBreach) error
}

const (
	staleDroneStateAfter = 10 * time.Minute
	pruneStateEvery      = time.Minute
	redeliveryDelay      = 5 * time.Second
)

type droneZoneState struct {
	zones         map[telemetry.ZoneID]string
	lastTimestamp time.Time
	lastSeen      time.Time
}

type ZoneChecker struct {
	repo      ZoneRepository
	alerts    AlertPublisher
	logger    *slog.Logger
	mu        sync.Mutex
	state     map[telemetry.DroneID]*droneZoneState
	lastPrune time.Time
}

func NewZoneChecker(repo ZoneRepository, alerts AlertPublisher, logger *slog.Logger) *ZoneChecker {
	return &ZoneChecker{
		repo:   repo,
		alerts: alerts,
		logger: logger,
		state:  make(map[telemetry.DroneID]*droneZoneState),
	}
}

func (z *ZoneChecker) Run(ctx context.Context, consumer jetstream.Consumer, workerCount, queueSize int) error {
	messages := make(chan jetstream.Msg, queueSize)
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		select {
		case <-ctx.Done():
		case messages <- msg:
		}
	})
	if err != nil {
		return fmt.Errorf("consume telemetry for zone checks: %w", err)
	}
	defer consumeCtx.Stop()

	z.logger.Info("zone checker started", "worker_count", workerCount)

	var wg sync.WaitGroup
	for n := 0; n < workerCount; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-messages:
					z.acknowledge(msg, z.Process(ctx, msg.Data()))
				}
			}
		}()
	}
	wg.Wait()
	return nil
}

func (z *ZoneChecker) acknowledge(msg jetstream.Msg, processErr error) {
	if processErr != nil {
		z.logger.Error("process telemetry", "error", processErr)
		if err := msg.NakWithDelay(redeliveryDelay); err != nil {
			z.logger.Error("nak telemetry message", "error", err)
		}
		return
	}
	if err := msg.Ack(); err != nil {
		z.logger.Error("ack telemetry message", "error", err)
	}
}

func (z *ZoneChecker) Process(ctx context.Context, payload []byte) error {
	sample, ok := decodeSample(payload, z.logger)
	if !ok {
		return nil
	}

	zones, err := z.repo.BreachedZones(ctx, sample.Longitude, sample.Latitude)
	if err != nil {
		return fmt.Errorf("check nofly zones for %s: %w", sample.DroneID, err)
	}

	entered, exited := z.diffZones(sample, zones)
	for _, zone := range entered {
		z.logger.Error(
			fmt.Sprintf("ALERT: Drone %s breached No-Fly Zone %s!", sample.DroneID, zone.Name),
			"drone_id", sample.DroneID,
			"zone_id", zone.ID,
			"zone_name", zone.Name,
			"latitude", sample.Latitude,
			"longitude", sample.Longitude,
			"altitude", sample.Altitude,
		)
		if err := z.alerts.PublishAlert(ctx, telemetry.ZoneBreach{Zone: zone, Sample: sample}); err != nil {
			z.logger.Error("publish breach alert", "drone_id", sample.DroneID, "zone_id", zone.ID, "error", err)
		}
	}
	for _, zone := range exited {
		z.logger.Info("drone left no-fly zone",
			"drone_id", sample.DroneID,
			"zone_id", zone.ID,
			"zone_name", zone.Name,
		)
	}
	return nil
}

func (z *ZoneChecker) diffZones(sample telemetry.Sample, current []telemetry.NoFlyZone) (entered, exited []telemetry.NoFlyZone) {
	z.mu.Lock()
	defer z.mu.Unlock()

	now := time.Now()
	z.pruneLocked(now)

	st, known := z.state[sample.DroneID]
	if !known {
		st = &droneZoneState{zones: make(map[telemetry.ZoneID]string)}
		z.state[sample.DroneID] = st
	}
	if known && !sample.Timestamp.After(st.lastTimestamp) {
		st.lastSeen = now
		return nil, nil
	}

	currentSet := make(map[telemetry.ZoneID]string, len(current))
	for _, zone := range current {
		currentSet[zone.ID] = zone.Name
		if _, in := st.zones[zone.ID]; !in {
			entered = append(entered, zone)
		}
	}
	for id, name := range st.zones {
		if _, in := currentSet[id]; !in {
			exited = append(exited, telemetry.NoFlyZone{ID: id, Name: name})
		}
	}

	st.zones = currentSet
	st.lastTimestamp = sample.Timestamp
	st.lastSeen = now
	return entered, exited
}

func (z *ZoneChecker) pruneLocked(now time.Time) {
	if now.Sub(z.lastPrune) < pruneStateEvery {
		return
	}
	z.lastPrune = now
	for id, st := range z.state {
		if now.Sub(st.lastSeen) > staleDroneStateAfter {
			delete(z.state, id)
		}
	}
}

func decodeSample(payload []byte, logger *slog.Logger) (telemetry.Sample, bool) {
	var pb telemetryv1.DroneTelemetry
	if err := proto.Unmarshal(payload, &pb); err != nil {
		logger.Error("unmarshal telemetry", "error", err)
		return telemetry.Sample{}, false
	}
	return telemetry.Sample{
		DroneID:           telemetry.DroneID(pb.GetDroneId()),
		Timestamp:         pb.GetTimestamp().AsTime(),
		Latitude:          pb.GetLatitude(),
		Longitude:         pb.GetLongitude(),
		Altitude:          pb.GetAltitude(),
		Speed:             pb.GetSpeed(),
		BatteryPercentage: pb.GetBatteryPercentage(),
	}, true
}
