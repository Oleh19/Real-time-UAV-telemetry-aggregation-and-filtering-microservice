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

func TestListHistoryReturnsOrderedWindow(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := postgres.NewRepository(pool)

	droneID := telemetry.DroneID("itest-history-list-001")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM telemetry_history WHERE drone_id = $1`, string(droneID))
	})

	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	samples := make([]telemetry.Sample, 0, 5)
	for n := range 5 {
		samples = append(samples, telemetry.Sample{
			DroneID:    droneID,
			Timestamp:  base.Add(time.Duration(n) * time.Minute),
			Latitude:   50.0 + float64(n)*0.01,
			Longitude:  30.0 + float64(n)*0.01,
			Altitude:   100,
			Speed:      20,
			Confidence: 90,
		})
	}
	if err := repo.SaveHistoryBatch(ctx, samples); err != nil {
		t.Fatalf("SaveHistoryBatch: %v", err)
	}

	got, err := repo.ListHistory(ctx, droneID, base.Add(time.Minute), base.Add(3*time.Minute), 0)
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListHistory returned %d samples, want 3", len(got))
	}
	for n := 1; n < len(got); n++ {
		if got[n].Timestamp.Before(got[n-1].Timestamp) {
			t.Fatalf("samples out of order: %s before %s", got[n].Timestamp, got[n-1].Timestamp)
		}
	}
	if got[0].DroneID != droneID {
		t.Fatalf("DroneID = %s, want %s", got[0].DroneID, droneID)
	}
	if got[0].Latitude != 50.01 {
		t.Fatalf("first latitude = %f, want 50.01", got[0].Latitude)
	}
}

func TestListHistoryRangeAcrossDrones(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := postgres.NewRepository(pool)

	first := telemetry.DroneID("itest-range-001")
	second := telemetry.DroneID("itest-range-002")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM telemetry_history WHERE drone_id IN ($1, $2)`, string(first), string(second))
	})

	base := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	var samples []telemetry.Sample
	for n := range 3 {
		ts := base.Add(time.Duration(n) * time.Minute)
		samples = append(samples,
			telemetry.Sample{DroneID: first, Timestamp: ts, Latitude: 50, Longitude: 30, Altitude: 100, Speed: 20, Confidence: 90},
			telemetry.Sample{DroneID: second, Timestamp: ts.Add(time.Second), Latitude: 51, Longitude: 31, Altitude: 100, Speed: 20, Confidence: 90},
		)
	}
	if err := repo.SaveHistoryBatch(ctx, samples); err != nil {
		t.Fatalf("SaveHistoryBatch: %v", err)
	}

	all, err := repo.ListHistoryRange(ctx, base, base.Add(10*time.Minute), "", 0)
	if err != nil {
		t.Fatalf("ListHistoryRange: %v", err)
	}
	mine := 0
	for _, s := range all {
		if s.DroneID == first || s.DroneID == second {
			mine++
		}
	}
	if mine != 6 {
		t.Fatalf("range returned %d of our samples, want 6", mine)
	}
	for n := 1; n < len(all); n++ {
		if all[n].Timestamp.Before(all[n-1].Timestamp) {
			t.Fatal("range results are not ordered by recorded_at")
		}
	}

	filtered, err := repo.ListHistoryRange(ctx, base, base.Add(10*time.Minute), first, 0)
	if err != nil {
		t.Fatalf("filtered ListHistoryRange: %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("filtered range returned %d samples, want 3", len(filtered))
	}
	for _, s := range filtered {
		if s.DroneID != first {
			t.Fatalf("filtered range leaked drone %s", s.DroneID)
		}
	}
}

func TestCustomZoneLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := postgres.NewRepository(pool)

	zoneName := "itest-custom-zone-airfield"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM custom_zones WHERE name = $1`, zoneName)
	})

	square := [][2]float64{{30.0, 50.0}, {30.2, 50.0}, {30.2, 50.2}, {30.0, 50.2}}
	zone, err := repo.CreateCustomZone(ctx, zoneName, square)
	if err != nil {
		t.Fatalf("CreateCustomZone: %v", err)
	}
	if zone.ID <= postgres.CustomZoneIDOffset {
		t.Fatalf("zone ID = %d, want it offset above %d", zone.ID, postgres.CustomZoneIDOffset)
	}

	features, err := repo.ListCustomZoneFeatures(ctx)
	if err != nil {
		t.Fatalf("ListCustomZoneFeatures: %v", err)
	}
	found := false
	for _, f := range features {
		if f.Zone.ID == zone.ID && f.Zone.Name == zoneName {
			found = true
		}
	}
	if !found {
		t.Fatal("created zone is missing from ListCustomZoneFeatures")
	}

	alertFeatures, err := repo.ListAlertZoneFeatures(ctx)
	if err != nil {
		t.Fatalf("ListAlertZoneFeatures: %v", err)
	}
	inUnion := false
	for _, f := range alertFeatures {
		if f.Zone.ID == zone.ID {
			inUnion = true
		}
	}
	if !inUnion {
		t.Fatal("custom zone is missing from the alert zone union used by the geofence index")
	}

	if _, err := repo.CreateCustomZone(ctx, zoneName, [][2]float64{{30.0, 50.0}, {30.1, 50.1}}); err == nil {
		t.Fatal("CreateCustomZone accepted a 2-point polygon")
	}

	deleted, err := repo.DeleteCustomZone(ctx, zone.ID)
	if err != nil {
		t.Fatalf("DeleteCustomZone: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteCustomZone reported nothing deleted")
	}
	if deleted, _ := repo.DeleteCustomZone(ctx, zone.ID); deleted {
		t.Fatal("second DeleteCustomZone deleted something")
	}
}

func TestZoneBreachJournalRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	repo := postgres.NewRepository(pool)

	droneID := telemetry.DroneID("itest-breach-001")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM zone_breaches WHERE drone_id = $1`, string(droneID))
	})

	occurredAt := time.Now().UTC().Truncate(time.Millisecond)
	breach := telemetry.ZoneBreach{
		Zone: telemetry.Zone{ID: 7, Name: "Kyiv Oblast"},
		Sample: telemetry.Sample{
			DroneID:   droneID,
			Timestamp: occurredAt,
			Latitude:  50.45,
			Longitude: 30.52,
			Altitude:  120,
		},
		Event: telemetry.BreachEntered,
	}

	if err := repo.SaveZoneBreach(ctx, breach); err != nil {
		t.Fatalf("SaveZoneBreach: %v", err)
	}
	if err := repo.SaveZoneBreach(ctx, breach); err != nil {
		t.Fatalf("duplicate SaveZoneBreach: %v", err)
	}
	exit := breach
	exit.Event = telemetry.BreachExited
	exit.Sample.Timestamp = occurredAt.Add(time.Minute)
	if err := repo.SaveZoneBreach(ctx, exit); err != nil {
		t.Fatalf("SaveZoneBreach exit: %v", err)
	}

	records, err := repo.ListZoneBreaches(ctx, 10)
	if err != nil {
		t.Fatalf("ListZoneBreaches: %v", err)
	}
	var mine []postgres.BreachRecord
	for _, rec := range records {
		if rec.DroneID == droneID {
			mine = append(mine, rec)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("journal has %d records for %s, want 2 (duplicate deduped)", len(mine), droneID)
	}
	if mine[0].Event != telemetry.BreachExited {
		t.Errorf("newest record event = %s, want exited first (DESC order)", mine[0].Event)
	}
	if mine[1].ZoneName != "Kyiv Oblast" || mine[1].Latitude != 50.45 {
		t.Errorf("record = zone %q lat %f, want Kyiv Oblast 50.45", mine[1].ZoneName, mine[1].Latitude)
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
