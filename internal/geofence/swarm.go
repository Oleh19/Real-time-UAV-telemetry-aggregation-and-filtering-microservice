package geofence

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/telemetry"
)

type SwarmConfig struct {
	RadiusMeters float64
	MinSize      int
	EvalInterval time.Duration
	PositionTTL  time.Duration
}

func DefaultSwarmConfig() SwarmConfig {
	return SwarmConfig{
		RadiusMeters: 5000,
		MinSize:      3,
		EvalInterval: 5 * time.Second,
		PositionTTL:  30 * time.Second,
	}
}

type Swarm struct {
	ID         string    `json:"id"`
	DroneIDs   []string  `json:"droneIds"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	DetectedAt time.Time `json:"detectedAt"`
}

type trackedPosition struct {
	sample telemetry.Sample
	seenAt time.Time
}

type swarmState struct {
	swarm   Swarm
	members map[telemetry.DroneID]struct{}
}

type SwarmDetector struct {
	cfg           SwarmConfig
	logger        *slog.Logger
	mu            sync.Mutex
	positions     map[telemetry.DroneID]trackedPosition
	active        map[string]*swarmState
	nextSwarm     int64
	detectedTotal atomic.Int64
}

func NewSwarmDetector(cfg SwarmConfig, logger *slog.Logger) *SwarmDetector {
	defaults := DefaultSwarmConfig()
	if cfg.RadiusMeters <= 0 {
		cfg.RadiusMeters = defaults.RadiusMeters
	}
	if cfg.MinSize < 2 {
		cfg.MinSize = defaults.MinSize
	}
	if cfg.EvalInterval <= 0 {
		cfg.EvalInterval = defaults.EvalInterval
	}
	if cfg.PositionTTL <= 0 {
		cfg.PositionTTL = defaults.PositionTTL
	}
	return &SwarmDetector{
		cfg:       cfg,
		logger:    logger,
		positions: make(map[telemetry.DroneID]trackedPosition),
		active:    make(map[string]*swarmState),
	}
}

func (d *SwarmDetector) Run(ctx context.Context, consumer jetstream.Consumer) error {
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		if sample, ok := decodeSample(msg.Data(), d.logger); ok {
			d.Observe(sample)
		}
		if err := msg.Ack(); err != nil {
			d.logger.Error("ack swarm telemetry message", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("consume telemetry for swarm detection: %w", err)
	}
	defer consumeCtx.Stop()

	d.logger.Info("swarm detector started",
		"radius_m", d.cfg.RadiusMeters,
		"min_size", d.cfg.MinSize,
		"eval_interval", d.cfg.EvalInterval.String(),
	)

	ticker := time.NewTicker(d.cfg.EvalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			d.Evaluate(now)
		}
	}
}

func (d *SwarmDetector) Observe(sample telemetry.Sample) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.positions[sample.DroneID] = trackedPosition{sample: sample, seenAt: time.Now()}
}

func (d *SwarmDetector) Evaluate(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pruneLocked(now)

	ids := make([]telemetry.DroneID, 0, len(d.positions))
	points := make([]clusterPoint, 0, len(d.positions))
	for id, pos := range d.positions {
		ids = append(ids, id)
		points = append(points, clusterPoint{latitude: pos.sample.Latitude, longitude: pos.sample.Longitude})
	}
	clusters := clusterPoints(points, d.cfg.RadiusMeters, d.cfg.MinSize)

	next := make(map[string]*swarmState, len(clusters))
	for _, cluster := range clusters {
		members := make(map[telemetry.DroneID]struct{}, len(cluster))
		var latSum, lonSum float64
		for _, index := range cluster {
			members[ids[index]] = struct{}{}
			latSum += points[index].latitude
			lonSum += points[index].longitude
		}
		state := d.matchLocked(members)
		if state == nil {
			d.nextSwarm++
			state = &swarmState{swarm: Swarm{
				ID:         fmt.Sprintf("swarm-%03d", d.nextSwarm),
				DetectedAt: now,
			}}
			d.detectedTotal.Add(1)
			d.logger.Error(
				fmt.Sprintf("SWARM DETECTED: %d targets moving in a compact group!", len(members)),
				"swarm_id", state.swarm.ID,
				"size", len(members),
			)
		}
		state.members = members
		state.swarm.DroneIDs = sortedIDs(members)
		state.swarm.Latitude = latSum / float64(len(cluster))
		state.swarm.Longitude = lonSum / float64(len(cluster))
		next[state.swarm.ID] = state
	}

	for id, state := range d.active {
		if _, alive := next[id]; !alive {
			d.logger.Info("swarm dissolved", "swarm_id", id, "size", len(state.members))
		}
	}
	d.active = next
}

func (d *SwarmDetector) matchLocked(members map[telemetry.DroneID]struct{}) *swarmState {
	var best *swarmState
	bestOverlap := 0
	for _, state := range d.active {
		overlap := 0
		for id := range members {
			if _, ok := state.members[id]; ok {
				overlap++
			}
		}
		if overlap > bestOverlap {
			best = state
			bestOverlap = overlap
		}
	}
	return best
}

func (d *SwarmDetector) pruneLocked(now time.Time) {
	cutoff := now.Add(-d.cfg.PositionTTL)
	for id, pos := range d.positions {
		if pos.seenAt.Before(cutoff) {
			delete(d.positions, id)
		}
	}
}

func (d *SwarmDetector) Snapshot() []Swarm {
	d.mu.Lock()
	defer d.mu.Unlock()
	swarms := make([]Swarm, 0, len(d.active))
	for _, state := range d.active {
		swarms = append(swarms, state.swarm)
	}
	sort.Slice(swarms, func(i, j int) bool { return swarms[i].ID < swarms[j].ID })
	return swarms
}

func (d *SwarmDetector) ActiveSwarms() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.active)
}

func (d *SwarmDetector) DetectedTotal() int64 {
	return d.detectedTotal.Load()
}

func sortedIDs(members map[telemetry.DroneID]struct{}) []string {
	ids := make([]string, 0, len(members))
	for id := range members {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	return ids
}
