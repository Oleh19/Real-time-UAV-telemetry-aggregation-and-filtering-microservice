package postgres

import (
	"context"
	"fmt"
	"time"

	"uavmonitor/internal/telemetry"
)

const (
	MaxHistoryPoints = 5000
	MaxReplayPoints  = 20000
)

func (r *Repository) ListHistory(ctx context.Context, droneID telemetry.DroneID, from, to time.Time, limit int) ([]telemetry.Sample, error) {
	if limit <= 0 || limit > MaxHistoryPoints {
		limit = MaxHistoryPoints
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT drone_id, recorded_at, ST_Y(position), ST_X(position), altitude, speed, confidence
		   FROM telemetry_history
		  WHERE drone_id = $1 AND recorded_at >= $2 AND recorded_at <= $3
		  ORDER BY recorded_at
		  LIMIT $4`,
		string(droneID), from, to, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query telemetry history: %w", err)
	}
	defer rows.Close()

	samples := make([]telemetry.Sample, 0, 64)
	for rows.Next() {
		var s telemetry.Sample
		if err := rows.Scan(&s.DroneID, &s.Timestamp, &s.Latitude, &s.Longitude, &s.Altitude, &s.Speed, &s.Confidence); err != nil {
			return nil, fmt.Errorf("scan telemetry history: %w", err)
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry history: %w", err)
	}
	return samples, nil
}

func (r *Repository) ListHistoryRange(ctx context.Context, from, to time.Time, droneID telemetry.DroneID, limit int) ([]telemetry.Sample, error) {
	if limit <= 0 || limit > MaxReplayPoints {
		limit = MaxReplayPoints
	}
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT drone_id, recorded_at, ST_Y(position), ST_X(position), altitude, speed, confidence
		   FROM telemetry_history
		  WHERE recorded_at >= $1 AND recorded_at <= $2
		    AND ($3 = '' OR drone_id = $3)
		  ORDER BY recorded_at
		  LIMIT $4`,
		from, to, string(droneID), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query telemetry history range: %w", err)
	}
	defer rows.Close()

	samples := make([]telemetry.Sample, 0, 256)
	for rows.Next() {
		var s telemetry.Sample
		if err := rows.Scan(&s.DroneID, &s.Timestamp, &s.Latitude, &s.Longitude, &s.Altitude, &s.Speed, &s.Confidence); err != nil {
			return nil, fmt.Errorf("scan telemetry history range: %w", err)
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate telemetry history range: %w", err)
	}
	return samples, nil
}
