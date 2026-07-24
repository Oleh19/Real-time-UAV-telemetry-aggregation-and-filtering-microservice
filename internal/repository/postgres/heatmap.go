package postgres

import (
	"context"
	"fmt"
	"time"
)

const (
	MaxHeatmapCells    = 5000
	MinHeatmapCell     = 0.05
	MaxHeatmapCell     = 5.0
	DefaultHeatmapCell = 0.25
)

type HeatCell struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Samples   int64   `json:"samples"`
	Drones    int64   `json:"drones"`
}

func (r *Repository) IncursionHeatmap(ctx context.Context, from, to time.Time, cellDegrees float64) ([]HeatCell, error) {
	if cellDegrees < MinHeatmapCell {
		cellDegrees = DefaultHeatmapCell
	}
	if cellDegrees > MaxHeatmapCell {
		cellDegrees = MaxHeatmapCell
	}
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx,
		`SELECT floor(ST_Y(position) / $3) * $3 + $3 / 2 AS lat,
		        floor(ST_X(position) / $3) * $3 + $3 / 2 AS lon,
		        count(*) AS samples,
		        count(DISTINCT drone_id) AS drones
		   FROM telemetry_history
		  WHERE recorded_at >= $1 AND recorded_at <= $2
		  GROUP BY lat, lon
		  ORDER BY samples DESC
		  LIMIT $4`,
		from, to, cellDegrees, MaxHeatmapCells,
	)
	if err != nil {
		return nil, fmt.Errorf("query incursion heatmap: %w", err)
	}
	defer rows.Close()

	cells := make([]HeatCell, 0, 256)
	for rows.Next() {
		var cell HeatCell
		if err := rows.Scan(&cell.Latitude, &cell.Longitude, &cell.Samples, &cell.Drones); err != nil {
			return nil, fmt.Errorf("scan incursion heatmap: %w", err)
		}
		cells = append(cells, cell)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incursion heatmap: %w", err)
	}
	return cells, nil
}
