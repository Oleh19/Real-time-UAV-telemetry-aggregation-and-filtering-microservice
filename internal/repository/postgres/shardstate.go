package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"uavmonitor/internal/telemetry"
)

type SwarmSnapshot struct {
	ID         string
	DroneIDs   []string
	Latitude   float64
	Longitude  float64
	DetectedAt time.Time
}

func (r *Repository) PublishZonePresence(ctx context.Context, replica string, alarms map[telemetry.ZoneID]int) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM zone_presence WHERE replica = $1`, replica)
	for zoneID, drones := range alarms {
		if drones <= 0 {
			continue
		}
		batch.Queue(
			`INSERT INTO zone_presence (replica, zone_id, drones, updated_at)
			 VALUES ($1, $2, $3, now())`,
			replica, int64(zoneID), drones,
		)
	}
	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range batch.Len() {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("publish zone presence: %w", err)
		}
	}
	return nil
}

func (r *Repository) ActiveZoneAlarms(ctx context.Context, freshWithin time.Duration) (map[telemetry.ZoneID]int, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT zone_id, SUM(drones)::bigint
		   FROM zone_presence
		  WHERE updated_at > now() - $1::interval
		  GROUP BY zone_id`,
		fmt.Sprintf("%d milliseconds", freshWithin.Milliseconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("query active zone alarms: %w", err)
	}
	defer rows.Close()

	alarms := make(map[telemetry.ZoneID]int)
	for rows.Next() {
		var zoneID int64
		var drones int64
		if err := rows.Scan(&zoneID, &drones); err != nil {
			return nil, fmt.Errorf("scan active zone alarm: %w", err)
		}
		alarms[telemetry.ZoneID(zoneID)] = int(drones)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active zone alarms: %w", err)
	}
	return alarms, nil
}

func (r *Repository) PublishSwarms(ctx context.Context, replica string, swarms []SwarmSnapshot) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM active_swarms WHERE replica = $1`, replica)
	for _, swarm := range swarms {
		ids, err := json.Marshal(swarm.DroneIDs)
		if err != nil {
			return fmt.Errorf("marshal swarm drone ids: %w", err)
		}
		batch.Queue(
			`INSERT INTO active_swarms (replica, swarm_id, drone_ids, latitude, longitude, detected_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, now())`,
			replica, swarm.ID, ids, swarm.Latitude, swarm.Longitude, swarm.DetectedAt,
		)
	}
	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range batch.Len() {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("publish swarms: %w", err)
		}
	}
	return nil
}

func (r *Repository) ActiveSwarms(ctx context.Context, freshWithin time.Duration) ([]SwarmSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT swarm_id, drone_ids, latitude, longitude, detected_at
		   FROM active_swarms
		  WHERE updated_at > now() - $1::interval
		  ORDER BY swarm_id`,
		fmt.Sprintf("%d milliseconds", freshWithin.Milliseconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("query active swarms: %w", err)
	}
	defer rows.Close()

	swarms := make([]SwarmSnapshot, 0)
	for rows.Next() {
		var swarm SwarmSnapshot
		var ids []byte
		if err := rows.Scan(&swarm.ID, &ids, &swarm.Latitude, &swarm.Longitude, &swarm.DetectedAt); err != nil {
			return nil, fmt.Errorf("scan active swarm: %w", err)
		}
		if err := json.Unmarshal(ids, &swarm.DroneIDs); err != nil {
			return nil, fmt.Errorf("unmarshal swarm drone ids: %w", err)
		}
		swarms = append(swarms, swarm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active swarms: %w", err)
	}
	return swarms, nil
}
