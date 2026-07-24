package geofence_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/geofence"
	"uavmonitor/internal/telemetry"
)

type fakeBreachRepo struct {
	mu       sync.Mutex
	breaches []telemetry.ZoneBreach
	err      error
}

func (r *fakeBreachRepo) SaveZoneBreach(_ context.Context, breach telemetry.ZoneBreach) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.breaches = append(r.breaches, breach)
	return nil
}

func (r *fakeBreachRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.breaches)
}

func breachPayload(event telemetryv1.BreachEvent) []byte {
	payload, err := proto.Marshal(&telemetryv1.ZoneBreach{
		DroneId:   "drone-001",
		ZoneId:    7,
		ZoneName:  "Kyiv Oblast",
		Timestamp: timestamppb.New(time.Now()),
		Latitude:  50.45,
		Longitude: 30.52,
		Altitude:  120,
		Event:     event,
	})
	if err != nil {
		panic(err)
	}
	return payload
}

func runJournal(t *testing.T, repo *fakeBreachRepo) *fakeConsumer {
	t.Helper()
	consumer := &fakeConsumer{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := geofence.NewBreachJournal(repo, discardLogger()).Run(ctx, []jetstream.Consumer{consumer}); err != nil {
			t.Errorf("journal run: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	if !eventually(consumer.handlerRegistered) {
		t.Fatal("journal never registered a consumer handler")
	}
	return consumer
}

func TestBreachJournalPersistsAndAcks(t *testing.T) {
	repo := &fakeBreachRepo{}
	consumer := runJournal(t, repo)

	msg := &fakeMsg{data: breachPayload(telemetryv1.BreachEvent_BREACH_EVENT_EXITED)}
	if !consumer.push(msg) {
		t.Fatal("push failed")
	}

	if !eventually(func() bool { return repo.count() == 1 }) {
		t.Fatal("breach was not persisted")
	}
	acked, naked, termed := msg.state()
	if !acked || naked || termed {
		t.Fatalf("message state acked=%v naked=%v termed=%v, want acked only", acked, naked, termed)
	}
	repo.mu.Lock()
	got := repo.breaches[0]
	repo.mu.Unlock()
	if got.Event != telemetry.BreachExited {
		t.Errorf("Event = %s, want %s", got.Event, telemetry.BreachExited)
	}
	if got.Zone.Name != "Kyiv Oblast" || got.Sample.DroneID != "drone-001" {
		t.Errorf("breach = zone %q drone %s, want Kyiv Oblast drone-001", got.Zone.Name, got.Sample.DroneID)
	}
}

func TestBreachJournalDefaultsUnspecifiedEventToEntered(t *testing.T) {
	repo := &fakeBreachRepo{}
	consumer := runJournal(t, repo)

	consumer.push(&fakeMsg{data: breachPayload(telemetryv1.BreachEvent_BREACH_EVENT_UNSPECIFIED)})

	if !eventually(func() bool { return repo.count() == 1 }) {
		t.Fatal("breach was not persisted")
	}
	repo.mu.Lock()
	got := repo.breaches[0].Event
	repo.mu.Unlock()
	if got != telemetry.BreachEntered {
		t.Errorf("Event = %s, want %s", got, telemetry.BreachEntered)
	}
}

func TestBreachJournalTerminatesMalformedMessages(t *testing.T) {
	repo := &fakeBreachRepo{}
	consumer := runJournal(t, repo)

	msg := &fakeMsg{data: []byte{0xff, 0xff, 0xff}}
	consumer.push(msg)

	if !eventually(func() bool { _, _, termed := msg.state(); return termed }) {
		t.Fatal("malformed message was not terminated")
	}
	if repo.count() != 0 {
		t.Errorf("persisted %d breaches from garbage, want 0", repo.count())
	}
}

func TestBreachJournalNaksOnSaveFailure(t *testing.T) {
	repo := &fakeBreachRepo{err: errors.New("database down")}
	consumer := runJournal(t, repo)

	msg := &fakeMsg{data: breachPayload(telemetryv1.BreachEvent_BREACH_EVENT_ENTERED)}
	consumer.push(msg)

	if !eventually(func() bool { _, naked, _ := msg.state(); return naked }) {
		t.Fatal("message was not nacked after save failure")
	}
	acked, _, termed := msg.state()
	if acked || termed {
		t.Fatalf("message state acked=%v termed=%v, want neither", acked, termed)
	}
}
