package main

import (
	"context"
	"log/slog"
	"time"

	"uavmonitor/internal/repository/postgres"
)

const (
	shardStateInterval = time.Second
	shardStateFresh    = 5 * time.Second
)

func publishShardState(ctx context.Context, deps *dependencies, replica string, logger *slog.Logger) {
	ticker := time.NewTicker(shardStateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeCtx, cancel := context.WithTimeout(ctx, shardStateInterval)
			if err := deps.repo.PublishZonePresence(writeCtx, replica, deps.checker.ActiveAlarms()); err != nil {
				logger.Error("publish zone presence", "error", err)
			}
			if err := deps.repo.PublishSwarms(writeCtx, replica, swarmSnapshots(deps)); err != nil {
				logger.Error("publish swarms", "error", err)
			}
			cancel()
		}
	}
}

func swarmSnapshots(deps *dependencies) []postgres.SwarmSnapshot {
	swarms := deps.swarmDetector.Snapshot()
	out := make([]postgres.SwarmSnapshot, 0, len(swarms))
	for _, swarm := range swarms {
		out = append(out, postgres.SwarmSnapshot{
			ID:         swarm.ID,
			DroneIDs:   swarm.DroneIDs,
			Latitude:   swarm.Latitude,
			Longitude:  swarm.Longitude,
			DetectedAt: swarm.DetectedAt,
		})
	}
	return out
}
