package postgres

import (
	"context"
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

func (r *Repository) BreachedZones(ctx context.Context, longitude, latitude float64) ([]telemetry.NoFlyZone, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, name
		   FROM nofly_zones
		  WHERE ST_Intersects(boundary, ST_SetSRID(ST_MakePoint($1, $2), 4326))`,
		longitude, latitude,
	)
	if err != nil {
		return nil, fmt.Errorf("query nofly zones: %w", err)
	}
	defer rows.Close()

	var zones []telemetry.NoFlyZone
	for rows.Next() {
		var z telemetry.NoFlyZone
		if err := rows.Scan(&z.ID, &z.Name); err != nil {
			return nil, fmt.Errorf("scan nofly zone: %w", err)
		}
		zones = append(zones, z)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nofly zones: %w", err)
	}
	return zones, nil
}

const insertHistorySQL = `INSERT INTO telemetry_history (drone_id, recorded_at, position, altitude, speed, battery_percentage)
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
			sample.BatteryPercentage,
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
