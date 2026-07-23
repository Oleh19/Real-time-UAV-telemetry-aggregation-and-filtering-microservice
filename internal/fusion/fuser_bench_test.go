package fusion_test

import (
	"fmt"
	"testing"
	"time"

	"uavmonitor/internal/fusion"
	"uavmonitor/internal/telemetry"
)

func BenchmarkResolveManyTracksConcurrent(b *testing.B) {
	fuser := fusion.NewFuser(fusion.DefaultConfig())
	const tracks = 5000
	base := time.Now()
	for n := range tracks {
		lat := 44.5 + float64(n%50)*0.15
		lon := 22.5 + float64(n%50)*0.35
		fuser.Resolve(telemetry.Sample{
			DroneID:   telemetry.DroneID(fmt.Sprintf("s1-t%d", n)),
			StationID: "station-01",
			Timestamp: base,
			Latitude:  lat,
			Longitude: lon,
		})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		n := 0
		for pb.Next() {
			idx := n % tracks
			n++
			lat := 44.5 + float64(idx%50)*0.15
			lon := 22.5 + float64(idx%50)*0.35
			fuser.Resolve(telemetry.Sample{
				DroneID:   telemetry.DroneID(fmt.Sprintf("s1-t%d", idx)),
				StationID: "station-01",
				Timestamp: base,
				Latitude:  lat,
				Longitude: lon,
			})
		}
	})
}
