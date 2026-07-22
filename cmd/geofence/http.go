package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/sse"
	"uavmonitor/internal/telemetry"
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
	mux.HandleFunc("GET /alerts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(alertsSnapshot(r.Context(), deps, logger)); err != nil {
			logger.Error("encode oblast alerts", "error", err)
		}
	})
	mux.HandleFunc("GET /events", sse.Handler(sse.DefaultInterval, func(ctx context.Context) any {
		return alertsSnapshot(ctx, deps, logger)
	}, logger))
	mux.HandleFunc("GET /zones", zonesHandler(deps.repo, logger))
	mux.HandleFunc("GET /history", historyHandler(deps.repo, logger))
	mux.HandleFunc("GET /breaches", breachesHandler(deps.repo, logger))
	mux.HandleFunc("GET /swarms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		swarms, err := deps.repo.ActiveSwarms(r.Context(), shardStateFresh)
		if err != nil {
			logger.Error("list active swarms", "error", err)
			http.Error(w, "failed to load swarms", http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(swarms); err != nil {
			logger.Error("encode swarms", "error", err)
		}
	})
	mux.HandleFunc("GET /custom-zones", listCustomZonesHandler(deps, logger))
	mux.HandleFunc("POST /custom-zones", createCustomZoneHandler(deps, logger))
	mux.HandleFunc("DELETE /custom-zones/{id}", deleteCustomZoneHandler(deps, logger))
	mux.HandleFunc("GET /replays", listReplaysHandler(deps.replayManager, logger))
	mux.HandleFunc("POST /replays", startReplayHandler(deps.replayManager, logger))
	mux.HandleFunc("DELETE /replays/{id}", cancelReplayHandler(deps.replayManager))
	mux.Handle("GET /metrics", newMetricsHandler(deps))
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

func alertsSnapshot(ctx context.Context, deps *dependencies, logger *slog.Logger) []oblastAlert {
	alarms, err := deps.repo.ActiveZoneAlarms(ctx, shardStateFresh)
	if err != nil {
		logger.Error("query active zone alarms", "error", err)
		alarms = map[telemetry.ZoneID]int{}
	}
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
