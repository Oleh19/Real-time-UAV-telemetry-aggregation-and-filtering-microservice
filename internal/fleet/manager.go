package fleet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrDroneNotFound   = errors.New("drone not found")
	ErrMissionNotFound = errors.New("mission not found")
	ErrDroneExists     = errors.New("drone already exists")
	ErrDroneBusy       = errors.New("drone is airborne")
	ErrInvalidDrone    = errors.New("drone id and model are required")
	ErrInvalidMission  = errors.New("mission needs a name, an existing drone and at least one waypoint")
	ErrBatteryLow      = errors.New("battery too low to launch")
	ErrBadTransition   = errors.New("operation not allowed in the current state")
)

const (
	stepDegrees   = 0.01
	drainPerTick  = 1.5
	hoverDrain    = 0.3
	chargePerTick = 5.0
	lowBattery    = 20.0
	fullBattery   = 100.0
)

type Store interface {
	ListDrones(ctx context.Context) ([]Drone, error)
	SaveDrone(ctx context.Context, drone Drone) error
	DeleteDrone(ctx context.Context, id string) error
	ListMissions(ctx context.Context) ([]Mission, error)
	SaveMission(ctx context.Context, mission Mission) error
	DeleteMission(ctx context.Context, id string) error
}

type Manager struct {
	store    Store
	logger   *slog.Logger
	mu       sync.Mutex
	drones   map[string]*Drone
	missions map[string]*Mission
	nextID   atomic.Int64
}

func NewManager(store Store, logger *slog.Logger) *Manager {
	return &Manager{
		store:    store,
		logger:   logger,
		drones:   make(map[string]*Drone),
		missions: make(map[string]*Mission),
	}
}

func (m *Manager) Load(ctx context.Context) error {
	drones, err := m.store.ListDrones(ctx)
	if err != nil {
		return fmt.Errorf("load drones: %w", err)
	}
	missions, err := m.store.ListMissions(ctx)
	if err != nil {
		return fmt.Errorf("load missions: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var maxSeq int64
	for _, d := range drones {
		drone := d
		if drone.Status.InFlight() {
			drone.Status = StatusIdle
			drone.MissionID = ""
		}
		drone.Latitude, drone.Longitude = drone.Base.Latitude, drone.Base.Longitude
		m.drones[drone.ID] = &drone
	}
	for _, ms := range missions {
		mission := ms
		if mission.State == MissionActive || mission.State == MissionPaused {
			mission.State = MissionPlanned
			mission.Progress = 0
		}
		m.missions[mission.ID] = &mission
		if seq := missionSeq(mission.ID); seq > maxSeq {
			maxSeq = seq
		}
	}
	m.nextID.Store(maxSeq)
	return nil
}

func (m *Manager) Snapshot() FleetSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() FleetSnapshot {
	drones := make([]Drone, 0, len(m.drones))
	for _, d := range m.drones {
		drones = append(drones, *d)
	}
	slices.SortFunc(drones, func(a, b Drone) int { return cmp.Compare(a.ID, b.ID) })
	missions := make([]Mission, 0, len(m.missions))
	for _, ms := range m.missions {
		missions = append(missions, *ms)
	}
	slices.SortFunc(missions, func(a, b Mission) int { return cmp.Compare(a.ID, b.ID) })
	return FleetSnapshot{Drones: drones, Missions: missions}
}

func (m *Manager) AddDrone(ctx context.Context, drone Drone) (Drone, error) {
	if drone.ID == "" || drone.Model == "" {
		return Drone{}, ErrInvalidDrone
	}
	drone.Status = StatusIdle
	drone.Battery = fullBattery
	drone.Latitude, drone.Longitude = drone.Base.Latitude, drone.Base.Longitude
	drone.MissionID = ""

	m.mu.Lock()
	if _, exists := m.drones[drone.ID]; exists {
		m.mu.Unlock()
		return Drone{}, ErrDroneExists
	}
	stored := drone
	m.drones[drone.ID] = &stored
	m.mu.Unlock()

	if err := m.store.SaveDrone(ctx, drone); err != nil {
		m.mu.Lock()
		delete(m.drones, drone.ID)
		m.mu.Unlock()
		return Drone{}, fmt.Errorf("persist drone: %w", err)
	}
	return drone, nil
}

func (m *Manager) RemoveDrone(ctx context.Context, id string) error {
	m.mu.Lock()
	drone, ok := m.drones[id]
	if !ok {
		m.mu.Unlock()
		return ErrDroneNotFound
	}
	if drone.Status.InFlight() {
		m.mu.Unlock()
		return ErrDroneBusy
	}
	delete(m.drones, id)
	m.mu.Unlock()
	if err := m.store.DeleteDrone(ctx, id); err != nil {
		return fmt.Errorf("delete drone: %w", err)
	}
	return nil
}

func (m *Manager) CreateMission(ctx context.Context, name, droneID string, waypoints []Waypoint) (Mission, error) {
	if name == "" || droneID == "" || len(waypoints) == 0 {
		return Mission{}, ErrInvalidMission
	}
	m.mu.Lock()
	if _, ok := m.drones[droneID]; !ok {
		m.mu.Unlock()
		return Mission{}, ErrDroneNotFound
	}
	id := fmt.Sprintf("mission-%03d", m.nextID.Add(1))
	mission := Mission{ID: id, Name: name, DroneID: droneID, Waypoints: waypoints, State: MissionPlanned}
	stored := mission
	m.missions[id] = &stored
	m.mu.Unlock()

	if err := m.store.SaveMission(ctx, mission); err != nil {
		m.mu.Lock()
		delete(m.missions, id)
		m.mu.Unlock()
		return Mission{}, fmt.Errorf("persist mission: %w", err)
	}
	return mission, nil
}

func (m *Manager) DeleteMission(ctx context.Context, id string) error {
	m.mu.Lock()
	mission, ok := m.missions[id]
	if !ok {
		m.mu.Unlock()
		return ErrMissionNotFound
	}
	if mission.State == MissionActive || mission.State == MissionPaused {
		m.mu.Unlock()
		return ErrBadTransition
	}
	delete(m.missions, id)
	m.mu.Unlock()
	if err := m.store.DeleteMission(ctx, id); err != nil {
		return fmt.Errorf("delete mission: %w", err)
	}
	return nil
}

func (m *Manager) Launch(ctx context.Context, missionID string) (Mission, error) {
	return m.mutateMission(ctx, missionID, func(mission *Mission, drone *Drone) error {
		if mission.State != MissionPlanned && mission.State != MissionAborted && mission.State != MissionCompleted {
			return ErrBadTransition
		}
		if drone.Status.InFlight() {
			return ErrDroneBusy
		}
		if drone.Battery <= lowBattery {
			return ErrBatteryLow
		}
		mission.State = MissionActive
		mission.Progress = 0
		drone.Status = StatusEnRoute
		drone.MissionID = mission.ID
		return nil
	})
}

func (m *Manager) Pause(ctx context.Context, missionID string) (Mission, error) {
	return m.mutateMission(ctx, missionID, func(mission *Mission, drone *Drone) error {
		if mission.State != MissionActive {
			return ErrBadTransition
		}
		mission.State = MissionPaused
		if drone.Status == StatusEnRoute {
			drone.Status = StatusHolding
		}
		return nil
	})
}

func (m *Manager) Resume(ctx context.Context, missionID string) (Mission, error) {
	return m.mutateMission(ctx, missionID, func(mission *Mission, drone *Drone) error {
		if mission.State != MissionPaused {
			return ErrBadTransition
		}
		mission.State = MissionActive
		if drone.Status == StatusHolding {
			drone.Status = StatusEnRoute
		}
		return nil
	})
}

func (m *Manager) Abort(ctx context.Context, missionID string) (Mission, error) {
	return m.mutateMission(ctx, missionID, func(mission *Mission, drone *Drone) error {
		if mission.State != MissionActive && mission.State != MissionPaused {
			return ErrBadTransition
		}
		mission.State = MissionAborted
		drone.Status = StatusReturning
		return nil
	})
}

func (m *Manager) mutateMission(ctx context.Context, missionID string, apply func(*Mission, *Drone) error) (Mission, error) {
	m.mu.Lock()
	mission, ok := m.missions[missionID]
	if !ok {
		m.mu.Unlock()
		return Mission{}, ErrMissionNotFound
	}
	drone, ok := m.drones[mission.DroneID]
	if !ok {
		m.mu.Unlock()
		return Mission{}, ErrDroneNotFound
	}
	if err := apply(mission, drone); err != nil {
		m.mu.Unlock()
		return Mission{}, err
	}
	result := *mission
	m.mu.Unlock()

	if err := m.store.SaveMission(ctx, result); err != nil {
		return Mission{}, fmt.Errorf("persist mission: %w", err)
	}
	return result, nil
}

func (m *Manager) Recall(droneID string) (Drone, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	drone, ok := m.drones[droneID]
	if !ok {
		return Drone{}, ErrDroneNotFound
	}
	if !drone.Status.InFlight() {
		return Drone{}, ErrBadTransition
	}
	if mission, ok := m.missions[drone.MissionID]; ok && (mission.State == MissionActive || mission.State == MissionPaused) {
		mission.State = MissionAborted
	}
	drone.Status = StatusReturning
	return *drone, nil
}

func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, mission := range m.tick() {
				if err := m.store.SaveMission(ctx, mission); err != nil {
					m.logger.Error("persist mission state", "mission_id", mission.ID, "error", err)
				}
			}
		}
	}
}

func (m *Manager) tick() []Mission {
	m.mu.Lock()
	defer m.mu.Unlock()
	var changed []Mission
	for _, drone := range m.drones {
		switch drone.Status {
		case StatusEnRoute:
			if mission := m.missions[drone.MissionID]; mission != nil {
				if done := m.advanceMission(drone, mission); done {
					changed = append(changed, *mission)
				}
			} else {
				drone.Status = StatusReturning
			}
		case StatusHolding:
			drone.Battery = clampBattery(drone.Battery - hoverDrain)
			if drone.Battery <= lowBattery {
				if mission := m.missions[drone.MissionID]; mission != nil && mission.State == MissionPaused {
					mission.State = MissionAborted
					changed = append(changed, *mission)
				}
				drone.Status = StatusReturning
			}
		case StatusReturning:
			m.returnHome(drone)
		case StatusCharging:
			drone.Battery = clampBattery(drone.Battery + chargePerTick)
			if drone.Battery >= fullBattery {
				drone.Battery = fullBattery
				drone.Status = StatusIdle
			}
		}
	}
	return changed
}

func (m *Manager) advanceMission(drone *Drone, mission *Mission) (missionChanged bool) {
	drone.Battery = clampBattery(drone.Battery - drainPerTick)
	if drone.Battery <= lowBattery {
		mission.State = MissionAborted
		drone.Status = StatusReturning
		return true
	}
	if mission.Progress >= len(mission.Waypoints) {
		mission.State = MissionCompleted
		drone.Status = StatusReturning
		return true
	}
	target := mission.Waypoints[mission.Progress]
	lat, lon, reached := moveToward(drone.Latitude, drone.Longitude, target.Latitude, target.Longitude)
	drone.Latitude, drone.Longitude = lat, lon
	if reached {
		mission.Progress++
		if mission.Progress >= len(mission.Waypoints) {
			mission.State = MissionCompleted
			drone.Status = StatusReturning
			return true
		}
	}
	return false
}

func (m *Manager) returnHome(drone *Drone) {
	drone.Battery = clampBattery(drone.Battery - hoverDrain)
	lat, lon, reached := moveToward(drone.Latitude, drone.Longitude, drone.Base.Latitude, drone.Base.Longitude)
	drone.Latitude, drone.Longitude = lat, lon
	if reached {
		drone.MissionID = ""
		if drone.Battery >= fullBattery {
			drone.Status = StatusIdle
		} else {
			drone.Status = StatusCharging
		}
	}
}

func moveToward(lat, lon, targetLat, targetLon float64) (nextLat, nextLon float64, reached bool) {
	dLat := targetLat - lat
	dLon := targetLon - lon
	dist := math.Hypot(dLat, dLon)
	if dist <= stepDegrees || dist == 0 {
		return targetLat, targetLon, true
	}
	ratio := stepDegrees / dist
	return lat + dLat*ratio, lon + dLon*ratio, false
}

func clampBattery(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > fullBattery {
		return fullBattery
	}
	return v
}

func missionSeq(id string) int64 {
	var n int64
	if _, err := fmt.Sscanf(id, "mission-%d", &n); err != nil {
		return 0
	}
	return n
}
