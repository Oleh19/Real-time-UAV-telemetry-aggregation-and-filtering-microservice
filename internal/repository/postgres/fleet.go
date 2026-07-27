package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"uavmonitor/internal/fleet"
)

var (
	_ fleet.Store     = (*Repository)(nil)
	_ fleet.ZoneGuard = (*Repository)(nil)
)

func (r *Repository) RestrictedWaypoints(ctx context.Context, waypoints []fleet.Waypoint) ([]int, error) {
	if len(waypoints) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	lons := make([]float64, len(waypoints))
	lats := make([]float64, len(waypoints))
	for i, w := range waypoints {
		lons[i] = w.Longitude
		lats[i] = w.Latitude
	}

	rows, err := r.pool.Query(ctx,
		`SELECT w.idx - 1
		   FROM unnest($1::float8[], $2::float8[]) WITH ORDINALITY AS w(lon, lat, idx)
		  WHERE EXISTS (
		    SELECT 1 FROM custom_zones z
		     WHERE ST_Contains(z.boundary, ST_SetSRID(ST_MakePoint(w.lon, w.lat), 4326))
		  )`,
		lons, lats,
	)
	if err != nil {
		return nil, fmt.Errorf("query restricted waypoints: %w", err)
	}
	defer rows.Close()

	var restricted []int
	for rows.Next() {
		var idx int
		if err := rows.Scan(&idx); err != nil {
			return nil, fmt.Errorf("scan restricted waypoint: %w", err)
		}
		restricted = append(restricted, idx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restricted waypoints: %w", err)
	}
	return restricted, nil
}

func (r *Repository) ListDrones(ctx context.Context) ([]fleet.Drone, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT id, model, base_lat, base_lon, firmware FROM fleet_drones ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query fleet drones: %w", err)
	}
	defer rows.Close()

	drones := make([]fleet.Drone, 0)
	for rows.Next() {
		var d fleet.Drone
		if err := rows.Scan(&d.ID, &d.Model, &d.Base.Latitude, &d.Base.Longitude, &d.Firmware); err != nil {
			return nil, fmt.Errorf("scan fleet drone: %w", err)
		}
		drones = append(drones, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fleet drones: %w", err)
	}
	return drones, nil
}

func (r *Repository) SaveDrone(ctx context.Context, drone fleet.Drone) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO fleet_drones (id, model, base_lat, base_lon, firmware)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO UPDATE SET
		   model = EXCLUDED.model, base_lat = EXCLUDED.base_lat,
		   base_lon = EXCLUDED.base_lon, firmware = EXCLUDED.firmware`,
		drone.ID, drone.Model, drone.Base.Latitude, drone.Base.Longitude, drone.Firmware,
	)
	if err != nil {
		return fmt.Errorf("save fleet drone: %w", err)
	}
	return nil
}

func (r *Repository) DeleteDrone(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	if _, err := r.pool.Exec(ctx, `DELETE FROM fleet_drones WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete fleet drone: %w", err)
	}
	return nil
}

func (r *Repository) ListMissions(ctx context.Context) ([]fleet.Mission, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT id, name, drone_id, waypoints, state, progress FROM fleet_missions ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query fleet missions: %w", err)
	}
	defer rows.Close()

	missions := make([]fleet.Mission, 0)
	for rows.Next() {
		var m fleet.Mission
		var waypoints []byte
		if err := rows.Scan(&m.ID, &m.Name, &m.DroneID, &waypoints, &m.State, &m.Progress); err != nil {
			return nil, fmt.Errorf("scan fleet mission: %w", err)
		}
		if err := json.Unmarshal(waypoints, &m.Waypoints); err != nil {
			return nil, fmt.Errorf("decode mission waypoints: %w", err)
		}
		missions = append(missions, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fleet missions: %w", err)
	}
	return missions, nil
}

func (r *Repository) SaveMission(ctx context.Context, mission fleet.Mission) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	waypoints, err := json.Marshal(mission.Waypoints)
	if err != nil {
		return fmt.Errorf("encode mission waypoints: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO fleet_missions (id, name, drone_id, waypoints, state, progress)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name, drone_id = EXCLUDED.drone_id, waypoints = EXCLUDED.waypoints,
		   state = EXCLUDED.state, progress = EXCLUDED.progress`,
		mission.ID, mission.Name, mission.DroneID, waypoints, string(mission.State), mission.Progress,
	)
	if err != nil {
		return fmt.Errorf("save fleet mission: %w", err)
	}
	return nil
}

func (r *Repository) DeleteMission(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	if _, err := r.pool.Exec(ctx, `DELETE FROM fleet_missions WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete fleet mission: %w", err)
	}
	return nil
}
