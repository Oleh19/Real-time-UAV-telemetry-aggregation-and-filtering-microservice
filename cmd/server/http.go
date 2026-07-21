package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nats-io/nats.go"

	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/sse"
	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

type telemetryEvent struct {
	Drones []telemetry.Sample `json:"drones"`
	Stats  usecase.Stats      `json:"stats"`
}

func observabilityHandler(ingestor *usecase.Ingestor, publisher *natspub.AsyncPublisher, fuser fusionStats, hub hubStats, classifier classifierStats, natsConn *nats.Conn, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if natsConn.Status() != nats.CONNECTED {
			http.Error(w, "nats unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", newMetricsHandler(ingestor, publisher, fuser, hub, classifier))
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, metricsSnapshot(ingestor, publisher))
	})
	mux.HandleFunc("GET /drones", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, ingestor.Snapshot())
	})
	mux.HandleFunc("GET /events", sse.Handler(sse.DefaultInterval, func(context.Context) any {
		return telemetryEvent{
			Drones: ingestor.Snapshot(),
			Stats:  metricsSnapshot(ingestor, publisher),
		}
	}, logger))
	return mux
}

func metricsSnapshot(ingestor *usecase.Ingestor, publisher *natspub.AsyncPublisher) usecase.Stats {
	stats := ingestor.Stats()
	stats.Failed += publisher.Failed()
	return stats
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
