package fleet

type DroneStatus string

const (
	StatusIdle        DroneStatus = "idle"
	StatusEnRoute     DroneStatus = "en-route"
	StatusHolding     DroneStatus = "holding"
	StatusReturning   DroneStatus = "returning"
	StatusCharging    DroneStatus = "charging"
	StatusMaintenance DroneStatus = "maintenance"
	StatusOffline     DroneStatus = "offline"
)

type MissionState string

const (
	MissionPlanned   MissionState = "planned"
	MissionActive    MissionState = "active"
	MissionPaused    MissionState = "paused"
	MissionCompleted MissionState = "completed"
	MissionAborted   MissionState = "aborted"
)

type Waypoint struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Drone struct {
	ID        string      `json:"id"`
	Model     string      `json:"model"`
	Status    DroneStatus `json:"status"`
	Battery   float64     `json:"battery"`
	Base      Waypoint    `json:"base"`
	Latitude  float64     `json:"latitude"`
	Longitude float64     `json:"longitude"`
	Firmware  string      `json:"firmware"`
	MissionID string      `json:"missionId,omitempty"`
}

type Mission struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	DroneID   string       `json:"droneId"`
	Waypoints []Waypoint   `json:"waypoints"`
	State     MissionState `json:"state"`
	Progress  int          `json:"progress"`
}

type FleetSnapshot struct {
	Drones   []Drone   `json:"drones"`
	Missions []Mission `json:"missions"`
}

func (s DroneStatus) InFlight() bool {
	return s == StatusEnRoute || s == StatusHolding || s == StatusReturning
}
