package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

type fakeBreachSource struct {
	records []postgres.BreachRecord
}

func (f *fakeBreachSource) ListZoneBreaches(_ context.Context, _ int) ([]postgres.BreachRecord, error) {
	return f.records, nil
}

func (f *fakeBreachSource) SetBreachStatus(_ context.Context, _ int64, _ string) error {
	return nil
}

func sampleRecords() []postgres.BreachRecord {
	return []postgres.BreachRecord{{
		ID:         42,
		DroneID:    telemetry.DroneID("target-001"),
		ZoneID:     7,
		ZoneName:   "Kyiv Oblast",
		Event:      telemetry.BreachEntered,
		Status:     "open",
		OccurredAt: time.Unix(1700000000, 0).UTC(),
		Latitude:   50.45,
		Longitude:  30.52,
		Altitude:   120,
	}}
}

func TestBreachExportCSV(t *testing.T) {
	handler := breachExportHandler(&fakeBreachSource{records: sampleRecords()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/breaches/export", nil))

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content type = %q, want text/csv", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "id,drone_id,zone_id,zone_name,event,status,occurred_at,latitude,longitude,altitude") {
		t.Fatalf("csv header missing: %q", body)
	}
	if !strings.Contains(body, "42,target-001,7,Kyiv Oblast,entered,open,") {
		t.Fatalf("csv row missing: %q", body)
	}
}

func TestBreachExportGeoJSON(t *testing.T) {
	handler := breachExportHandler(&fakeBreachSource{records: sampleRecords()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/breaches/export?format=geojson", nil))

	var fc geoJSONFeatureCollection
	if err := json.NewDecoder(rec.Body).Decode(&fc); err != nil {
		t.Fatalf("decode geojson: %v", err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 1 {
		t.Fatalf("geojson = %+v, want one feature collection", fc)
	}
	if fc.Features[0].Properties["zone_name"] != "Kyiv Oblast" {
		t.Fatalf("feature properties = %+v", fc.Features[0].Properties)
	}
	if !strings.Contains(string(fc.Features[0].Geometry), "30.52") {
		t.Fatalf("geometry missing longitude: %s", fc.Features[0].Geometry)
	}
}
