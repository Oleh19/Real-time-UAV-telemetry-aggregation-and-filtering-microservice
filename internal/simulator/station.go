package simulator

import (
	"fmt"
	"math/rand/v2"

	"uavmonitor/gen/telemetryv1"
)

const (
	metersPerDegree    = 111320.0
	altitudeNoiseScale = 0.3
	confidenceJitter   = 8
)

type Station struct {
	id    string
	noise float64
	rng   *rand.Rand
}

func NewStation(index int, noiseMeters int, rng *rand.Rand) *Station {
	return &Station{
		id:    fmt.Sprintf("station-%02d", index),
		noise: float64(noiseMeters),
		rng:   rng,
	}
}

func (s *Station) ID() string {
	return s.id
}

func (s *Station) Observe(truth *telemetryv1.DroneTelemetry) *telemetryv1.DroneTelemetry {
	confidence := truth.GetConfidence() + int32(s.rng.IntN(2*confidenceJitter+1)) - confidenceJitter
	if confidence < 1 {
		confidence = 1
	}
	if confidence > 100 {
		confidence = 100
	}
	return &telemetryv1.DroneTelemetry{
		DroneId:    fmt.Sprintf("%s/%s", s.id, truth.GetDroneId()),
		StationId:  s.id,
		Timestamp:  truth.GetTimestamp(),
		Latitude:   truth.GetLatitude() + s.rng.NormFloat64()*s.noise/metersPerDegree,
		Longitude:  truth.GetLongitude() + s.rng.NormFloat64()*s.noise/metersPerDegree,
		Altitude:   truth.GetAltitude() + s.rng.NormFloat64()*s.noise*altitudeNoiseScale,
		Speed:      truth.GetSpeed(),
		Confidence: confidence,
	}
}
