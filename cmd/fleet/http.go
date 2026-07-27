package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/fleet"
	"uavmonitor/internal/sse"
)

const maxBodyBytes = 64 * 1024

type fleetService interface {
	Snapshot() fleet.FleetSnapshot
	Stats() fleet.FleetStats
	AddDrone(ctx context.Context, drone fleet.Drone) (fleet.Drone, error)
	RemoveDrone(ctx context.Context, id string) error
	CreateMission(ctx context.Context, name, droneID string, waypoints []fleet.Waypoint) (fleet.Mission, error)
	DeleteMission(ctx context.Context, id string) error
	Launch(ctx context.Context, missionID string) (fleet.Mission, error)
	Pause(ctx context.Context, missionID string) (fleet.Mission, error)
	Resume(ctx context.Context, missionID string) (fleet.Mission, error)
	Abort(ctx context.Context, missionID string) (fleet.Mission, error)
	Recall(ctx context.Context, droneID string) (fleet.Drone, error)
}

type addDroneRequest struct {
	ID       string         `json:"id"`
	Model    string         `json:"model"`
	Base     fleet.Waypoint `json:"base"`
	Firmware string         `json:"firmware"`
}

type createMissionRequest struct {
	Name      string           `json:"name"`
	DroneID   string           `json:"droneId"`
	Waypoints []fleet.Waypoint `json:"waypoints"`
}

func newHTTPHandler(manager fleetService, pool *pgxpool.Pool, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", newMetricsHandler(manager))

	mux.HandleFunc("GET /fleet", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, manager.Snapshot(), logger)
	})
	mux.HandleFunc("GET /fleet/stream", sse.Handler(sse.DefaultInterval, func(context.Context) any {
		return manager.Snapshot()
	}, logger))

	mux.HandleFunc("POST /fleet/drones", func(w http.ResponseWriter, r *http.Request) {
		var req addDroneRequest
		if !decode(w, r, &req) {
			return
		}
		drone, err := manager.AddDrone(r.Context(), fleet.Drone{ID: req.ID, Model: req.Model, Base: req.Base, Firmware: req.Firmware})
		if err != nil {
			writeFleetError(w, err, logger)
			return
		}
		writeJSONStatus(w, http.StatusCreated, drone, logger)
	})
	mux.HandleFunc("DELETE /fleet/drones/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.RemoveDrone(r.Context(), r.PathValue("id")); err != nil {
			writeFleetError(w, err, logger)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /fleet/drones/{id}/recall", func(w http.ResponseWriter, r *http.Request) {
		drone, err := manager.Recall(r.Context(), r.PathValue("id"))
		if err != nil {
			writeFleetError(w, err, logger)
			return
		}
		writeJSON(w, drone, logger)
	})

	mux.HandleFunc("POST /fleet/missions", func(w http.ResponseWriter, r *http.Request) {
		var req createMissionRequest
		if !decode(w, r, &req) {
			return
		}
		mission, err := manager.CreateMission(r.Context(), req.Name, req.DroneID, req.Waypoints)
		if err != nil {
			writeFleetError(w, err, logger)
			return
		}
		writeJSONStatus(w, http.StatusCreated, mission, logger)
	})
	mux.HandleFunc("DELETE /fleet/missions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := manager.DeleteMission(r.Context(), r.PathValue("id")); err != nil {
			writeFleetError(w, err, logger)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /fleet/missions/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var mission fleet.Mission
		var err error
		switch r.PathValue("action") {
		case "launch":
			mission, err = manager.Launch(r.Context(), id)
		case "pause":
			mission, err = manager.Pause(r.Context(), id)
		case "resume":
			mission, err = manager.Resume(r.Context(), id)
		case "abort":
			mission, err = manager.Abort(r.Context(), id)
		default:
			http.Error(w, "unknown action", http.StatusNotFound)
			return
		}
		if err != nil {
			writeFleetError(w, err, logger)
			return
		}
		writeJSON(w, mission, logger)
	})

	return mux
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(v); err != nil {
		http.Error(w, "request body must be valid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any, logger *slog.Logger) {
	writeJSONStatus(w, http.StatusOK, v, logger)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("encode fleet response", "error", err)
	}
}

func writeFleetError(w http.ResponseWriter, err error, logger *slog.Logger) {
	switch {
	case errors.Is(err, fleet.ErrDroneNotFound), errors.Is(err, fleet.ErrMissionNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, fleet.ErrDroneExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, fleet.ErrInvalidDrone), errors.Is(err, fleet.ErrInvalidMission):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, fleet.ErrDroneBusy), errors.Is(err, fleet.ErrBatteryLow), errors.Is(err, fleet.ErrBadTransition):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		logger.Error("fleet request failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
