package geofence

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

type CustomZoneSource interface {
	ListCustomZones(ctx context.Context) ([]telemetry.Zone, error)
}

type CustomZoneCache struct {
	current atomic.Pointer[[]telemetry.Zone]
}

func NewCustomZoneCache() *CustomZoneCache {
	c := &CustomZoneCache{}
	empty := make([]telemetry.Zone, 0)
	c.current.Store(&empty)
	return c
}

func (c *CustomZoneCache) Zones() []telemetry.Zone {
	return *c.current.Load()
}

func (c *CustomZoneCache) Refresh(ctx context.Context, source CustomZoneSource) error {
	zones, err := source.ListCustomZones(ctx)
	if err != nil {
		return err
	}
	c.current.Store(&zones)
	return nil
}

func (c *CustomZoneCache) Run(ctx context.Context, source CustomZoneSource, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Refresh(ctx, source); err != nil {
				logger.Error("refresh custom zone registry", "error", err)
			}
		}
	}
}
