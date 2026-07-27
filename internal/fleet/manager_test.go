package fleet

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type memStore struct {
	drones   map[string]Drone
	missions map[string]Mission
}

func newMemStore() *memStore {
	return &memStore{drones: map[string]Drone{}, missions: map[string]Mission{}}
}

func (s *memStore) ListDrones(context.Context) ([]Drone, error) {
	out := make([]Drone, 0, len(s.drones))
	for _, d := range s.drones {
		out = append(out, d)
	}
	return out, nil
}
func (s *memStore) SaveDrone(_ context.Context, d Drone) error     { s.drones[d.ID] = d; return nil }
func (s *memStore) DeleteDrone(_ context.Context, id string) error { delete(s.drones, id); return nil }
func (s *memStore) ListMissions(context.Context) ([]Mission, error) {
	out := make([]Mission, 0, len(s.missions))
	for _, m := range s.missions {
		out = append(out, m)
	}
	return out, nil
}
func (s *memStore) SaveMission(_ context.Context, m Mission) error { s.missions[m.ID] = m; return nil }
func (s *memStore) DeleteMission(_ context.Context, id string) error {
	delete(s.missions, id)
	return nil
}

func newManager() *Manager {
	return NewManager(newMemStore(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func mustDrone(t *testing.T, m *Manager, id string, lat, lon float64) {
	t.Helper()
	if _, err := m.AddDrone(context.Background(), Drone{ID: id, Model: "quad", Base: Waypoint{lat, lon}}); err != nil {
		t.Fatalf("AddDrone: %v", err)
	}
}

func droneState(m *Manager, id string) Drone {
	for _, d := range m.Snapshot().Drones {
		if d.ID == id {
			return d
		}
	}
	return Drone{}
}

func missionState(m *Manager, id string) Mission {
	for _, ms := range m.Snapshot().Missions {
		if ms.ID == id {
			return ms
		}
	}
	return Mission{}
}

func TestAddDroneDefaults(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	d := droneState(m, "uav-1")
	if d.Status != StatusIdle || d.Battery != fullBattery || d.Latitude != 50 || d.Longitude != 30 {
		t.Fatalf("unexpected default drone: %+v", d)
	}
	if _, err := m.AddDrone(context.Background(), Drone{ID: "uav-1", Model: "quad"}); !errors.Is(err, ErrDroneExists) {
		t.Fatalf("duplicate add = %v, want ErrDroneExists", err)
	}
	if _, err := m.AddDrone(context.Background(), Drone{ID: "", Model: ""}); !errors.Is(err, ErrInvalidDrone) {
		t.Fatalf("invalid add = %v, want ErrInvalidDrone", err)
	}

	mustDrone(t, m, "uav-2", 40, 20)
	if d2 := droneState(m, "uav-2"); d2.Base.Latitude != 40 || d2.Longitude != 20 {
		t.Fatalf("second drone base not applied: %+v", d2)
	}
}

func TestMissionValidation(t *testing.T) {
	m := newManager()
	if _, err := m.CreateMission(context.Background(), "m", "ghost", []Waypoint{{50, 30}}); !errors.Is(err, ErrDroneNotFound) {
		t.Fatalf("mission for unknown drone = %v, want ErrDroneNotFound", err)
	}
	mustDrone(t, m, "uav-1", 50, 30)
	if _, err := m.CreateMission(context.Background(), "", "uav-1", nil); !errors.Is(err, ErrInvalidMission) {
		t.Fatalf("empty mission = %v, want ErrInvalidMission", err)
	}
}

func TestLaunchAdvancesAndCompletes(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	mission, err := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{50.02, 30}})
	if err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if _, err := m.Launch(context.Background(), mission.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if droneState(m, "uav-1").Status != StatusEnRoute {
		t.Fatalf("drone not en-route after launch")
	}

	for range 200 {
		m.tick()
		if droneState(m, "uav-1").Status == StatusIdle {
			break
		}
	}
	d := droneState(m, "uav-1")
	if d.Status != StatusIdle {
		t.Fatalf("drone final status = %s, want idle", d.Status)
	}
	if d.Latitude != 50 || d.Longitude != 30 {
		t.Fatalf("drone did not return to base: %+v", d)
	}
	if d.Battery != fullBattery {
		t.Fatalf("drone did not recharge: %v", d.Battery)
	}
	if missionState(m, mission.ID).State != MissionCompleted {
		t.Fatalf("mission state = %s, want completed", missionState(m, mission.ID).State)
	}
}

func TestPauseResumeAbort(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	mission, _ := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{55, 30}})
	if _, err := m.Launch(context.Background(), mission.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, err := m.Pause(context.Background(), mission.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if missionState(m, mission.ID).State != MissionPaused || droneState(m, "uav-1").Status != StatusHolding {
		t.Fatalf("pause did not hold: %+v %+v", missionState(m, mission.ID), droneState(m, "uav-1"))
	}
	if _, err := m.Resume(context.Background(), mission.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if droneState(m, "uav-1").Status != StatusEnRoute {
		t.Fatalf("resume did not restart flight")
	}
	if _, err := m.Abort(context.Background(), mission.ID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if missionState(m, mission.ID).State != MissionAborted || droneState(m, "uav-1").Status != StatusReturning {
		t.Fatalf("abort did not return the drone")
	}
}

func TestRecallAbortsActiveMission(t *testing.T) {
	store := newMemStore()
	m := NewManager(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mustDrone(t, m, "uav-1", 50, 30)
	mission, _ := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{55, 30}})
	if _, err := m.Launch(context.Background(), mission.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if _, err := m.Recall(context.Background(), "uav-1"); err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if droneState(m, "uav-1").Status != StatusReturning {
		t.Fatalf("recall did not set returning")
	}
	if missionState(m, mission.ID).State != MissionAborted {
		t.Fatalf("recall did not abort mission")
	}
	if store.missions[mission.ID].State != MissionAborted {
		t.Fatalf("recall did not persist the aborted mission: %s", store.missions[mission.ID].State)
	}
	if _, err := m.Recall(context.Background(), "ghost"); !errors.Is(err, ErrDroneNotFound) {
		t.Fatalf("recall unknown = %v, want ErrDroneNotFound", err)
	}
}

func TestLowBatteryAutoReturns(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	mission, _ := m.CreateMission(context.Background(), "far", "uav-1", []Waypoint{{80, 30}})
	if _, err := m.Launch(context.Background(), mission.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	aborted := false
	for range 200 {
		m.tick()
		if missionState(m, mission.ID).State == MissionAborted {
			aborted = true
			break
		}
	}
	if !aborted {
		t.Fatalf("mission was not aborted on low battery")
	}
	if s := droneState(m, "uav-1").Status; s != StatusReturning && s != StatusCharging && s != StatusIdle {
		t.Fatalf("drone status after low-battery abort = %s", s)
	}
}

func TestDeleteMissionBlockedWhileActive(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	mission, _ := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{55, 30}})
	if _, err := m.Launch(context.Background(), mission.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := m.DeleteMission(context.Background(), mission.ID); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("delete active mission = %v, want ErrBadTransition", err)
	}
	if err := m.RemoveDrone(context.Background(), "uav-1"); !errors.Is(err, ErrDroneBusy) {
		t.Fatalf("remove busy drone = %v, want ErrDroneBusy", err)
	}
}

func TestRemoveDroneBlockedByPendingMission(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	if _, err := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{55, 30}}); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if err := m.RemoveDrone(context.Background(), "uav-1"); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("remove drone with planned mission = %v, want ErrBadTransition", err)
	}
}

func TestStatsCountsCompletedAndAborted(t *testing.T) {
	m := newManager()
	mustDrone(t, m, "uav-1", 50, 30)
	done, _ := m.CreateMission(context.Background(), "near", "uav-1", []Waypoint{{50.02, 30}})
	if _, err := m.Launch(context.Background(), done.ID); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	for range 200 {
		m.tick()
		if droneState(m, "uav-1").Status == StatusIdle {
			break
		}
	}
	aborted, _ := m.CreateMission(context.Background(), "far", "uav-1", []Waypoint{{55, 30}})
	if _, err := m.Launch(context.Background(), aborted.ID); err != nil {
		t.Fatalf("Launch far: %v", err)
	}
	if _, err := m.Abort(context.Background(), aborted.ID); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	stats := m.Stats()
	if stats.Completed != 1 {
		t.Fatalf("completed = %d, want 1", stats.Completed)
	}
	if stats.Aborted != 1 {
		t.Fatalf("aborted = %d, want 1", stats.Aborted)
	}
}

type blockGuard struct{ restricted []int }

func (g blockGuard) RestrictedWaypoints(context.Context, []Waypoint) ([]int, error) {
	return g.restricted, nil
}

func TestCreateMissionRejectsRestrictedRoute(t *testing.T) {
	m := newManager()
	m.SetZoneGuard(blockGuard{restricted: []int{0}})
	mustDrone(t, m, "uav-1", 50, 30)
	if _, err := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{50.1, 30.1}}); !errors.Is(err, ErrMissionRestricted) {
		t.Fatalf("restricted route = %v, want ErrMissionRestricted", err)
	}

	m.SetZoneGuard(blockGuard{restricted: nil})
	if _, err := m.CreateMission(context.Background(), "recon", "uav-1", []Waypoint{{50.1, 30.1}}); err != nil {
		t.Fatalf("unrestricted route rejected: %v", err)
	}
}

func TestLoadResetsInFlightState(t *testing.T) {
	store := newMemStore()
	store.drones["uav-1"] = Drone{ID: "uav-1", Model: "quad", Status: StatusEnRoute, Base: Waypoint{50, 30}, MissionID: "mission-001"}
	store.missions["mission-001"] = Mission{ID: "mission-001", Name: "recon", DroneID: "uav-1", Waypoints: []Waypoint{{55, 30}}, State: MissionActive, Progress: 2}
	m := NewManager(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d := droneState(m, "uav-1"); d.Status != StatusIdle || d.MissionID != "" {
		t.Fatalf("in-flight drone not reset on load: %+v", d)
	}
	if ms := missionState(m, "mission-001"); ms.State != MissionPlanned || ms.Progress != 0 {
		t.Fatalf("active mission not reset on load: %+v", ms)
	}
	next, _ := m.CreateMission(context.Background(), "x", "uav-1", []Waypoint{{55, 30}})
	if next.ID != "mission-002" {
		t.Fatalf("next mission id = %s, want mission-002 (seq continued)", next.ID)
	}
}
