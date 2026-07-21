package replay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

var (
	ErrNoHistory      = errors.New("no recorded telemetry in the requested range")
	ErrTooManyReplays = errors.New("too many replays are already running")
	ErrInvalidRange   = errors.New("replay range is invalid: from must be before to")
	ErrInvalidSpeed   = errors.New("replay speed must be within [0.1, 1000]")
	ErrNotFound       = errors.New("replay not found")
)

const (
	MinSpeed             = 0.1
	MaxSpeed             = 1000.0
	DefaultSpeed         = 10.0
	DefaultMaxConcurrent = 4
)

type HistorySource interface {
	ListHistoryRange(ctx context.Context, from, to time.Time, droneID telemetry.DroneID, limit int) ([]telemetry.Sample, error)
}

type Publisher interface {
	Publish(ctx context.Context, sample telemetry.Sample) error
}

type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateCancelled State = "cancelled"
	StateFailed    State = "failed"
)

type Status struct {
	ID        string    `json:"id"`
	State     State     `json:"state"`
	Speed     float64   `json:"speed"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	DroneID   string    `json:"droneId,omitempty"`
	Total     int       `json:"total"`
	Published int       `json:"published"`
	StartedAt time.Time `json:"startedAt"`
}

type run struct {
	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
}

func (r *run) snapshot() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *run) setState(state State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State == StateRunning {
		r.status.State = state
	}
}

func (r *run) incrementPublished() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.Published++
}

type Manager struct {
	source          HistorySource
	publisher       Publisher
	logger          *slog.Logger
	maxConcurrent   int
	maxPoints       int
	mu              sync.Mutex
	runs            map[string]*run
	nextID          int64
	baseCtx         context.Context
	baseCancel      context.CancelFunc
	wg              sync.WaitGroup
	samplesReplayed atomic.Int64
}

func NewManager(source HistorySource, publisher Publisher, logger *slog.Logger, maxConcurrent, maxPoints int) *Manager {
	if maxConcurrent < 1 {
		maxConcurrent = DefaultMaxConcurrent
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	return &Manager{
		source:        source,
		publisher:     publisher,
		logger:        logger,
		maxConcurrent: maxConcurrent,
		maxPoints:     maxPoints,
		runs:          make(map[string]*run),
		baseCtx:       baseCtx,
		baseCancel:    baseCancel,
	}
}

type Request struct {
	From    time.Time
	To      time.Time
	Speed   float64
	DroneID telemetry.DroneID
}

func (m *Manager) Start(ctx context.Context, req Request) (Status, error) {
	if !req.From.Before(req.To) {
		return Status{}, ErrInvalidRange
	}
	if req.Speed == 0 {
		req.Speed = DefaultSpeed
	}
	if req.Speed < MinSpeed || req.Speed > MaxSpeed {
		return Status{}, ErrInvalidSpeed
	}

	samples, err := m.source.ListHistoryRange(ctx, req.From, req.To, req.DroneID, m.maxPoints)
	if err != nil {
		return Status{}, fmt.Errorf("load history for replay: %w", err)
	}
	if len(samples) == 0 {
		return Status{}, ErrNoHistory
	}

	m.mu.Lock()
	if m.runningLocked() >= m.maxConcurrent {
		m.mu.Unlock()
		return Status{}, ErrTooManyReplays
	}
	m.nextID++
	id := fmt.Sprintf("replay-%03d", m.nextID)
	runCtx, cancel := context.WithCancel(m.baseCtx)
	active := &run{
		status: Status{
			ID:        id,
			State:     StateRunning,
			Speed:     req.Speed,
			From:      req.From,
			To:        req.To,
			DroneID:   string(req.DroneID),
			Total:     len(samples),
			StartedAt: time.Now(),
		},
		cancel: cancel,
	}
	m.runs[id] = active
	m.mu.Unlock()

	m.logger.Info("replay started",
		"replay_id", id,
		"samples", len(samples),
		"speed", req.Speed,
		"drone_id", string(req.DroneID),
	)
	m.wg.Go(func() { m.play(runCtx, active, samples) })
	return active.snapshot(), nil
}

func (m *Manager) play(ctx context.Context, active *run, samples []telemetry.Sample) {
	defer active.cancel()
	id := active.snapshot().ID
	prefix := id + "/"
	base := samples[0].Timestamp
	speed := active.snapshot().Speed
	startedAt := time.Now()

	for _, sample := range samples {
		offset := time.Duration(float64(sample.Timestamp.Sub(base)) / speed)
		if wait := time.Until(startedAt.Add(offset)); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				active.setState(StateCancelled)
				m.logger.Info("replay cancelled", "replay_id", id, "published", active.snapshot().Published)
				return
			case <-timer.C:
			}
		} else if ctx.Err() != nil {
			active.setState(StateCancelled)
			return
		}

		replayed := sample
		replayed.DroneID = telemetry.DroneID(prefix + string(sample.DroneID))
		replayed.Timestamp = time.Now()
		if err := m.publisher.Publish(ctx, replayed); err != nil {
			active.setState(StateFailed)
			m.logger.Error("replay publish failed", "replay_id", id, "error", err)
			return
		}
		active.incrementPublished()
		m.samplesReplayed.Add(1)
	}
	active.setState(StateCompleted)
	m.logger.Info("replay completed", "replay_id", id, "published", active.snapshot().Published)
}

func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	statuses := make([]Status, 0, len(m.runs))
	for _, active := range m.runs {
		statuses = append(statuses, active.snapshot())
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return statuses
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	active, ok := m.runs[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	active.cancel()
	return nil
}

func (m *Manager) ActiveReplays() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningLocked()
}

func (m *Manager) SamplesReplayed() int64 {
	return m.samplesReplayed.Load()
}

func (m *Manager) runningLocked() int {
	count := 0
	for _, active := range m.runs {
		if active.snapshot().State == StateRunning {
			count++
		}
	}
	return count
}

func (m *Manager) Close() {
	m.baseCancel()
	m.wg.Wait()
}
