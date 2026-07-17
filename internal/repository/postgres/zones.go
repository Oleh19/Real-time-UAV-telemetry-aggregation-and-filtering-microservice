package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/telemetry"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const queryTimeout = 3 * time.Second

func (r *Repository) ListAlertZoneFeatures(ctx context.Context) ([]ZoneFeature, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, ST_AsGeoJSON(alert_zone)
		   FROM oblasts
		  ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query alert zone features: %w", err)
	}
	defer rows.Close()
	return scanZoneFeatures(rows)
}

func (r *Repository) ListZones(ctx context.Context) ([]telemetry.Zone, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT id, name FROM oblasts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query oblasts: %w", err)
	}
	defer rows.Close()

	var zones []telemetry.Zone
	for rows.Next() {
		var z telemetry.Zone
		if err := rows.Scan(&z.ID, &z.Name); err != nil {
			return nil, fmt.Errorf("scan oblast: %w", err)
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate oblasts: %w", err)
	}
	return zones, nil
}

type ZoneFeature struct {
	Zone     telemetry.Zone
	Geometry json.RawMessage
}

func (r *Repository) ListZoneFeatures(ctx context.Context) ([]ZoneFeature, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, ST_AsGeoJSON(boundary)
		   FROM oblasts
		  ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("query zone features: %w", err)
	}
	defer rows.Close()
	return scanZoneFeatures(rows)
}

func scanZoneFeatures(rows pgx.Rows) ([]ZoneFeature, error) {
	var features []ZoneFeature
	for rows.Next() {
		var f ZoneFeature
		var geometry string
		if err := rows.Scan(&f.Zone.ID, &f.Zone.Name, &geometry); err != nil {
			return nil, fmt.Errorf("scan zone feature: %w", err)
		}
		f.Geometry = json.RawMessage(geometry)
		features = append(features, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate zone features: %w", err)
	}
	return features, nil
}

const insertHistorySQL = `INSERT INTO telemetry_history (drone_id, recorded_at, position, altitude, speed, confidence)
	 VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6, $7)
	 ON CONFLICT (drone_id, recorded_at) DO NOTHING`

const batchTimeout = 10 * time.Second

func (r *Repository) SaveHistoryBatch(ctx context.Context, samples []telemetry.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	batch := &pgx.Batch{}
	for _, sample := range samples {
		batch.Queue(insertHistorySQL,
			string(sample.DroneID),
			sample.Timestamp,
			sample.Longitude,
			sample.Latitude,
			sample.Altitude,
			sample.Speed,
			sample.Confidence,
		)
	}

	results := r.pool.SendBatch(ctx, batch)
	for range samples {
		if _, err := results.Exec(); err != nil {
			closeErr := results.Close()
			return fmt.Errorf("insert telemetry history batch: %w", errors.Join(err, closeErr))
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("close telemetry history batch: %w", err)
	}
	return nil
}

func (r *Repository) DeleteHistoryBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	tag, err := r.pool.Exec(ctx,
		`DELETE FROM telemetry_history WHERE recorded_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("delete telemetry history: %w", err)
	}
	return tag.RowsAffected(), nil
}
