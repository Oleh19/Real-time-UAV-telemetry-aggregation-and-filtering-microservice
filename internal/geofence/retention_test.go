package geofence_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"uavmonitor/internal/geofence"
)

type fakePruner struct {
	mu      sync.Mutex
	cutoffs []time.Time
}

func (p *fakePruner) DeleteHistoryBefore(_ context.Context, cutoff time.Time) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cutoffs = append(p.cutoffs, cutoff)
	return 1, nil
}

func (p *fakePruner) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cutoffs)
}

func TestRunRetentionPrunesImmediately(t *testing.T) {
	pruner := &fakePruner{}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		geofence.RunRetention(ctx, pruner, 24*time.Hour, discardLogger())
		close(done)
	}()

	if !eventually(func() bool { return pruner.count() > 0 }) {
		t.Error("expected an immediate prune on startup")
	}
	cancel()
	<-done
}
