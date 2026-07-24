package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"uavmonitor/internal/repository/postgres"
)

type friendlySource interface {
	ListFriendlySquawks(ctx context.Context) ([]postgres.FriendlySquawk, error)
}

func listFriendlyHandler(source friendlySource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		squawks, err := source.ListFriendlySquawks(r.Context())
		if err != nil {
			logger.Error("list friendly squawks", "error", err)
			http.Error(w, "failed to load friendly registry", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(squawks); err != nil {
			logger.Error("encode friendly squawks", "error", err)
		}
	}
}

func createFriendlyHandler(deps *dependencies, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload postgres.FriendlySquawk
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		payload.Code = strings.TrimSpace(payload.Code)
		payload.Label = strings.TrimSpace(payload.Label)
		if payload.Code == "" || payload.Label == "" {
			http.Error(w, "code and label are required", http.StatusBadRequest)
			return
		}
		if err := deps.repo.AddFriendlySquawk(r.Context(), payload.Code, payload.Label); err != nil {
			logger.Error("add friendly squawk", "code", payload.Code, "error", err)
			http.Error(w, "failed to store friendly squawk", http.StatusInternalServerError)
			return
		}
		if err := deps.friendlyCache.Refresh(r.Context(), deps.repo); err != nil {
			logger.Error("refresh friendly registry", "error", err)
		}
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			logger.Error("encode friendly squawk", "error", err)
		}
	}
}

func deleteFriendlyHandler(deps *dependencies, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimSpace(r.PathValue("code"))
		if code == "" {
			http.Error(w, "code is required", http.StatusBadRequest)
			return
		}
		if err := deps.repo.DeleteFriendlySquawk(r.Context(), code); err != nil {
			if errors.Is(err, postgres.ErrFriendlyNotFound) {
				http.Error(w, "friendly squawk not found", http.StatusNotFound)
				return
			}
			logger.Error("delete friendly squawk", "code", code, "error", err)
			http.Error(w, "failed to delete friendly squawk", http.StatusInternalServerError)
			return
		}
		if err := deps.friendlyCache.Refresh(r.Context(), deps.repo); err != nil {
			logger.Error("refresh friendly registry", "error", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
