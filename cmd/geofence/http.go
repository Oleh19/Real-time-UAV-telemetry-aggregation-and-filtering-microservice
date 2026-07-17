package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/sse"
)

type geoJSONFeature struct {
	Type       string          `json:"type"`
	Properties map[string]any  `json:"properties"`
	Geometry   json.RawMessage `json:"geometry"`
}

type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
}

type oblastAlert struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Alarmed bool   `json:"alarmed"`
	Drones  int    `json:"drones"`
}

func newHTTPHandler(deps *dependencies, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(deps.pool))
	mux.HandleFunc("GET /alerts", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(alertsSnapshot(deps)); err != nil {
			logger.Error("encode oblast alerts", "error", err)
		}
	})
	mux.HandleFunc("GET /events", sse.Handler(eventsInterval, func(context.Context) any {
		return alertsSnapshot(deps)
	}, logger))
	mux.HandleFunc("GET /zones", zonesHandler(deps.repo, logger))
	return mux
}

func healthHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func alertsSnapshot(deps *dependencies) []oblastAlert {
	alarms := deps.checker.ActiveAlarms()
	alerts := make([]oblastAlert, 0, len(deps.oblasts))
	for _, oblast := range deps.oblasts {
		drones := alarms[oblast.ID]
		alerts = append(alerts, oblastAlert{
			ID:      int64(oblast.ID),
			Name:    oblast.Name,
			Alarmed: drones > 0,
			Drones:  drones,
		})
	}
	return alerts
}

func zonesHandler(repo *postgres.Repository, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		features, err := repo.ListZoneFeatures(r.Context())
		if err != nil {
			logger.Error("list zone features", "error", err)
			http.Error(w, "failed to load zones", http.StatusInternalServerError)
			return
		}
		collection := geoJSONFeatureCollection{Type: "FeatureCollection", Features: make([]geoJSONFeature, 0, len(features))}
		for _, f := range features {
			collection.Features = append(collection.Features, geoJSONFeature{
				Type:       "Feature",
				Properties: map[string]any{"id": f.Zone.ID, "name": f.Zone.Name},
				Geometry:   f.Geometry,
			})
		}
		w.Header().Set("Content-Type", "application/geo+json")
		if err := json.NewEncoder(w).Encode(collection); err != nil {
			logger.Error("encode zone features", "error", err)
		}
	}
}
