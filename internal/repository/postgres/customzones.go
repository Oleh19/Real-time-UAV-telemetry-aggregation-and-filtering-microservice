package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"uavmonitor/internal/telemetry"
)

const CustomZoneIDOffset telemetry.ZoneID = 1_000_000

var ErrInvalidZoneGeometry = errors.New("zone polygon is not a valid geometry")

func (r *Repository) CreateCustomZone(ctx context.Context, name string, ring [][2]float64) (telemetry.Zone, error) {
	geometry, err := polygonGeoJSON(ring)
	if err != nil {
		return telemetry.Zone{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var id int64
	err = r.pool.QueryRow(ctx,
		`INSERT INTO custom_zones (name, boundary)
		 SELECT $1, geom
		   FROM (SELECT ST_SetSRID(ST_GeomFromGeoJSON($2), 4326) AS geom) candidate
		  WHERE ST_IsValid(geom)
		 RETURNING id`,
		name, string(geometry),
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return telemetry.Zone{}, ErrInvalidZoneGeometry
		}
		return telemetry.Zone{}, fmt.Errorf("insert custom zone: %w", err)
	}
	return telemetry.Zone{ID: telemetry.ZoneID(id) + CustomZoneIDOffset, Name: name}, nil
}

func (r *Repository) DeleteCustomZone(ctx context.Context, id telemetry.ZoneID) (bool, error) {
	if id <= CustomZoneIDOffset {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`DELETE FROM custom_zones WHERE id = $1`,
		int64(id-CustomZoneIDOffset),
	)
	if err != nil {
		return false, fmt.Errorf("delete custom zone: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repository) ListCustomZoneFeatures(ctx context.Context) ([]ZoneFeature, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, ST_AsGeoJSON(boundary)
		   FROM custom_zones
		  ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query custom zone features: %w", err)
	}
	defer rows.Close()

	features, err := scanZoneFeatures(rows)
	if err != nil {
		return nil, err
	}
	for n := range features {
		features[n].Zone.ID += CustomZoneIDOffset
	}
	return features, nil
}

func polygonGeoJSON(ring [][2]float64) (json.RawMessage, error) {
	if len(ring) < 3 {
		return nil, fmt.Errorf("%w: a polygon needs at least 3 points", ErrInvalidZoneGeometry)
	}
	for _, point := range ring {
		if point[0] < telemetry.MinLongitude || point[0] > telemetry.MaxLongitude ||
			point[1] < telemetry.MinLatitude || point[1] > telemetry.MaxLatitude {
			return nil, fmt.Errorf("%w: point [%g, %g] is out of range", ErrInvalidZoneGeometry, point[0], point[1])
		}
	}
	closed := ring
	if ring[0] != ring[len(ring)-1] {
		closed = append(append([][2]float64{}, ring...), ring[0])
	}
	geometry, err := json.Marshal(map[string]any{
		"type":        "Polygon",
		"coordinates": [][][2]float64{closed},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal polygon geometry: %w", err)
	}
	return geometry, nil
}
