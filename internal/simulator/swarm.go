package simulator

import (
	"fmt"
	"math"
	"math/rand/v2"

	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
)

const (
	minFormationOffsetMeters = 300.0
	maxFormationOffsetMeters = 1800.0
	formationJitterMeters    = 40.0
)

type SwarmMember struct {
	id              string
	offsetLatitude  float64
	offsetLongitude float64
	rng             *rand.Rand
}

func NewSwarmMember(index int, rng *rand.Rand) *SwarmMember {
	offset := minFormationOffsetMeters + rng.Float64()*(maxFormationOffsetMeters-minFormationOffsetMeters)
	angle := rng.Float64() * 2 * math.Pi
	return &SwarmMember{
		id:              fmt.Sprintf("drone-%03d", index),
		offsetLatitude:  offset * math.Cos(angle) / metersPerDegree,
		offsetLongitude: offset * math.Sin(angle) / metersPerDegree,
		rng:             rng,
	}
}

func (m *SwarmMember) ID() string {
	return m.id
}

func (m *SwarmMember) Follow(leader *telemetryv1.DroneTelemetry) *telemetryv1.DroneTelemetry {
	jitterLat := m.rng.NormFloat64() * formationJitterMeters / metersPerDegree
	jitterLon := m.rng.NormFloat64() * formationJitterMeters / metersPerDegree
	return &telemetryv1.DroneTelemetry{
		DroneId:    m.id,
		Timestamp:  timestamppb.New(leader.GetTimestamp().AsTime()),
		Latitude:   leader.GetLatitude() + m.offsetLatitude + jitterLat,
		Longitude:  leader.GetLongitude() + m.offsetLongitude + jitterLon,
		Altitude:   leader.GetAltitude() + m.rng.Float64()*40 - 20,
		Speed:      leader.GetSpeed(),
		Confidence: leader.GetConfidence(),
	}
}
