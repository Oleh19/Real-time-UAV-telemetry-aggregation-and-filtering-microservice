package natspub

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
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
	if err := ensureStream(ctx, js); err != nil {
		return nil, err
	}
	return js, nil
}

func ensureStream(ctx context.Context, js jetstream.JetStream) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     StreamName,
		Subjects: []string{"drone.>"},
		Storage:  jetstream.FileStorage,
		MaxAge:   streamMaxAge,
	})
	if err != nil {
		return fmt.Errorf("ensure stream %s: %w", StreamName, err)
	}
	return nil
}

type AsyncPublisher struct {
	js     jetstream.JetStream
	logger *slog.Logger
	failed atomic.Int64
}

func NewAsyncPublisher(ctx context.Context, conn *nats.Conn, logger *slog.Logger) (*AsyncPublisher, error) {
	publisher := &AsyncPublisher{logger: logger}
	js, err := jetstream.New(conn,
		jetstream.WithPublishAsyncMaxPending(4096),
		jetstream.WithPublishAsyncErrHandler(func(_ jetstream.JetStream, msg *nats.Msg, err error) {
			publisher.failed.Add(1)
			logger.Error("async publish failed", "subject", msg.Subject, "error", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}
	if err := ensureStream(ctx, js); err != nil {
		return nil, err
	}
	publisher.js = js
	return publisher, nil
}

func (p *AsyncPublisher) Publish(_ context.Context, sample telemetry.Sample) error {
	payload, err := proto.Marshal(toProto(sample))
	if err != nil {
		return fmt.Errorf("marshal telemetry: %w", err)
	}
	if _, err := p.js.PublishAsync(SubjectTelemetry, payload); err != nil {
		return fmt.Errorf("publish to %s: %w", SubjectTelemetry, err)
	}
	return nil
}

func (p *AsyncPublisher) Failed() int64 {
	return p.failed.Load()
}

func (p *AsyncPublisher) Flush(ctx context.Context) error {
	select {
	case <-p.js.PublishAsyncComplete():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("flush pending telemetry: %w", ctx.Err())
	}
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
		DroneId:    string(sample.DroneID),
		Timestamp:  timestamppb.New(sample.Timestamp),
		Latitude:   sample.Latitude,
		Longitude:  sample.Longitude,
		Altitude:   sample.Altitude,
		Speed:      sample.Speed,
		Confidence: sample.Confidence,
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
		Event:     breachEventToProto(breach.Event),
	}
}

func breachEventToProto(event telemetry.BreachEvent) telemetryv1.BreachEvent {
	if event == telemetry.BreachExited {
		return telemetryv1.BreachEvent_BREACH_EVENT_EXITED
	}
	return telemetryv1.BreachEvent_BREACH_EVENT_ENTERED
}
