package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/telemetry"
)

const redeliveryDelay = 5 * time.Second

type Dispatcher struct {
	sinks          []Sink
	logger         *slog.Logger
	notifyOnExit   bool
	timeout        time.Duration
	cooldown       *cooldownGate
	now            func() time.Time
	sentTotal      atomic.Int64
	failedTotal    atomic.Int64
	skippedTotal   atomic.Int64
	throttledTotal atomic.Int64
}

func NewDispatcher(sinks []Sink, notifyOnExit bool, cooldown, timeout time.Duration, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		sinks:        sinks,
		logger:       logger,
		notifyOnExit: notifyOnExit,
		timeout:      timeout,
		cooldown:     newCooldownGate(cooldown),
		now:          time.Now,
	}
}

func (d *Dispatcher) SentTotal() int64      { return d.sentTotal.Load() }
func (d *Dispatcher) FailedTotal() int64    { return d.failedTotal.Load() }
func (d *Dispatcher) SkippedTotal() int64   { return d.skippedTotal.Load() }
func (d *Dispatcher) ThrottledTotal() int64 { return d.throttledTotal.Load() }

func (d *Dispatcher) Run(ctx context.Context, consumers []jetstream.Consumer) error {
	stop, err := natspub.ConsumeAll(consumers, func(msg jetstream.Msg) {
		d.handle(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("consume alerts for notifier: %w", err)
	}
	defer stop()

	d.logger.Info("notifier started", "sinks", d.sinkNames())
	<-ctx.Done()
	return nil
}

func (d *Dispatcher) handle(ctx context.Context, msg jetstream.Msg) {
	breach, ok := decodeBreach(msg.Data(), d.logger)
	if !ok {
		if err := msg.Term(); err != nil {
			d.logger.Error("terminate malformed alert", "error", err)
		}
		return
	}
	if breach.Event == telemetry.BreachExited && !d.notifyOnExit {
		d.skippedTotal.Add(1)
		if err := msg.Ack(); err != nil {
			d.logger.Error("ack skipped alert", "error", err)
		}
		return
	}
	if !d.cooldown.allow(cooldownKey(breach), d.now()) {
		d.throttledTotal.Add(1)
		if err := msg.Ack(); err != nil {
			d.logger.Error("ack throttled alert", "error", err)
		}
		return
	}
	if d.dispatch(ctx, FromBreach(breach)) {
		if err := msg.Ack(); err != nil {
			d.logger.Error("ack alert", "error", err)
		}
		return
	}
	if err := msg.NakWithDelay(redeliveryDelay); err != nil {
		d.logger.Error("nak alert", "error", err)
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, notification Notification) bool {
	sendCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var wg sync.WaitGroup
	var failures atomic.Int64
	for _, sink := range d.sinks {
		wg.Add(1)
		go func(sink Sink) {
			defer wg.Done()
			if err := sink.Send(sendCtx, notification); err != nil {
				failures.Add(1)
				d.failedTotal.Add(1)
				d.logger.Error("dispatch notification", "sink", sink.Name(), "drone_id", notification.DroneID, "error", err)
				return
			}
			d.sentTotal.Add(1)
		}(sink)
	}
	wg.Wait()
	return failures.Load() == 0
}

func (d *Dispatcher) sinkNames() []string {
	names := make([]string, 0, len(d.sinks))
	for _, sink := range d.sinks {
		names = append(names, sink.Name())
	}
	return names
}

func cooldownKey(breach telemetry.ZoneBreach) string {
	return fmt.Sprintf("%s|%d|%s", breach.Sample.DroneID, breach.Zone.ID, breach.Event)
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
