package main

import (
	"context"
	"encoding/csv"
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

func breachExportHandler(source breachSource, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		records, err := source.ListZoneBreaches(r.Context(), postgres.MaxBreachLimit)
		if err != nil {
			logger.Error("export zone breaches", "error", err)
			http.Error(w, "failed to load breach journal", http.StatusInternalServerError)
			return
		}
		switch r.URL.Query().Get("format") {
		case "geojson":
			writeBreachGeoJSON(w, records, logger)
		default:
			writeBreachCSV(w, records, logger)
		}
	}
}

func writeBreachCSV(w http.ResponseWriter, records []postgres.BreachRecord, logger *slog.Logger) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="incidents.csv"`)
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"id", "drone_id", "zone_id", "zone_name", "event", "status", "occurred_at", "latitude", "longitude", "altitude"}); err != nil {
		logger.Error("write breach csv header", "error", err)
		return
	}
	for _, rec := range records {
		row := []string{
			strconv.FormatInt(rec.ID, 10),
			string(rec.DroneID),
			strconv.FormatInt(int64(rec.ZoneID), 10),
			rec.ZoneName,
			string(rec.Event),
			rec.Status,
			rec.OccurredAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			strconv.FormatFloat(rec.Latitude, 'f', 6, 64),
			strconv.FormatFloat(rec.Longitude, 'f', 6, 64),
			strconv.FormatFloat(rec.Altitude, 'f', 1, 64),
		}
		if err := cw.Write(row); err != nil {
			logger.Error("write breach csv row", "error", err)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		logger.Error("flush breach csv", "error", err)
	}
}

func writeBreachGeoJSON(w http.ResponseWriter, records []postgres.BreachRecord, logger *slog.Logger) {
	features := make([]geoJSONFeature, 0, len(records))
	for _, rec := range records {
		geometry, err := json.Marshal(map[string]any{
			"type":        "Point",
			"coordinates": []float64{rec.Longitude, rec.Latitude},
		})
		if err != nil {
			logger.Error("marshal breach geometry", "error", err)
			continue
		}
		features = append(features, geoJSONFeature{
			Type: "Feature",
			Properties: map[string]any{
				"id":          rec.ID,
				"drone_id":    string(rec.DroneID),
				"zone_id":     int64(rec.ZoneID),
				"zone_name":   rec.ZoneName,
				"event":       string(rec.Event),
				"status":      rec.Status,
				"occurred_at": rec.OccurredAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				"altitude":    rec.Altitude,
			},
			Geometry: geometry,
		})
	}
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Content-Disposition", `attachment; filename="incidents.geojson"`)
	if err := json.NewEncoder(w).Encode(geoJSONFeatureCollection{Type: "FeatureCollection", Features: features}); err != nil {
		logger.Error("encode breach geojson", "error", err)
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
