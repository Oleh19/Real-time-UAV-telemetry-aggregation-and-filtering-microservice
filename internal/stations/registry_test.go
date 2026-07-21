package stations_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"uavmonitor/internal/stations"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newRegistry(onlineWithin, offlineAfter time.Duration) *stations.Registry {
	return stations.NewRegistry(stations.Config{
		OnlineWithin: onlineWithin,
		OfflineAfter: offlineAfter,
	}, discardLogger())
}

func TestRegistryTracksStationsAndCounts(t *testing.T) {
	registry := newRegistry(time.Second, 2*time.Second)

	registry.Observe("station-01")
	registry.Observe("station-01")
	registry.Observe("station-02")
	registry.Observe("")

	snapshot := registry.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("Snapshot has %d stations, want 2 (empty id ignored)", len(snapshot))
	}
	if snapshot[0].ID != "station-01" || snapshot[0].Samples != 2 {
		t.Errorf("first station = %s with %d samples, want station-01 with 2", snapshot[0].ID, snapshot[0].Samples)
	}
	if snapshot[0].Status != stations.StatusOnline {
		t.Errorf("fresh station status = %s, want online", snapshot[0].Status)
	}
	online, silent := registry.Counts()
	if online != 2 || silent != 0 {
		t.Errorf("Counts = %d online %d silent, want 2/0", online, silent)
	}
}

func TestRegistryMarksSilentStations(t *testing.T) {
	registry := newRegistry(20*time.Millisecond, 60*time.Millisecond)

	registry.Observe("station-01")
	time.Sleep(35 * time.Millisecond)
	registry.Observe("station-02")

	var silentStatus stations.Status
	for _, info := range registry.Snapshot() {
		if info.ID == "station-01" {
			silentStatus = info.Status
		}
	}
	if silentStatus != stations.StatusStale {
		t.Fatalf("station-01 status = %s, want stale", silentStatus)
	}

	time.Sleep(40 * time.Millisecond)
	for _, info := range registry.Snapshot() {
		if info.ID == "station-01" && info.Status != stations.StatusOffline {
			t.Fatalf("station-01 status = %s, want offline", info.Status)
		}
	}
	online, silent := registry.Counts()
	if online != 0 || silent != 2 {
		t.Errorf("Counts = %d online %d silent, want 0/2", online, silent)
	}
}

func TestRegistryRecoversStations(t *testing.T) {
	registry := newRegistry(20*time.Millisecond, 60*time.Millisecond)

	registry.Observe("station-01")
	time.Sleep(70 * time.Millisecond)
	registry.Observe("station-01")

	snapshot := registry.Snapshot()
	if snapshot[0].Status != stations.StatusOnline {
		t.Fatalf("status after recovery = %s, want online", snapshot[0].Status)
	}
}

func TestRegistryComputesRate(t *testing.T) {
	registry := newRegistry(time.Second, 2*time.Second)

	for range 20 {
		registry.Observe("station-01")
		time.Sleep(2 * time.Millisecond)
	}
	snapshot := registry.Snapshot()
	if snapshot[0].RatePerSecond <= 0 {
		t.Fatalf("rate = %f, want a positive estimate", snapshot[0].RatePerSecond)
	}
}
