package postgres_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"uavmonitor/internal/repository/postgres"
)

func TestSwarmSnapshotJSONUsesDashboardKeys(t *testing.T) {
	snapshot := postgres.SwarmSnapshot{
		ID:         "swarm-001",
		DroneIDs:   []string{"target-001", "target-002"},
		Latitude:   50.4,
		Longitude:  30.5,
		DetectedAt: time.Unix(1700000000, 0).UTC(),
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	for _, key := range []string{`"id"`, `"droneIds"`, `"latitude"`, `"longitude"`, `"detectedAt"`} {
		if !strings.Contains(string(payload), key) {
			t.Errorf("swarm snapshot JSON %s is missing key %s", payload, key)
		}
	}
}
