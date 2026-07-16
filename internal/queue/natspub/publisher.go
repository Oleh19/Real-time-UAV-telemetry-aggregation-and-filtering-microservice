package natspub

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

const (
	StreamName       = "DRONE"
	SubjectTelemetry = "drone.telemetry"
	SubjectAlerts    = "drone.alerts"
	streamMaxAge     = 24 * time.Hour
)

func NewJetStream(ctx context.Context, conn *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(conn)
	if err != nil {
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"drone.>"},
		Storage:  jetstream.FileStorage,
		MaxAge:   streamMaxAge,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure stream %s: %w", StreamName, err)
	}
	return js, nil
}

func Connect(url string, logger *slog.Logger) (*nats.Conn, error) {
	conn, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.Timeout(5*time.Second),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			if sub != nil {
				logger.Error("nats async error", "subject", sub.Subject, "error", err)
				return
			}
			logger.Error("nats async error", "error", err)
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				logger.Warn("nats disconnected", "error", err)
			}
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) {
			logger.Info("nats reconnected", "url", conn.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to nats at %s: %w", url, err)
	}
	return conn, nil
}

type Publisher struct {
	js jetstream.JetStream
}

func NewPublisher(js jetstream.JetStream) *Publisher {
	return &Publisher{js: js}
}

func (p *Publisher) Publish(ctx context.Context, sample telemetry.Sample) error {
	payload, err := proto.Marshal(toProto(sample))
	if err != nil {
		return fmt.Errorf("marshal telemetry: %w", err)
	}
	if _, err := p.js.Publish(ctx, SubjectTelemetry, payload); err != nil {
		return fmt.Errorf("publish to %s: %w", SubjectTelemetry, err)
	}
	return nil
}

func (p *Publisher) PublishAlert(ctx context.Context, breach telemetry.ZoneBreach) error {
	payload, err := proto.Marshal(breachToProto(breach))
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}
	if _, err := p.js.Publish(ctx, SubjectAlerts, payload); err != nil {
		return fmt.Errorf("publish to %s: %w", SubjectAlerts, err)
	}
	return nil
}

func toProto(sample telemetry.Sample) *telemetryv1.DroneTelemetry {
	return &telemetryv1.DroneTelemetry{
		DroneId:           string(sample.DroneID),
		Timestamp:         timestamppb.New(sample.Timestamp),
		Latitude:          sample.Latitude,
		Longitude:         sample.Longitude,
		Altitude:          sample.Altitude,
		Speed:             sample.Speed,
		BatteryPercentage: sample.BatteryPercentage,
	}
}

func breachToProto(breach telemetry.ZoneBreach) *telemetryv1.ZoneBreach {
	return &telemetryv1.ZoneBreach{
		DroneId:   string(breach.Sample.DroneID),
		ZoneId:    int64(breach.Zone.ID),
		ZoneName:  breach.Zone.Name,
		Timestamp: timestamppb.New(breach.Sample.Timestamp),
		Latitude:  breach.Sample.Latitude,
		Longitude: breach.Sample.Longitude,
		Altitude:  breach.Sample.Altitude,
	}
}
