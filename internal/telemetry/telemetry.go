package telemetry

import (
	"errors"
	"fmt"
	"time"
)

type DroneID string

type ZoneID int64

const (
	MaxFutureDrift = time.Minute
	MaxSampleAge   = 24 * time.Hour
	MinLatitude    = -90.0
	MaxLatitude    = 90.0
	MinLongitude   = -180.0
	MaxLongitude   = 180.0
	MinAltitude    = -500.0
	MaxAltitude    = 50000.0
	MaxSpeed       = 500.0
	MinBattery     = 0
	MaxBattery     = 100
)

type Sample struct {
	DroneID           DroneID
	Timestamp         time.Time
	Latitude          float64
	Longitude         float64
	Altitude          float64
	Speed             float32
	BatteryPercentage int32
}

func (s Sample) Validate() error {
	if s.DroneID == "" {
		return errors.New("drone id must not be empty")
	}
	if !inRange(s.Latitude, MinLatitude, MaxLatitude) {
		return fmt.Errorf("latitude %f out of range [%g, %g]", s.Latitude, MinLatitude, MaxLatitude)
	}
	if !inRange(s.Longitude, MinLongitude, MaxLongitude) {
		return fmt.Errorf("longitude %f out of range [%g, %g]", s.Longitude, MinLongitude, MaxLongitude)
	}
	if !inRange(s.Altitude, MinAltitude, MaxAltitude) {
		return fmt.Errorf("altitude %f out of range [%g, %g]", s.Altitude, MinAltitude, MaxAltitude)
	}
	if !inRange(float64(s.Speed), 0, MaxSpeed) {
		return fmt.Errorf("speed %f out of range [0, %g]", s.Speed, MaxSpeed)
	}
	if s.BatteryPercentage < MinBattery || s.BatteryPercentage > MaxBattery {
		return fmt.Errorf("battery percentage %d out of range [%d, %d]", s.BatteryPercentage, MinBattery, MaxBattery)
	}
	now := time.Now()
	if s.Timestamp.After(now.Add(MaxFutureDrift)) {
		return fmt.Errorf("timestamp %s is too far in the future", s.Timestamp)
	}
	if s.Timestamp.Before(now.Add(-MaxSampleAge)) {
		return fmt.Errorf("timestamp %s is older than %s", s.Timestamp, MaxSampleAge)
	}
	return nil
}

func inRange(v, min, max float64) bool {
	return v >= min && v <= max
}

type NoFlyZone struct {
	ID   ZoneID
	Name string
}

type ZoneBreach struct {
	Zone   NoFlyZone
	Sample Sample
}
