package geofence_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type fakeConsumer struct {
	jetstream.Consumer
	mu      sync.Mutex
	handler jetstream.MessageHandler
}

func (c *fakeConsumer) Consume(handler jetstream.MessageHandler, _ ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
	return fakeConsumeContext{}, nil
}

func (c *fakeConsumer) push(msg jetstream.Msg) bool {
	c.mu.Lock()
	handler := c.handler
	c.mu.Unlock()
	if handler == nil {
		return false
	}
	handler(msg)
	return true
}

func (c *fakeConsumer) handlerRegistered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handler != nil
}

type fakeConsumeContext struct {
	jetstream.ConsumeContext
}

func (fakeConsumeContext) Stop() {}

type fakeMsg struct {
	jetstream.Msg
	data   []byte
	mu     sync.Mutex
	acked  bool
	naked  bool
	termed bool
}

func (m *fakeMsg) Data() []byte {
	return m.data
}

func (m *fakeMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *fakeMsg) NakWithDelay(_ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	return nil
}

func (m *fakeMsg) Term() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termed = true
	return nil
}

func (m *fakeMsg) state() (acked, naked, termed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked, m.naked, m.termed
}

type fakeLocator struct {
	mu    sync.Mutex
	zones []telemetry.Zone
}

func (l *fakeLocator) Containing(_, _ float64) []telemetry.Zone {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.zones
}

func (l *fakeLocator) setZones(zones []telemetry.Zone) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.zones = zones
}

type fakeAlerts struct {
	mu       sync.Mutex
	breaches []telemetry.ZoneBreach
}

func (a *fakeAlerts) PublishAlert(_ context.Context, breach telemetry.ZoneBreach) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.breaches = append(a.breaches, breach)
	return nil
}

func (a *fakeAlerts) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.breaches)
}

type fakeHistoryRepo struct {
	mu      sync.Mutex
	batches [][]telemetry.Sample
	err     error
}

func (r *fakeHistoryRepo) SaveHistoryBatch(_ context.Context, samples []telemetry.Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.batches = append(r.batches, samples)
	return nil
}

func (r *fakeHistoryRepo) batchCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.batches)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func payloadAt(id string, ts time.Time) []byte {
	payload, err := proto.Marshal(&telemetryv1.DroneTelemetry{
		DroneId:   id,
		Timestamp: timestamppb.New(ts),
		Latitude:  50.45,
		Longitude: 30.52,
	})
	if err != nil {
		panic(err)
	}
	return payload
}

func eventually(condition func() bool) bool {
	limit := time.Now().Add(2 * time.Second)
	for time.Now().Before(limit) {
		if condition() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return condition()
}
