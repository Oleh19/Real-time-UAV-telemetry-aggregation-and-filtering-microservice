package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"uavmonitor/internal/replay"
	"uavmonitor/internal/telemetry"
)

type startReplayRequest struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	Speed   float64 `json:"speed"`
	DroneID string  `json:"droneId"`
}

func startReplayHandler(manager *replay.Manager, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req startReplayRequest
		body := http.MaxBytesReader(w, r.Body, maxZoneBodyBytes)
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			http.Error(w, "request body must be valid JSON", http.StatusBadRequest)
			return
		}
		from, err := time.Parse(time.RFC3339, req.From)
		if err != nil {
			http.Error(w, "from must be an RFC 3339 timestamp", http.StatusBadRequest)
			return
		}
		to, err := time.Parse(time.RFC3339, req.To)
		if err != nil {
			http.Error(w, "to must be an RFC 3339 timestamp", http.StatusBadRequest)
			return
		}
		status, err := manager.Start(r.Context(), replay.Request{
			From:    from,
			To:      to,
			Speed:   req.Speed,
			DroneID: telemetry.DroneID(req.DroneID),
		})
		if err != nil {
			switch {
			case errors.Is(err, replay.ErrInvalidRange), errors.Is(err, replay.ErrInvalidSpeed):
				http.Error(w, err.Error(), http.StatusBadRequest)
			case errors.Is(err, replay.ErrNoHistory):
				http.Error(w, err.Error(), http.StatusNotFound)
			case errors.Is(err, replay.ErrTooManyReplays):
				http.Error(w, err.Error(), http.StatusConflict)
			default:
				logger.Error("start replay", "error", err)
				http.Error(w, "failed to start replay", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(status); err != nil {
			logger.Error("encode replay status", "error", err)
		}
	}
}

func listReplaysHandler(manager *replay.Manager, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(manager.List()); err != nil {
			logger.Error("encode replays", "error", err)
		}
	}
}

func cancelReplayHandler(manager *replay.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := manager.Cancel(r.PathValue("id")); err != nil {
			http.Error(w, "replay not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
