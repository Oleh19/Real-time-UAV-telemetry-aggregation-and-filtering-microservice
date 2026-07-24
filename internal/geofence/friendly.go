package geofence

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type FriendlySource interface {
	ListFriendlyCodes(ctx context.Context) ([]string, error)
}

type FriendlyCache struct {
	current atomic.Pointer[map[string]struct{}]
}

func NewFriendlyCache() *FriendlyCache {
	c := &FriendlyCache{}
	empty := make(map[string]struct{})
	c.current.Store(&empty)
	return c
}

func (c *FriendlyCache) IsFriendly(squawk string) bool {
	if squawk == "" {
		return false
	}
	set := c.current.Load()
	_, ok := (*set)[squawk]
	return ok
}

func (c *FriendlyCache) Refresh(ctx context.Context, source FriendlySource) error {
	codes, err := source.ListFriendlyCodes(ctx)
	if err != nil {
		return err
	}
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	c.current.Store(&set)
	return nil
}

func (c *FriendlyCache) Run(ctx context.Context, source FriendlySource, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Refresh(ctx, source); err != nil {
				logger.Error("refresh friendly registry", "error", err)
			}
		}
	}
}
