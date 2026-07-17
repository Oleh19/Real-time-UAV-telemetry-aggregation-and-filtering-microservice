//go:build integration

package geofence_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/geofence"
	"uavmonitor/internal/repository/postgres"
)

func TestZoneIndexFromRealOblasts(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := postgres.NewRepository(pool)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := repo.SeedOblasts(ctx); err != nil {
		t.Fatalf("SeedOblasts: %v", err)
	}

	index, err := geofence.NewRefreshingZoneIndex(ctx, repo)
	if err != nil {
		t.Fatalf("NewRefreshingZoneIndex: %v", err)
	}

	kyiv := index.Containing(30.52, 50.45)
	if len(kyiv) == 0 {
		t.Fatal("Kyiv coordinates matched no oblast alert zone")
	}
	found := false
	for _, zone := range kyiv {
		if strings.Contains(zone.Name, "Kyiv") {
			found = true
		}
	}
	if !found {
		t.Errorf("Kyiv coordinates matched %v, expected a Kyiv oblast", kyiv)
	}

	if outside := index.Containing(2.35, 48.85); len(outside) != 0 {
		t.Errorf("Paris coordinates matched %v, expected no oblast", outside)
	}
}
