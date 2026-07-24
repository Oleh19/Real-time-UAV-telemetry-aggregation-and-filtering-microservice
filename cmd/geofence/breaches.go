package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"uavmonitor/internal/repository/postgres"
)

type breachSource interface {
	ListZoneBreaches(ctx context.Context, limit int) ([]postgres.BreachRecord, error)
	SetBreachStatus(ctx context.Context, id int64, status string) error
}

func breachStatusHandler(source breachSource, status string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "id must be a positive integer", http.StatusBadRequest)
			return
		}
		if err := source.SetBreachStatus(r.Context(), id, status); err != nil {
			if errors.Is(err, postgres.ErrBreachNotFound) {
				http.Error(w, "breach not found", http.StatusNotFound)
				return
			}
			logger.Error("set breach status", "id", id, "status", status, "error", err)
			http.Error(w, "failed to update breach", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func breachesHandler(source breachSource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := postgres.DefaultBreachLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		records, err := source.ListZoneBreaches(r.Context(), limit)
		if err != nil {
			logger.Error("list zone breaches", "error", err)
			http.Error(w, "failed to load breach journal", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(records); err != nil {
			logger.Error("encode zone breaches", "error", err)
		}
	}
}
