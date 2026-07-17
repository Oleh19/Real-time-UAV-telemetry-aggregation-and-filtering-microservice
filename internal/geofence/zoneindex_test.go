package geofence_test

import (
	"encoding/json"
	"testing"

	"uavmonitor/internal/geofence"
	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

func squareZone(id telemetry.ZoneID, name string, minLon, minLat, maxLon, maxLat float64, hole bool) postgres.ZoneFeature {
	outer := [][]float64{
		{minLon, minLat}, {maxLon, minLat}, {maxLon, maxLat}, {minLon, maxLat}, {minLon, minLat},
	}
	rings := [][][]float64{outer}
	if hole {
		midLon := (minLon + maxLon) / 2
		midLat := (minLat + maxLat) / 2
		quarterLon := (maxLon - minLon) / 4
		quarterLat := (maxLat - minLat) / 4
		rings = append(rings, [][]float64{
			{midLon - quarterLon, midLat - quarterLat},
			{midLon + quarterLon, midLat - quarterLat},
			{midLon + quarterLon, midLat + quarterLat},
			{midLon - quarterLon, midLat + quarterLat},
			{midLon - quarterLon, midLat - quarterLat},
		})
	}
	geometry, err := json.Marshal(map[string]any{
		"type":        "MultiPolygon",
		"coordinates": [][][][]float64{rings},
	})
	if err != nil {
		panic(err)
	}
	return postgres.ZoneFeature{
		Zone:     telemetry.Zone{ID: id, Name: name},
		Geometry: geometry,
	}
}

func TestZoneIndexContaining(t *testing.T) {
	index, err := geofence.NewZoneIndex([]postgres.ZoneFeature{
		squareZone(1, "West", 20, 45, 25, 50, false),
		squareZone(2, "East", 30, 45, 35, 50, true),
	})
	if err != nil {
		t.Fatalf("NewZoneIndex: %v", err)
	}

	tests := []struct {
		name      string
		longitude float64
		latitude  float64
		want      []telemetry.ZoneID
	}{
		{name: "inside west", longitude: 22, latitude: 47, want: []telemetry.ZoneID{1}},
		{name: "inside east near edge", longitude: 30.5, latitude: 45.5, want: []telemetry.ZoneID{2}},
		{name: "inside east hole", longitude: 32.5, latitude: 47.5, want: nil},
		{name: "outside all", longitude: 27, latitude: 47, want: nil},
		{name: "far outside bounding boxes", longitude: 0, latitude: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zones := index.Containing(tt.longitude, tt.latitude)
			var got []telemetry.ZoneID
			for _, zone := range zones {
				got = append(got, zone.ID)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Containing() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Containing() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestZoneIndexRejectsUnsupportedGeometry(t *testing.T) {
	_, err := geofence.NewZoneIndex([]postgres.ZoneFeature{
		{Zone: telemetry.Zone{ID: 1, Name: "Broken"}, Geometry: json.RawMessage(`{"type":"Point","coordinates":[1,2]}`)},
	})
	if err == nil {
		t.Fatal("expected error for unsupported geometry type")
	}
}
