package stations

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"uavmonitor/internal/telemetry"
)

type Status string

const (
	StatusOnline  Status = "online"
	StatusStale   Status = "stale"
	StatusOffline Status = "offline"
)

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
	lastSeen     time.Time
	samples      int64
	rateSamples  int64
	rateSince    time.Time
	rate         float64
	lastReported Status
}

type Registry struct {
	cfg      Config
	logger   *slog.Logger
	mu       sync.Mutex
	stations map[telemetry.StationID]*stationState
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
	return &Registry{
		cfg:      cfg,
		logger:   logger,
		stations: make(map[telemetry.StationID]*stationState),
	}
}

func (r *Registry) Observe(station telemetry.StationID) {
	if station == "" {
		return
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.stations[station]
	if !ok {
		state = &stationState{rateSince: now}
		r.stations[station] = state
		r.logger.Info("station appeared", "station_id", station)
		state.lastReported = StatusOnline
	}
	state.lastSeen = now
	state.samples++
	state.rateSamples++
	if window := now.Sub(state.rateSince); window >= 10*time.Second {
		state.rate = float64(state.rateSamples) / window.Seconds()
		state.rateSamples = 0
		state.rateSince = now
	}
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
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, state := range r.stations {
		if now.Sub(state.lastSeen) > r.cfg.ForgetAfter {
			delete(r.stations, id)
			continue
		}
		status := r.statusLocked(state, now)
		if status == state.lastReported {
			continue
		}
		switch status {
		case StatusOnline:
			r.logger.Info("station recovered", "station_id", id)
		case StatusStale:
			r.logger.Warn("station went silent", "station_id", id, "last_seen", state.lastSeen)
		case StatusOffline:
			r.logger.Error("station offline", "station_id", id, "last_seen", state.lastSeen)
		}
		state.lastReported = status
	}
}

func (r *Registry) statusLocked(state *stationState, now time.Time) Status {
	age := now.Sub(state.lastSeen)
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
	r.mu.Lock()
	defer r.mu.Unlock()
	infos := make([]Info, 0, len(r.stations))
	for id, state := range r.stations {
		rate := state.rate
		if window := now.Sub(state.rateSince); rate == 0 && window > 0 {
			rate = float64(state.rateSamples) / window.Seconds()
		}
		infos = append(infos, Info{
			ID:            id,
			Status:        r.statusLocked(state, now),
			LastSeen:      state.lastSeen,
			Samples:       state.samples,
			RatePerSecond: rate,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

func (r *Registry) Counts() (online, silent int) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.stations {
		if r.statusLocked(state, now) == StatusOnline {
			online++
		} else {
			silent++
		}
	}
	return online, silent
}
