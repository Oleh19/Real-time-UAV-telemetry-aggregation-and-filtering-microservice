package geofence

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type stubLocator struct {
	zones []telemetry.Zone
}

func (s *stubLocator) Containing(_, _ float64) []telemetry.Zone {
	return s.zones
}

type stubAlerts struct{}

func (stubAlerts) PublishAlert(_ context.Context, _ telemetry.ZoneBreach) error { return nil }

func samplePayload(id string, ts time.Time) []byte {
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

func TestActiveAlarmsClearWhenDroneGoesSilent(t *testing.T) {
	locator := &stubLocator{zones: []telemetry.Zone{{ID: 7, Name: "Kyiv Oblast"}}}
	checker := NewZoneChecker(locator, stubAlerts{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	clock := time.Unix(1700000000, 0).UTC()
	checker.now = func() time.Time { return clock }

	checker.Process(context.Background(), samplePayload("drone-1", clock))
	if got := checker.ActiveAlarms()[7]; got != 1 {
		t.Fatalf("alarms right after entry = %d, want 1", got)
	}

	clock = clock.Add(alarmActiveWindow + time.Second)
	if got := checker.ActiveAlarms()[7]; got != 0 {
		t.Fatalf("alarms after the drone went silent = %d, want 0", got)
	}
}
