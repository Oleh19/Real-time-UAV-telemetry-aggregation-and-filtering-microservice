package geofence

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type BreachRepository interface {
	SaveZoneBreach(ctx context.Context, breach telemetry.ZoneBreach) error
}

type BreachJournal struct {
	repo   BreachRepository
	logger *slog.Logger
}

func NewBreachJournal(repo BreachRepository, logger *slog.Logger) *BreachJournal {
	return &BreachJournal{repo: repo, logger: logger}
}

func (j *BreachJournal) Run(ctx context.Context, consumer jetstream.Consumer) error {
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		j.record(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("consume breach alerts for journal: %w", err)
	}
	defer consumeCtx.Stop()

	j.logger.Info("breach journal started")
	<-ctx.Done()
	return nil
}

func (j *BreachJournal) record(ctx context.Context, msg jetstream.Msg) {
	breach, ok := decodeBreach(msg.Data(), j.logger)
	if !ok {
		if err := msg.Term(); err != nil {
			j.logger.Error("terminate malformed breach message", "error", err)
		}
		return
	}
	if err := j.repo.SaveZoneBreach(ctx, breach); err != nil {
		j.logger.Error("save zone breach", "drone_id", breach.Sample.DroneID, "zone_id", breach.Zone.ID, "error", err)
		if nakErr := msg.NakWithDelay(redeliveryDelay); nakErr != nil {
			j.logger.Error("nak breach message", "error", nakErr)
		}
		return
	}
	if err := msg.Ack(); err != nil {
		j.logger.Error("ack breach message", "error", err)
	}
}

func decodeBreach(payload []byte, logger *slog.Logger) (telemetry.ZoneBreach, bool) {
	var pb telemetryv1.ZoneBreach
	if err := proto.Unmarshal(payload, &pb); err != nil {
		logger.Error("unmarshal zone breach", "error", err)
		return telemetry.ZoneBreach{}, false
	}
	event := telemetry.BreachEntered
	if pb.GetEvent() == telemetryv1.BreachEvent_BREACH_EVENT_EXITED {
		event = telemetry.BreachExited
	}
	return telemetry.ZoneBreach{
		Zone: telemetry.Zone{
			ID:   telemetry.ZoneID(pb.GetZoneId()),
			Name: pb.GetZoneName(),
		},
		Sample: telemetry.Sample{
			DroneID:   telemetry.DroneID(pb.GetDroneId()),
			Timestamp: pb.GetTimestamp().AsTime(),
			Latitude:  pb.GetLatitude(),
			Longitude: pb.GetLongitude(),
			Altitude:  pb.GetAltitude(),
		},
		Event: event,
	}, true
}
