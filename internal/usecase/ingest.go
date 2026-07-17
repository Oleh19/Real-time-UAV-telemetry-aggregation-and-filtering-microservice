package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"uavmonitor/internal/telemetry"
)

var (
	ErrQueueFull     = errors.New("ingest queue is full")
	ErrShutdown      = errors.New("ingestor is shut down")
	ErrInvalidSample = errors.New("invalid telemetry sample")
)

type Publisher interface {
	Publish(ctx context.Context, sample telemetry.Sample) error
}

type lastEntry struct {
	sample   telemetry.Sample
	storedAt time.Time
}

type Ingestor struct {
	publisher Publisher
	logger    *slog.Logger
	queue     chan telemetry.Sample
	done      chan struct{}
	stateTTL  time.Duration
	lastState sync.Map
	wg        sync.WaitGroup
	mu        sync.RWMutex
	closed    bool
	received  atomic.Int64
	dropped   atomic.Int64
	published atomic.Int64
	failed    atomic.Int64
	rejected  atomic.Int64
}

func NewIngestor(publisher Publisher, logger *slog.Logger, queueSize int, stateTTL time.Duration) *Ingestor {
	return &Ingestor{
		publisher: publisher,
		logger:    logger,
		queue:     make(chan telemetry.Sample, queueSize),
		done:      make(chan struct{}),
		stateTTL:  stateTTL,
	}
}

func (i *Ingestor) Start(ctx context.Context, workerCount int) {
	for n := 0; n < workerCount; n++ {
		i.wg.Add(1)
		go i.worker(ctx)
	}
	i.wg.Add(1)
	go i.evictStaleState(ctx)
}

func (i *Ingestor) worker(ctx context.Context) {
	defer i.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case sample, ok := <-i.queue:
			if !ok {
				return
			}
			if err := i.publisher.Publish(ctx, sample); err != nil {
				i.failed.Add(1)
				i.logger.Error("publish telemetry", "drone_id", sample.DroneID, "error", err)
				continue
			}
			i.published.Add(1)
		}
	}
}

func (i *Ingestor) evictStaleState(ctx context.Context) {
	defer i.wg.Done()
	interval := i.stateTTL / 2
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-i.done:
			return
		case now := <-ticker.C:
			cutoff := now.Add(-i.stateTTL)
			i.lastState.Range(func(key, v any) bool {
				if entry, ok := v.(lastEntry); ok && entry.storedAt.Before(cutoff) {
					i.lastState.Delete(key)
				}
				return true
			})
		}
	}
}

func (i *Ingestor) Submit(ctx context.Context, sample telemetry.Sample) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("submit telemetry: %w", err)
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return ErrShutdown
	}
	i.received.Add(1)
	if err := sample.Validate(); err != nil {
		i.rejected.Add(1)
		return fmt.Errorf("%w: %w", ErrInvalidSample, err)
	}
	i.storeLastKnown(sample)
	select {
	case i.queue <- sample:
		return nil
	default:
		i.dropped.Add(1)
		return ErrQueueFull
	}
}

func (i *Ingestor) storeLastKnown(sample telemetry.Sample) {
	entry := lastEntry{sample: sample, storedAt: time.Now()}
	for {
		existing, loaded := i.lastState.Load(sample.DroneID)
		if !loaded {
			if _, raced := i.lastState.LoadOrStore(sample.DroneID, entry); !raced {
				return
			}
			continue
		}
		current, ok := existing.(lastEntry)
		if ok && !sample.Timestamp.After(current.sample.Timestamp) {
			return
		}
		if i.lastState.CompareAndSwap(sample.DroneID, existing, entry) {
			return
		}
	}
}

func (i *Ingestor) LastKnown(id telemetry.DroneID) (telemetry.Sample, bool) {
	v, ok := i.lastState.Load(id)
	if !ok {
		return telemetry.Sample{}, false
	}
	entry, ok := v.(lastEntry)
	return entry.sample, ok
}

func (i *Ingestor) Snapshot() []telemetry.Sample {
	samples := make([]telemetry.Sample, 0)
	i.lastState.Range(func(_, v any) bool {
		if entry, ok := v.(lastEntry); ok {
			samples = append(samples, entry.sample)
		}
		return true
	})
	return samples
}

type Stats struct {
	Received  int64
	Dropped   int64
	Published int64
	Failed    int64
	Rejected  int64
}

func (i *Ingestor) Stats() Stats {
	return Stats{
		Received:  i.received.Load(),
		Dropped:   i.dropped.Load(),
		Published: i.published.Load(),
		Failed:    i.failed.Load(),
		Rejected:  i.rejected.Load(),
	}
}

func (i *Ingestor) Shutdown() {
	i.mu.Lock()
	alreadyClosed := i.closed
	i.closed = true
	if !alreadyClosed {
		close(i.queue)
		close(i.done)
	}
	i.mu.Unlock()
	i.wg.Wait()
}
