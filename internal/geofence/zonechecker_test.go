package geofence_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/geofence"
	"uavmonitor/internal/telemetry"
)

func TestZoneCheckerPublishesEnterAndExitEvents(t *testing.T) {
	locator := &fakeLocator{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(locator, alerts, discardLogger())
	ctx := context.Background()
	base := time.Now()
	zone := telemetry.Zone{ID: 7, Name: "Kyiv Oblast"}

	locator.setZones([]telemetry.Zone{zone})
	checker.Process(ctx, payloadAt("drone-001", base))
	if got := alerts.count(); got != 1 {
		t.Fatalf("alerts after entering zone = %d, want 1", got)
	}

	checker.Process(ctx, payloadAt("drone-001", base.Add(time.Second)))
	if got := alerts.count(); got != 1 {
		t.Fatalf("alerts while staying in zone = %d, want still 1", got)
	}

	locator.setZones(nil)
	checker.Process(ctx, payloadAt("drone-001", base.Add(2*time.Second)))
	if got := alerts.count(); got != 2 {
		t.Fatalf("alerts after leaving zone = %d, want 2 (enter + exit)", got)
	}

	locator.setZones([]telemetry.Zone{zone})
	checker.Process(ctx, payloadAt("drone-001", base.Add(3*time.Second)))
	if got := alerts.count(); got != 3 {
		t.Fatalf("alerts after re-entering zone = %d, want 3", got)
	}

	breaches := func() []telemetry.ZoneBreach {
		alerts.mu.Lock()
		defer alerts.mu.Unlock()
		return append([]telemetry.ZoneBreach(nil), alerts.breaches...)
	}()
	wantEvents := []telemetry.BreachEvent{telemetry.BreachEntered, telemetry.BreachExited, telemetry.BreachEntered}
	for n, want := range wantEvents {
		if breaches[n].Event != want {
			t.Errorf("breach[%d].Event = %s, want %s", n, breaches[n].Event, want)
		}
	}
	if breaches[0].Zone.ID != zone.ID || breaches[0].Sample.DroneID != "drone-001" {
		t.Errorf("breach = zone %d drone %s, want zone %d drone drone-001", breaches[0].Zone.ID, breaches[0].Sample.DroneID, zone.ID)
	}
}

func TestZoneCheckerActiveAlarms(t *testing.T) {
	locator := &fakeLocator{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(locator, alerts, discardLogger())
	ctx := context.Background()
	base := time.Now()
	kyiv := telemetry.Zone{ID: 1, Name: "Kyiv Oblast"}
	lviv := telemetry.Zone{ID: 2, Name: "Lviv Oblast"}

	if len(checker.ActiveAlarms()) != 0 {
		t.Fatal("expected no alarms initially")
	}

	locator.setZones([]telemetry.Zone{kyiv})
	checker.Process(ctx, payloadAt("drone-001", base))
	locator.setZones([]telemetry.Zone{kyiv, lviv})
	checker.Process(ctx, payloadAt("drone-002", base))

	alarms := checker.ActiveAlarms()
	if alarms[kyiv.ID] != 2 {
		t.Errorf("alarms[kyiv] = %d, want 2 drones", alarms[kyiv.ID])
	}
	if alarms[lviv.ID] != 1 {
		t.Errorf("alarms[lviv] = %d, want 1 drone", alarms[lviv.ID])
	}

	locator.setZones(nil)
	checker.Process(ctx, payloadAt("drone-001", base.Add(time.Second)))
	alarms = checker.ActiveAlarms()
	if alarms[kyiv.ID] != 1 {
		t.Errorf("alarms[kyiv] after drone-001 left = %d, want 1", alarms[kyiv.ID])
	}
}

func TestZoneCheckerIgnoresOutOfOrderSamples(t *testing.T) {
	locator := &fakeLocator{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(locator, alerts, discardLogger())
	ctx := context.Background()
	base := time.Now()
	zone := telemetry.Zone{ID: 7, Name: "Kyiv Oblast"}

	checker.Process(ctx, payloadAt("drone-001", base))

	locator.setZones([]telemetry.Zone{zone})
	checker.Process(ctx, payloadAt("drone-001", base.Add(-time.Second)))
	if got := alerts.count(); got != 0 {
		t.Errorf("alerts from stale out-of-order sample = %d, want 0", got)
	}
}

func TestZoneCheckerIgnoresGarbagePayload(t *testing.T) {
	locator := &fakeLocator{zones: []telemetry.Zone{{ID: 7, Name: "Kyiv Oblast"}}}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(locator, alerts, discardLogger())

	checker.Process(context.Background(), []byte{0xff, 0xff, 0xff, 0xff})
	if got := alerts.count(); got != 0 {
		t.Errorf("alerts from garbage payload = %d, want 0", got)
	}
}

func TestZoneCheckerRunAcksMessages(t *testing.T) {
	locator := &fakeLocator{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(locator, alerts, discardLogger())
	consumer := &fakeConsumer{}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := checker.Run(ctx, []jetstream.Consumer{consumer}, 2, 4); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	msg := &fakeMsg{data: payloadAt("drone-001", time.Now())}
	if !eventually(func() bool { return consumer.push(msg) }) {
		t.Fatal("consumer handler was not registered")
	}
	if !eventually(func() bool { acked, _, _ := msg.state(); return acked }) {
		t.Error("message was not acked")
	}

	cancel()
	wg.Wait()
}
