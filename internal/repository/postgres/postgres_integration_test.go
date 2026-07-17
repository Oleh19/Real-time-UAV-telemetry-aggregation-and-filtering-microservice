//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.NewRepository(pool).Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestSeedAndListOblasts(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewRepository(testPool(t))

	if _, err := repo.SeedOblasts(ctx); err != nil {
		t.Fatalf("SeedOblasts: %v", err)
	}

	zones, err := repo.ListZones(ctx)
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	if len(zones) != 27 {
		t.Fatalf("ListZones returned %d oblasts, want 27", len(zones))
	}

	features, err := repo.ListZoneFeatures(ctx)
	if err != nil {
		t.Fatalf("ListZoneFeatures: %v", err)
	}
	if len(features) != 27 {
		t.Fatalf("ListZoneFeatures returned %d, want 27", len(features))
	}
	for _, f := range features {
		var geometry map[string]any
		if err := json.Unmarshal(f.Geometry, &geometry); err != nil {
			t.Fatalf("zone %s geometry is not valid JSON: %v", f.Zone.Name, err)
		}
		if geometry["type"] == "" {
			t.Fatalf("zone %s geometry has no type", f.Zone.Name)
		}
	}

	alertFeatures, err := repo.ListAlertZoneFeatures(ctx)
	if err != nil {
		t.Fatalf("ListAlertZoneFeatures: %v", err)
	}
	if len(alertFeatures) != 27 {
		t.Fatalf("ListAlertZoneFeatures returned %d, want 27", len(alertFeatures))
	}
}

func TestSeedOblastsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewRepository(testPool(t))

	if _, err := repo.SeedOblasts(ctx); err != nil {
		t.Fatalf("first SeedOblasts: %v", err)
	}
	seeded, err := repo.SeedOblasts(ctx)
	if err != nil {
		t.Fatalf("second SeedOblasts: %v", err)
	}
	if seeded != 0 {
		t.Fatalf("second SeedOblasts inserted %d rows, want 0", seeded)
	}
}

func TestHistoryBatchInsertConflictAndRetention(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := postgres.NewRepository(pool)

	droneID := telemetry.DroneID("itest-history-001")
	recordedAt := time.Now().UTC().Truncate(time.Millisecond)
	sample := telemetry.Sample{
		DroneID:    droneID,
		Timestamp:  recordedAt,
		Latitude:   50.45,
		Longitude:  30.52,
		Altitude:   150,
		Speed:      20,
		Confidence: 88,
	}

	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM telemetry_history WHERE drone_id = $1`, string(droneID)).Scan(&n); err != nil {
			t.Fatalf("count history: %v", err)
		}
		return n
	}

	if err := repo.SaveHistoryBatch(ctx, []telemetry.Sample{sample}); err != nil {
		t.Fatalf("first SaveHistoryBatch: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("after first insert count = %d, want 1", got)
	}

	if err := repo.SaveHistoryBatch(ctx, []telemetry.Sample{sample}); err != nil {
		t.Fatalf("duplicate SaveHistoryBatch: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("after duplicate insert count = %d, want 1 (ON CONFLICT DO NOTHING)", got)
	}

	deleted, err := repo.DeleteHistoryBefore(ctx, recordedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("DeleteHistoryBefore: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("DeleteHistoryBefore removed %d rows, want at least 1", deleted)
	}
	if got := count(); got != 0 {
		t.Fatalf("after retention delete count = %d, want 0", got)
	}
}
