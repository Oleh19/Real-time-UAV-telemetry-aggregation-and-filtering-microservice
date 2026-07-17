package geofence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

type ZoneLocator interface {
	Containing(longitude, latitude float64) []telemetry.Zone
}

type boundingBox struct {
	minLongitude float64
	minLatitude  float64
	maxLongitude float64
	maxLatitude  float64
}

type zoneGeometry struct {
	zone     telemetry.Zone
	polygons [][][][]float64
	bounds   boundingBox
}

type ZoneIndex struct {
	zones []zoneGeometry
}

type geoJSONGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func NewZoneIndex(features []postgres.ZoneFeature) (*ZoneIndex, error) {
	index := &ZoneIndex{zones: make([]zoneGeometry, 0, len(features))}
	for _, feature := range features {
		polygons, err := parsePolygons(feature.Geometry)
		if err != nil {
			return nil, fmt.Errorf("parse geometry of zone %s: %w", feature.Zone.Name, err)
		}
		index.zones = append(index.zones, zoneGeometry{
			zone:     feature.Zone,
			polygons: polygons,
			bounds:   boundsOf(polygons),
		})
	}
	return index, nil
}

func parsePolygons(raw json.RawMessage) ([][][][]float64, error) {
	var geometry geoJSONGeometry
	if err := json.Unmarshal(raw, &geometry); err != nil {
		return nil, err
	}
	switch geometry.Type {
	case "MultiPolygon":
		var polygons [][][][]float64
		if err := json.Unmarshal(geometry.Coordinates, &polygons); err != nil {
			return nil, err
		}
		return polygons, nil
	case "Polygon":
		var polygon [][][]float64
		if err := json.Unmarshal(geometry.Coordinates, &polygon); err != nil {
			return nil, err
		}
		return [][][][]float64{polygon}, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type %q", geometry.Type)
	}
}

func boundsOf(polygons [][][][]float64) boundingBox {
	bounds := boundingBox{
		minLongitude: 180,
		minLatitude:  90,
		maxLongitude: -180,
		maxLatitude:  -90,
	}
	for _, polygon := range polygons {
		for _, ring := range polygon {
			for _, point := range ring {
				bounds.minLongitude = min(bounds.minLongitude, point[0])
				bounds.maxLongitude = max(bounds.maxLongitude, point[0])
				bounds.minLatitude = min(bounds.minLatitude, point[1])
				bounds.maxLatitude = max(bounds.maxLatitude, point[1])
			}
		}
	}
	return bounds
}

func (i *ZoneIndex) Containing(longitude, latitude float64) []telemetry.Zone {
	var zones []telemetry.Zone
	for _, candidate := range i.zones {
		if longitude < candidate.bounds.minLongitude || longitude > candidate.bounds.maxLongitude ||
			latitude < candidate.bounds.minLatitude || latitude > candidate.bounds.maxLatitude {
			continue
		}
		for _, polygon := range candidate.polygons {
			if pointInPolygon(longitude, latitude, polygon) {
				zones = append(zones, candidate.zone)
				break
			}
		}
	}
	return zones
}

func pointInPolygon(longitude, latitude float64, rings [][][]float64) bool {
	inside := false
	for _, ring := range rings {
		for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
			longitudeI, latitudeI := ring[i][0], ring[i][1]
			longitudeJ, latitudeJ := ring[j][0], ring[j][1]
			if (latitudeI > latitude) != (latitudeJ > latitude) &&
				longitude < (longitudeJ-longitudeI)*(latitude-latitudeI)/(latitudeJ-latitudeI)+longitudeI {
				inside = !inside
			}
		}
	}
	return inside
}

type ZoneFeatureSource interface {
	ListAlertZoneFeatures(ctx context.Context) ([]postgres.ZoneFeature, error)
}

type RefreshingZoneIndex struct {
	current atomic.Pointer[ZoneIndex]
}

func NewRefreshingZoneIndex(ctx context.Context, source ZoneFeatureSource) (*RefreshingZoneIndex, error) {
	index, err := loadZoneIndex(ctx, source)
	if err != nil {
		return nil, err
	}
	refreshing := &RefreshingZoneIndex{}
	refreshing.current.Store(index)
	return refreshing, nil
}

func (r *RefreshingZoneIndex) Containing(longitude, latitude float64) []telemetry.Zone {
	return r.current.Load().Containing(longitude, latitude)
}

func (r *RefreshingZoneIndex) Run(ctx context.Context, source ZoneFeatureSource, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			index, err := loadZoneIndex(ctx, source)
			if err != nil {
				logger.Error("refresh zone index", "error", err)
				continue
			}
			r.current.Store(index)
		}
	}
}

func loadZoneIndex(ctx context.Context, source ZoneFeatureSource) (*ZoneIndex, error) {
	features, err := source.ListAlertZoneFeatures(ctx)
	if err != nil {
		return nil, fmt.Errorf("load alert zone features: %w", err)
	}
	return NewZoneIndex(features)
}
