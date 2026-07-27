package replay

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
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
	ErrNotRunning     = errors.New("replay is not running")
)

const (
	MinSpeed             = 0.1
	MaxSpeed             = 1000.0
	DefaultSpeed         = 10.0
	DefaultMaxConcurrent = 4
	maxFinishedRuns      = 20
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
	Paused    bool      `json:"paused"`
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

func (r *run) control() (speed float64, paused bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status.Speed, r.status.Paused
}

func (r *run) setSpeed(speed float64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != StateRunning {
		return false
	}
	r.status.Speed = speed
	return true
}

func (r *run) setPaused(paused bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != StateRunning {
		return false
	}
	r.status.Paused = paused
	return true
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
	m.pruneFinishedLocked()
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

const replayTick = 50 * time.Millisecond

func (m *Manager) play(ctx context.Context, active *run, samples []telemetry.Sample) {
	defer active.cancel()
	id := active.snapshot().ID
	prefix := id + "/"
	base := samples[0].Timestamp

	ticker := time.NewTicker(replayTick)
	defer ticker.Stop()

	var cursor time.Duration
	i := 0
	for i < len(samples) {
		for i < len(samples) && samples[i].Timestamp.Sub(base) <= cursor {
			replayed := samples[i]
			replayed.DroneID = telemetry.DroneID(prefix + string(samples[i].DroneID))
			replayed.Timestamp = time.Now()
			if err := m.publisher.Publish(ctx, replayed); err != nil {
				active.setState(StateFailed)
				m.logger.Error("replay publish failed", "replay_id", id, "error", err)
				return
			}
			active.incrementPublished()
			m.samplesReplayed.Add(1)
			i++
		}
		if i >= len(samples) {
			break
		}
		select {
		case <-ctx.Done():
			active.setState(StateCancelled)
			m.logger.Info("replay cancelled", "replay_id", id, "published", active.snapshot().Published)
			return
		case <-ticker.C:
			if speed, paused := active.control(); !paused {
				cursor += time.Duration(float64(replayTick) * speed)
			}
		}
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
	slices.SortFunc(statuses, func(a, b Status) int { return cmp.Compare(a.ID, b.ID) })
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

func (m *Manager) SetSpeed(id string, speed float64) (Status, error) {
	if speed < MinSpeed || speed > MaxSpeed {
		return Status{}, ErrInvalidSpeed
	}
	m.mu.Lock()
	active, ok := m.runs[id]
	m.mu.Unlock()
	if !ok {
		return Status{}, ErrNotFound
	}
	if !active.setSpeed(speed) {
		return Status{}, ErrNotRunning
	}
	return active.snapshot(), nil
}

func (m *Manager) SetPaused(id string, paused bool) (Status, error) {
	m.mu.Lock()
	active, ok := m.runs[id]
	m.mu.Unlock()
	if !ok {
		return Status{}, ErrNotFound
	}
	if !active.setPaused(paused) {
		return Status{}, ErrNotRunning
	}
	return active.snapshot(), nil
}

func (m *Manager) ActiveReplays() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningLocked()
}

func (m *Manager) SamplesReplayed() int64 {
	return m.samplesReplayed.Load()
}

func (m *Manager) pruneFinishedLocked() {
	var finished []string
	for id, active := range m.runs {
		if active.snapshot().State != StateRunning {
			finished = append(finished, id)
		}
	}
	if len(finished) <= maxFinishedRuns {
		return
	}
	slices.SortFunc(finished, func(a, b string) int {
		return cmp.Compare(replaySeq(a), replaySeq(b))
	})
	for _, id := range finished[:len(finished)-maxFinishedRuns] {
		delete(m.runs, id)
	}
}

func replaySeq(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "replay-"))
	if err != nil {
		return 0
	}
	return n
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
