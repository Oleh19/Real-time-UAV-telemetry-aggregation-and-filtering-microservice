package geofence_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"uavmonitor/internal/geofence"
	"uavmonitor/internal/telemetry"
)

func TestZoneCheckerAlertsOnEnterOnly(t *testing.T) {
	repo := &fakeZoneRepo{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(repo, alerts, discardLogger())
	ctx := context.Background()
	base := time.Now()
	zone := telemetry.NoFlyZone{ID: 7, Name: "Restricted Object Alpha"}

	repo.setZones([]telemetry.NoFlyZone{zone})
	if err := checker.Process(ctx, payloadAt("drone-001", base)); err != nil {
		t.Fatalf("Process(enter): %v", err)
	}
	if got := alerts.count(); got != 1 {
		t.Fatalf("alerts after entering zone = %d, want 1", got)
	}

	if err := checker.Process(ctx, payloadAt("drone-001", base.Add(time.Second))); err != nil {
		t.Fatalf("Process(stay): %v", err)
	}
	if got := alerts.count(); got != 1 {
		t.Fatalf("alerts while staying in zone = %d, want still 1", got)
	}

	repo.setZones(nil)
	if err := checker.Process(ctx, payloadAt("drone-001", base.Add(2*time.Second))); err != nil {
		t.Fatalf("Process(exit): %v", err)
	}
	if got := alerts.count(); got != 1 {
		t.Fatalf("alerts after leaving zone = %d, want still 1", got)
	}

	repo.setZones([]telemetry.NoFlyZone{zone})
	if err := checker.Process(ctx, payloadAt("drone-001", base.Add(3*time.Second))); err != nil {
		t.Fatalf("Process(re-enter): %v", err)
	}
	if got := alerts.count(); got != 2 {
		t.Fatalf("alerts after re-entering zone = %d, want 2", got)
	}

	breach := func() telemetry.ZoneBreach {
		alerts.mu.Lock()
		defer alerts.mu.Unlock()
		return alerts.breaches[0]
	}()
	if breach.Zone.ID != zone.ID || breach.Sample.DroneID != "drone-001" {
		t.Errorf("breach = zone %d drone %s, want zone %d drone drone-001", breach.Zone.ID, breach.Sample.DroneID, zone.ID)
	}
}

func TestZoneCheckerIgnoresOutOfOrderSamples(t *testing.T) {
	repo := &fakeZoneRepo{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(repo, alerts, discardLogger())
	ctx := context.Background()
	base := time.Now()
	zone := telemetry.NoFlyZone{ID: 7, Name: "Alpha"}

	if err := checker.Process(ctx, payloadAt("drone-001", base)); err != nil {
		t.Fatalf("Process(current): %v", err)
	}

	repo.setZones([]telemetry.NoFlyZone{zone})
	if err := checker.Process(ctx, payloadAt("drone-001", base.Add(-time.Second))); err != nil {
		t.Fatalf("Process(stale): %v", err)
	}
	if got := alerts.count(); got != 0 {
		t.Errorf("alerts from stale out-of-order sample = %d, want 0", got)
	}
}

func TestZoneCheckerProcessErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		repo    *fakeZoneRepo
		wantErr bool
	}{
		{
			name:    "garbage payload dropped without error",
			payload: []byte{0xff, 0xff, 0xff, 0xff},
			repo:    &fakeZoneRepo{},
			wantErr: false,
		},
		{
			name:    "zone query failure returned for redelivery",
			payload: payloadAt("drone-001", time.Now()),
			repo:    &fakeZoneRepo{zonesErr: errors.New("query failed")},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alerts := &fakeAlerts{}
			checker := geofence.NewZoneChecker(tt.repo, alerts, discardLogger())
			err := checker.Process(context.Background(), tt.payload)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got := alerts.count(); got != 0 {
				t.Errorf("alerts = %d, want 0", got)
			}
		})
	}
}

func TestZoneCheckerRunAcksMessages(t *testing.T) {
	repo := &fakeZoneRepo{}
	alerts := &fakeAlerts{}
	checker := geofence.NewZoneChecker(repo, alerts, discardLogger())
	consumer := &fakeConsumer{}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := checker.Run(ctx, consumer, 2, 4); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()

	msg := &fakeMsg{data: payloadAt("drone-001", time.Now())}
	if !eventually(2*time.Second, func() bool { return consumer.push(msg) }) {
		t.Fatal("consumer handler was not registered")
	}
	if !eventually(2*time.Second, func() bool { acked, _, _ := msg.state(); return acked }) {
		t.Error("message was not acked")
	}

	badMsg := &fakeMsg{data: payloadAt("drone-002", time.Now())}
	repo.mu.Lock()
	repo.zonesErr = errors.New("query failed")
	repo.mu.Unlock()
	consumer.push(badMsg)
	if !eventually(2*time.Second, func() bool { _, naked, _ := badMsg.state(); return naked }) {
		t.Error("message was not naked on processing failure")
	}

	cancel()
	wg.Wait()
}
