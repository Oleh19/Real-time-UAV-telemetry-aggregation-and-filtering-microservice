package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"uavmonitor/internal/broadcast"
	"uavmonitor/internal/classify"
	"uavmonitor/internal/fusion"
	"uavmonitor/internal/stations"
	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

type nopPublisher struct{}

func (nopPublisher) Publish(context.Context, telemetry.Sample) error { return nil }

type fakeFailures int64

func (f fakeFailures) Failed() int64 { return int64(f) }

func TestMetricsHandlerExposesIngestMetrics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ingestor := usecase.NewIngestor(nopPublisher{}, logger, 8, time.Minute)

	handler := newMetricsHandler(ingestor, fakeFailures(3), fusion.NewFuser(fusion.DefaultConfig()), broadcast.NewHub(8), classify.NewClassifier(), stations.NewRegistry(stations.DefaultConfig(), logger))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	wanted := []string{
		"uav_ingest_received_total 0",
		"uav_ingest_dropped_total 0",
		"uav_ingest_failed_total 3",
		"uav_ingest_queue_capacity 8",
		"uav_tracked_drones 0",
		"uav_fused_tracks 0",
		"uav_subscribers 0",
		"go_goroutines",
	}
	for _, want := range wanted {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %q", want)
		}
	}
}
