package notify

import (
	"context"
	"fmt"
	"time"

	"uavmonitor/internal/telemetry"
)

type Notification struct {
	DroneID   string    `json:"drone_id"`
	ZoneID    int64     `json:"zone_id"`
	ZoneName  string    `json:"zone_name"`
	Event     string    `json:"event"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Altitude  float64   `json:"altitude"`
	Timestamp time.Time `json:"timestamp"`
}

func FromBreach(breach telemetry.ZoneBreach) Notification {
	return Notification{
		DroneID:   string(breach.Sample.DroneID),
		ZoneID:    int64(breach.Zone.ID),
		ZoneName:  breach.Zone.Name,
		Event:     string(breach.Event),
		Latitude:  breach.Sample.Latitude,
		Longitude: breach.Sample.Longitude,
		Altitude:  breach.Sample.Altitude,
		Timestamp: breach.Sample.Timestamp,
	}
}

func (n Notification) Text() string {
	verb := "entered"
	if n.Event == string(telemetry.BreachExited) {
		verb = "left"
	}
	return fmt.Sprintf(
		"Drone %s %s alert zone %s at %.4f, %.4f (alt %.0f m)",
		n.DroneID, verb, n.ZoneName, n.Latitude, n.Longitude, n.Altitude,
	)
}

type Sink interface {
	Name() string
	Send(ctx context.Context, notification Notification) error
}
