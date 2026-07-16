package geofence

import (
	"context"
	"log/slog"
	"time"
)

type HistoryPruner interface {
	DeleteHistoryBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

func RunRetention(ctx context.Context, pruner HistoryPruner, retention time.Duration, logger *slog.Logger) {
	interval := retention / 10
	if interval > time.Hour {
		interval = time.Hour
	}
	if interval < time.Minute {
		interval = time.Minute
	}

	prune := func(now time.Time) {
		cutoff := now.Add(-retention)
		deleted, err := pruner.DeleteHistoryBefore(ctx, cutoff)
		if err != nil {
			logger.Error("prune telemetry history", "error", err)
			return
		}
		if deleted > 0 {
			logger.Info("pruned telemetry history", "deleted", deleted, "cutoff", cutoff)
		}
	}

	prune(time.Now())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			prune(now)
		}
	}
}
