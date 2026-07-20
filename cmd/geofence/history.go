package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

const defaultHistoryWindow = 15 * time.Minute

type historySource interface {
	ListHistory(ctx context.Context, droneID telemetry.DroneID, from, to time.Time, limit int) ([]telemetry.Sample, error)
}

func historyHandler(source historySource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		droneID, from, to, err := parseHistoryQuery(r.URL.Query().Get("drone_id"), r.URL.Query().Get("from"), r.URL.Query().Get("to"), time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		samples, err := source.ListHistory(r.Context(), droneID, from, to, postgres.MaxHistoryPoints)
		if err != nil {
			logger.Error("list telemetry history", "drone_id", droneID, "error", err)
			http.Error(w, "failed to load history", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(samples); err != nil {
			logger.Error("encode telemetry history", "error", err)
		}
	}
}

func parseHistoryQuery(droneID, fromRaw, toRaw string, now time.Time) (telemetry.DroneID, time.Time, time.Time, error) {
	if droneID == "" {
		return "", time.Time{}, time.Time{}, errRequired("drone_id")
	}
	to := now
	if toRaw != "" {
		parsed, err := time.Parse(time.RFC3339, toRaw)
		if err != nil {
			return "", time.Time{}, time.Time{}, errInvalidTime("to")
		}
		to = parsed
	}
	from := to.Add(-defaultHistoryWindow)
	if fromRaw != "" {
		parsed, err := time.Parse(time.RFC3339, fromRaw)
		if err != nil {
			return "", time.Time{}, time.Time{}, errInvalidTime("from")
		}
		from = parsed
	}
	if !from.Before(to) {
		return "", time.Time{}, time.Time{}, errRange()
	}
	return telemetry.DroneID(droneID), from, to, nil
}

type queryError string

func (e queryError) Error() string { return string(e) }

func errRequired(param string) error {
	return queryError(param + " query parameter is required")
}

func errInvalidTime(param string) error {
	return queryError(param + " must be an RFC 3339 timestamp")
}

func errRange() error {
	return queryError("from must be before to")
}
