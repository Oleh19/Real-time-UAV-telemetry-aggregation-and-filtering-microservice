package geofence_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"uavmonitor/internal/geofence"
)

func startHistoryWriter(t *testing.T, repo *fakeHistoryRepo, batchSize int, flushInterval time.Duration) (*fakeConsumer, context.CancelFunc, *sync.WaitGroup) {
	t.Helper()
	consumer := &fakeConsumer{}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := geofence.NewHistoryWriter(repo, discardLogger()).Run(ctx, consumer, batchSize, flushInterval); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	if !eventually(2*time.Second, consumer.handlerRegistered) {
		t.Fatal("consumer handler was not registered")
	}
	return consumer, cancel, &wg
}

func TestHistoryWriterFlushesFullBatch(t *testing.T) {
	repo := &fakeHistoryRepo{}
	consumer, cancel, wg := startHistoryWriter(t, repo, 2, time.Hour)
	defer func() { cancel(); wg.Wait() }()

	first := &fakeMsg{data: payloadAt("drone-001", time.Now())}
	second := &fakeMsg{data: payloadAt("drone-002", time.Now())}
	consumer.push(first)
	consumer.push(second)

	if !eventually(2*time.Second, func() bool {
		a1, _, _ := first.state()
		a2, _, _ := second.state()
		return a1 && a2
	}) {
		t.Fatal("full batch was not flushed and acked")
	}
	if got := repo.batchCount(); got != 1 {
		t.Errorf("batches saved = %d, want 1", got)
	}
}

func TestHistoryWriterFlushesOnInterval(t *testing.T) {
	repo := &fakeHistoryRepo{}
	consumer, cancel, wg := startHistoryWriter(t, repo, 100, 50*time.Millisecond)
	defer func() { cancel(); wg.Wait() }()

	msg := &fakeMsg{data: payloadAt("drone-001", time.Now())}
	consumer.push(msg)

	if !eventually(2*time.Second, func() bool { acked, _, _ := msg.state(); return acked }) {
		t.Fatal("partial batch was not flushed by timer")
	}
}

func TestHistoryWriterTerminatesMalformedMessages(t *testing.T) {
	repo := &fakeHistoryRepo{}
	consumer, cancel, wg := startHistoryWriter(t, repo, 1, time.Hour)
	defer func() { cancel(); wg.Wait() }()

	msg := &fakeMsg{data: []byte{0xff, 0xff, 0xff}}
	consumer.push(msg)

	if !eventually(2*time.Second, func() bool { _, _, termed := msg.state(); return termed }) {
		t.Fatal("malformed message was not terminated")
	}
	if got := repo.batchCount(); got != 0 {
		t.Errorf("batches saved = %d, want 0", got)
	}
}

func TestHistoryWriterNaksBatchOnSaveFailure(t *testing.T) {
	repo := &fakeHistoryRepo{err: errors.New("insert failed")}
	consumer, cancel, wg := startHistoryWriter(t, repo, 1, time.Hour)
	defer func() { cancel(); wg.Wait() }()

	msg := &fakeMsg{data: payloadAt("drone-001", time.Now())}
	consumer.push(msg)

	if !eventually(2*time.Second, func() bool { _, naked, _ := msg.state(); return naked }) {
		t.Fatal("message was not naked on save failure")
	}
}
