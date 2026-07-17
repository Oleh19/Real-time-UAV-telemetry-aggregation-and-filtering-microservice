package postgres

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

//go:embed oblasts.geojson
var oblastsGeoJSON []byte

const alertZoneBufferMeters = 10000

type oblastFeature struct {
	Properties struct {
		ShapeName string `json:"shapeName"`
	} `json:"properties"`
	Geometry json.RawMessage `json:"geometry"`
}

type oblastFeatureCollection struct {
	Features []oblastFeature `json:"features"`
}

func (r *Repository) SeedOblasts(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var existing int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM oblasts`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count oblasts: %w", err)
	}
	if existing > 0 {
		return 0, nil
	}

	var collection oblastFeatureCollection
	if err := json.Unmarshal(oblastsGeoJSON, &collection); err != nil {
		return 0, fmt.Errorf("parse embedded oblast boundaries: %w", err)
	}
	if len(collection.Features) == 0 {
		return 0, fmt.Errorf("embedded oblast boundaries contain no features")
	}

	inserted := 0
	for _, feature := range collection.Features {
		if feature.Properties.ShapeName == "" {
			return 0, fmt.Errorf("oblast feature %d has no name", inserted)
		}
		_, err := r.pool.Exec(ctx,
			`INSERT INTO oblasts (name, boundary, alert_zone)
			 VALUES (
			     $1,
			     ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($2), 4326)),
			     ST_Multi(ST_Buffer(ST_SetSRID(ST_GeomFromGeoJSON($2), 4326)::geography, $3)::geometry)
			 )
			 ON CONFLICT (name) DO NOTHING`,
			feature.Properties.ShapeName,
			string(feature.Geometry),
			alertZoneBufferMeters,
		)
		if err != nil {
			return 0, fmt.Errorf("insert oblast %s: %w", feature.Properties.ShapeName, err)
		}
		inserted++
	}
	return inserted, nil
}
