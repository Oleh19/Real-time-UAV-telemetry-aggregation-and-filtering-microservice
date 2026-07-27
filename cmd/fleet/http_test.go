package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"uavmonitor/internal/fleet"
)

type fakeFleet struct {
	snapshot   fleet.FleetSnapshot
	launched   string
	created    createMissionRequest
	addErr     error
	launchErr  error
	recalledID string
}

func (f *fakeFleet) Snapshot() fleet.FleetSnapshot { return f.snapshot }
func (f *fakeFleet) Stats() (int, int, int)        { return 1, 0, 0 }
func (f *fakeFleet) AddDrone(_ context.Context, d fleet.Drone) (fleet.Drone, error) {
	if f.addErr != nil {
		return fleet.Drone{}, f.addErr
	}
	return d, nil
}
func (f *fakeFleet) RemoveDrone(context.Context, string) error { return nil }
func (f *fakeFleet) CreateMission(_ context.Context, name, droneID string, wps []fleet.Waypoint) (fleet.Mission, error) {
	f.created = createMissionRequest{Name: name, DroneID: droneID, Waypoints: wps}
	return fleet.Mission{ID: "mission-001", Name: name, DroneID: droneID, Waypoints: wps, State: fleet.MissionPlanned}, nil
}
func (f *fakeFleet) DeleteMission(context.Context, string) error { return nil }
func (f *fakeFleet) Launch(_ context.Context, id string) (fleet.Mission, error) {
	f.launched = id
	if f.launchErr != nil {
		return fleet.Mission{}, f.launchErr
	}
	return fleet.Mission{ID: id, State: fleet.MissionActive}, nil
}
func (f *fakeFleet) Pause(_ context.Context, id string) (fleet.Mission, error) {
	return fleet.Mission{ID: id, State: fleet.MissionPaused}, nil
}
func (f *fakeFleet) Resume(_ context.Context, id string) (fleet.Mission, error) {
	return fleet.Mission{ID: id, State: fleet.MissionActive}, nil
}
func (f *fakeFleet) Abort(_ context.Context, id string) (fleet.Mission, error) {
	return fleet.Mission{ID: id, State: fleet.MissionAborted}, nil
}
func (f *fakeFleet) Recall(id string) (fleet.Drone, error) {
	f.recalledID = id
	return fleet.Drone{ID: id, Status: fleet.StatusReturning}, nil
}

func handler(f *fakeFleet) http.Handler {
	return newHTTPHandler(f, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestFleetSnapshotEndpoint(t *testing.T) {
	f := &fakeFleet{snapshot: fleet.FleetSnapshot{Drones: []fleet.Drone{{ID: "uav-1", Status: fleet.StatusIdle}}}}
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fleet", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var snap fleet.FleetSnapshot
	if err := json.NewDecoder(rec.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snap.Drones) != 1 || snap.Drones[0].ID != "uav-1" {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestCreateMissionEndpoint(t *testing.T) {
	f := &fakeFleet{}
	body := `{"name":"recon","droneId":"uav-1","waypoints":[{"latitude":50.1,"longitude":30.1}]}`
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/missions", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if f.created.Name != "recon" || f.created.DroneID != "uav-1" || len(f.created.Waypoints) != 1 {
		t.Fatalf("create not forwarded: %+v", f.created)
	}
}

func TestMissionActionEndpoints(t *testing.T) {
	f := &fakeFleet{}
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/missions/mission-007/launch", nil))
	if rec.Code != http.StatusOK || f.launched != "mission-007" {
		t.Fatalf("launch action failed: code=%d launched=%q", rec.Code, f.launched)
	}

	rec = httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/missions/mission-007/nonsense", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown action = %d, want 404", rec.Code)
	}
}

func TestRecallEndpoint(t *testing.T) {
	f := &fakeFleet{}
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/drones/uav-9/recall", nil))
	if rec.Code != http.StatusOK || f.recalledID != "uav-9" {
		t.Fatalf("recall failed: code=%d id=%q", rec.Code, f.recalledID)
	}
}

func TestAddDroneConflictMapsTo409(t *testing.T) {
	f := &fakeFleet{addErr: fleet.ErrDroneExists}
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/drones", strings.NewReader(`{"id":"uav-1","model":"quad"}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate drone = %d, want 409", rec.Code)
	}
}

func TestLaunchBatteryLowMapsTo409(t *testing.T) {
	f := &fakeFleet{launchErr: fleet.ErrBatteryLow}
	rec := httptest.NewRecorder()
	handler(f).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/fleet/missions/m/launch", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("low battery launch = %d, want 409", rec.Code)
	}
}
