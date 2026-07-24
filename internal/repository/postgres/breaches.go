package postgres

import (
	"context"
	"fmt"
	"time"

	"uavmonitor/internal/telemetry"
)

const (
	DefaultBreachLimit = 100
	MaxBreachLimit     = 500

	BreachStatusOpen         = "open"
	BreachStatusAcknowledged = "acknowledged"
	BreachStatusResolved     = "resolved"
)

var ErrBreachNotFound = fmt.Errorf("breach not found")

type BreachRecord struct {
	ID         int64
	DroneID    telemetry.DroneID
	ZoneID     telemetry.ZoneID
	ZoneName   string
	Event      telemetry.BreachEvent
	Status     string
	OccurredAt time.Time
	Latitude   float64
	Longitude  float64
	Altitude   float64
}

func (r *Repository) SaveZoneBreach(ctx context.Context, breach telemetry.ZoneBreach) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO zone_breaches (drone_id, zone_id, zone_name, event, occurred_at, position, altitude)
		 VALUES ($1, $2, $3, $4, $5, ST_SetSRID(ST_MakePoint($6, $7), 4326), $8)
		 ON CONFLICT (drone_id, zone_id, event, occurred_at) DO NOTHING`,
		string(breach.Sample.DroneID),
		int64(breach.Zone.ID),
		breach.Zone.Name,
		string(breach.Event),
		breach.Sample.Timestamp,
		breach.Sample.Longitude,
		breach.Sample.Latitude,
		breach.Sample.Altitude,
	)
	if err != nil {
		return fmt.Errorf("insert zone breach: %w", err)
	}
	return nil
}

func (r *Repository) ListZoneBreaches(ctx context.Context, limit int) ([]BreachRecord, error) {
	if limit <= 0 || limit > MaxBreachLimit {
		limit = DefaultBreachLimit
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT id, drone_id, zone_id, zone_name, event, status, occurred_at, ST_Y(position), ST_X(position), altitude
		   FROM zone_breaches
		  ORDER BY occurred_at DESC
		  LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query zone breaches: %w", err)
	}
	defer rows.Close()

	records := make([]BreachRecord, 0, limit)
	for rows.Next() {
		var rec BreachRecord
		if err := rows.Scan(&rec.ID, &rec.DroneID, &rec.ZoneID, &rec.ZoneName, &rec.Event, &rec.Status, &rec.OccurredAt, &rec.Latitude, &rec.Longitude, &rec.Altitude); err != nil {
			return nil, fmt.Errorf("scan zone breach: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate zone breaches: %w", err)
	}
	return records, nil
}

func (r *Repository) SetBreachStatus(ctx context.Context, id int64, status string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	column := ""
	switch status {
	case BreachStatusAcknowledged:
		column = "acknowledged_at"
	case BreachStatusResolved:
		column = "resolved_at"
	default:
		return fmt.Errorf("invalid breach status %q", status)
	}
	tag, err := r.pool.Exec(ctx,
		fmt.Sprintf(`UPDATE zone_breaches SET status = $1, %s = now() WHERE id = $2`, column),
		status, id,
	)
	if err != nil {
		return fmt.Errorf("update breach status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBreachNotFound
	}
	return nil
}
