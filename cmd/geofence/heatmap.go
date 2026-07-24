package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"uavmonitor/internal/repository/postgres"
)

const defaultHeatmapWindow = 24 * time.Hour

type heatmapSource interface {
	IncursionHeatmap(ctx context.Context, from, to time.Time, cellDegrees float64) ([]postgres.HeatCell, error)
}

func heatmapHandler(source heatmapSource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		to := time.Now()
		if raw := r.URL.Query().Get("to"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, errInvalidTime("to").Error(), http.StatusBadRequest)
				return
			}
			to = parsed
		}
		from := to.Add(-defaultHeatmapWindow)
		if raw := r.URL.Query().Get("from"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				http.Error(w, errInvalidTime("from").Error(), http.StatusBadRequest)
				return
			}
			from = parsed
		}
		if !from.Before(to) {
			http.Error(w, errRange().Error(), http.StatusBadRequest)
			return
		}
		cell := postgres.DefaultHeatmapCell
		if raw := r.URL.Query().Get("cell"); raw != "" {
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil || parsed <= 0 {
				http.Error(w, "cell must be a positive number of degrees", http.StatusBadRequest)
				return
			}
			cell = parsed
		}
		cells, err := source.IncursionHeatmap(r.Context(), from, to, cell)
		if err != nil {
			logger.Error("build incursion heatmap", "error", err)
			http.Error(w, "failed to build heatmap", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(cells); err != nil {
			logger.Error("encode incursion heatmap", "error", err)
		}
	}
}
