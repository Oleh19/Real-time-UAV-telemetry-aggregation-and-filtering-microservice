package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

const (
	maxZoneNameLength = 100
	maxZonePoints     = 500
	maxZoneBodyBytes  = 64 * 1024
)

type createZoneRequest struct {
	Name        string       `json:"name"`
	Coordinates [][2]float64 `json:"coordinates"`
}

type zoneResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func createCustomZoneHandler(deps *dependencies, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createZoneRequest
		body := http.MaxBytesReader(w, r.Body, maxZoneBodyBytes)
		if err := json.NewDecoder(body).Decode(&req); err != nil {
			http.Error(w, "request body must be valid JSON", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || len(req.Name) > maxZoneNameLength {
			http.Error(w, "name must be non-empty and at most 100 characters", http.StatusBadRequest)
			return
		}
		if len(req.Coordinates) > maxZonePoints {
			http.Error(w, "polygon has too many points", http.StatusBadRequest)
			return
		}
		zone, err := deps.repo.CreateCustomZone(r.Context(), req.Name, req.Coordinates)
		if err != nil {
			if errors.Is(err, postgres.ErrInvalidZoneGeometry) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			logger.Error("create custom zone", "error", err)
			http.Error(w, "failed to create zone", http.StatusInternalServerError)
			return
		}
		refreshZoneIndex(deps, logger, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(zoneResponse{ID: int64(zone.ID), Name: zone.Name}); err != nil {
			logger.Error("encode created zone", "error", err)
		}
	}
}

func deleteCustomZoneHandler(deps *dependencies, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "id must be a positive integer", http.StatusBadRequest)
			return
		}
		deleted, err := deps.repo.DeleteCustomZone(r.Context(), telemetry.ZoneID(id))
		if err != nil {
			logger.Error("delete custom zone", "zone_id", id, "error", err)
			http.Error(w, "failed to delete zone", http.StatusInternalServerError)
			return
		}
		if !deleted {
			http.Error(w, "zone not found", http.StatusNotFound)
			return
		}
		refreshZoneIndex(deps, logger, r)
		w.WriteHeader(http.StatusNoContent)
	}
}

func listCustomZonesHandler(deps *dependencies, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		features, err := deps.repo.ListCustomZoneFeatures(r.Context())
		if err != nil {
			logger.Error("list custom zones", "error", err)
			http.Error(w, "failed to load custom zones", http.StatusInternalServerError)
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
			logger.Error("encode custom zones", "error", err)
		}
	}
}

func refreshZoneIndex(deps *dependencies, logger *slog.Logger, r *http.Request) {
	if err := deps.zoneIndex.Refresh(r.Context(), deps.repo); err != nil {
		logger.Error("refresh zone index after zone change", "error", err)
	}
}
