package stations

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

type Status string

const (
	StatusOnline  Status = "online"
	StatusStale   Status = "stale"
	StatusOffline Status = "offline"
)

const rateWindow = 10 * time.Second

type Config struct {
	OnlineWithin  time.Duration
	OfflineAfter  time.Duration
	ForgetAfter   time.Duration
	CheckInterval time.Duration
}

func DefaultConfig() Config {
	return Config{
		OnlineWithin:  10 * time.Second,
		OfflineAfter:  time.Minute,
		ForgetAfter:   24 * time.Hour,
		CheckInterval: 5 * time.Second,
	}
}

type Info struct {
	ID            telemetry.StationID `json:"id"`
	Status        Status              `json:"status"`
	LastSeen      time.Time           `json:"lastSeen"`
	Samples       int64               `json:"samples"`
	RatePerSecond float64             `json:"ratePerSecond"`
}

type stationState struct {
	lastSeen     atomic.Int64
	samples      atomic.Int64
	windowStart  atomic.Int64
	windowCount  atomic.Int64
	lastReported Status
}

func (s *stationState) rate(nowNano int64) float64 {
	elapsed := float64(nowNano-s.windowStart.Load()) / float64(time.Second)
	if elapsed <= 0 {
		return 0
	}
	return float64(s.windowCount.Load()) / elapsed
}

type Registry struct {
	cfg      Config
	logger   *slog.Logger
	stations sync.Map
}

func NewRegistry(cfg Config, logger *slog.Logger) *Registry {
	defaults := DefaultConfig()
	if cfg.OnlineWithin <= 0 {
		cfg.OnlineWithin = defaults.OnlineWithin
	}
	if cfg.OfflineAfter <= cfg.OnlineWithin {
		cfg.OfflineAfter = defaults.OfflineAfter
	}
	if cfg.ForgetAfter <= cfg.OfflineAfter {
		cfg.ForgetAfter = defaults.ForgetAfter
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = defaults.CheckInterval
	}
	return &Registry{cfg: cfg, logger: logger}
}

func (r *Registry) Observe(station telemetry.StationID) {
	if station == "" {
		return
	}
	nowNano := time.Now().UnixNano()
	state := r.load(station, nowNano)
	state.samples.Add(1)
	state.windowCount.Add(1)
	state.lastSeen.Store(nowNano)
}

func (r *Registry) load(station telemetry.StationID, nowNano int64) *stationState {
	if v, ok := r.stations.Load(station); ok {
		return v.(*stationState)
	}
	fresh := &stationState{lastReported: StatusOnline}
	fresh.windowStart.Store(nowNano)
	fresh.lastSeen.Store(nowNano)
	if actual, loaded := r.stations.LoadOrStore(station, fresh); loaded {
		return actual.(*stationState)
	}
	r.logger.Info("station appeared", "station_id", station)
	return fresh
}

func (r *Registry) Run(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.reportTransitions(now)
		}
	}
}

func (r *Registry) reportTransitions(now time.Time) {
	nowNano := now.UnixNano()
	r.stations.Range(func(key, value any) bool {
		id := key.(telemetry.StationID)
		state := value.(*stationState)
		if now.Sub(time.Unix(0, state.lastSeen.Load())) > r.cfg.ForgetAfter {
			r.stations.Delete(id)
			return true
		}
		if nowNano-state.windowStart.Load() >= int64(rateWindow) {
			state.windowStart.Store(nowNano)
			state.windowCount.Store(0)
		}
		status := r.statusOf(state, now)
		if status == state.lastReported {
			return true
		}
		switch status {
		case StatusOnline:
			r.logger.Info("station recovered", "station_id", id)
		case StatusStale:
			r.logger.Warn("station went silent", "station_id", id)
		case StatusOffline:
			r.logger.Error("station offline", "station_id", id)
		}
		state.lastReported = status
		return true
	})
}

func (r *Registry) statusOf(state *stationState, now time.Time) Status {
	age := now.Sub(time.Unix(0, state.lastSeen.Load()))
	switch {
	case age <= r.cfg.OnlineWithin:
		return StatusOnline
	case age <= r.cfg.OfflineAfter:
		return StatusStale
	default:
		return StatusOffline
	}
}

func (r *Registry) Snapshot() []Info {
	now := time.Now()
	nowNano := now.UnixNano()
	infos := make([]Info, 0)
	r.stations.Range(func(key, value any) bool {
		state := value.(*stationState)
		infos = append(infos, Info{
			ID:            key.(telemetry.StationID),
			Status:        r.statusOf(state, now),
			LastSeen:      time.Unix(0, state.lastSeen.Load()),
			Samples:       state.samples.Load(),
			RatePerSecond: math.Round(state.rate(nowNano)*10) / 10,
		})
		return true
	})
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

func (r *Registry) Counts() (online, silent int) {
	now := time.Now()
	r.stations.Range(func(_, value any) bool {
		if r.statusOf(value.(*stationState), now) == StatusOnline {
			online++
		} else {
			silent++
		}
		return true
	})
	return online, silent
}
