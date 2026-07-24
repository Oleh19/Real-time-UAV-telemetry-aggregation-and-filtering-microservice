package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"uavmonitor/internal/repository/postgres"
)

type fakeHeatmapSource struct {
	cells       []postgres.HeatCell
	err         error
	lastFrom    time.Time
	lastTo      time.Time
	lastCell    float64
	invocations int
}

func (f *fakeHeatmapSource) IncursionHeatmap(_ context.Context, from, to time.Time, cell float64) ([]postgres.HeatCell, error) {
	f.invocations++
	f.lastFrom = from
	f.lastTo = to
	f.lastCell = cell
	return f.cells, f.err
}

func TestHeatmapHandlerReturnsCells(t *testing.T) {
	source := &fakeHeatmapSource{cells: []postgres.HeatCell{{Latitude: 50.1, Longitude: 30.1, Samples: 12, Drones: 3}}}
	handler := heatmapHandler(source, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/analytics/heatmap?cell=0.5", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var cells []postgres.HeatCell
	if err := json.NewDecoder(rec.Body).Decode(&cells); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(cells) != 1 || cells[0].Samples != 12 {
		t.Fatalf("cells = %+v, want one cell with 12 samples", cells)
	}
	if source.lastCell != 0.5 {
		t.Fatalf("cell degrees = %f, want 0.5", source.lastCell)
	}
}

func TestHeatmapHandlerRejectsBadRange(t *testing.T) {
	source := &fakeHeatmapSource{}
	handler := heatmapHandler(source, slog.New(slog.NewTextHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodGet, "/analytics/heatmap?from=2026-07-21T11:00:00Z&to=2026-07-21T10:00:00Z", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if source.invocations != 0 {
		t.Fatalf("source called %d times, want 0", source.invocations)
	}
}
