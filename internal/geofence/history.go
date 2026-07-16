package geofence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/telemetry"
)

type HistoryRepository interface {
	SaveHistoryBatch(ctx context.Context, samples []telemetry.Sample) error
}

const finalFlushTimeout = 5 * time.Second

type HistoryWriter struct {
	repo   HistoryRepository
	logger *slog.Logger
}

func NewHistoryWriter(repo HistoryRepository, logger *slog.Logger) *HistoryWriter {
	return &HistoryWriter{repo: repo, logger: logger}
}

func (h *HistoryWriter) Run(ctx context.Context, consumer jetstream.Consumer, batchSize int, flushInterval time.Duration) error {
	messages := make(chan jetstream.Msg, batchSize)
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		select {
		case <-ctx.Done():
		case messages <- msg:
		}
	})
	if err != nil {
		return fmt.Errorf("consume telemetry for history: %w", err)
	}
	defer consumeCtx.Stop()

	h.logger.Info("history writer started",
		"batch_size", batchSize,
		"flush_interval", flushInterval.String(),
	)

	batch := make([]jetstream.Msg, 0, batchSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), finalFlushTimeout)
			h.flush(flushCtx, batch)
			cancel()
			return nil
		case msg := <-messages:
			batch = append(batch, msg)
			if len(batch) >= batchSize {
				h.flush(ctx, batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				h.flush(ctx, batch)
				batch = batch[:0]
			}
		}
	}
}

func (h *HistoryWriter) flush(ctx context.Context, batch []jetstream.Msg) {
	if len(batch) == 0 {
		return
	}
	samples := make([]telemetry.Sample, 0, len(batch))
	valid := make([]jetstream.Msg, 0, len(batch))
	for _, msg := range batch {
		sample, ok := decodeSample(msg.Data(), h.logger)
		if !ok {
			if err := msg.Term(); err != nil {
				h.logger.Error("terminate malformed message", "error", err)
			}
			continue
		}
		samples = append(samples, sample)
		valid = append(valid, msg)
	}
	if len(samples) == 0 {
		return
	}
	if err := h.repo.SaveHistoryBatch(ctx, samples); err != nil {
		h.logger.Error("save telemetry history batch", "batch_size", len(samples), "error", err)
		for _, msg := range valid {
			if nakErr := msg.NakWithDelay(redeliveryDelay); nakErr != nil {
				h.logger.Error("nak history message", "error", nakErr)
			}
		}
		return
	}
	for _, msg := range valid {
		if err := msg.Ack(); err != nil {
			h.logger.Error("ack history message", "error", err)
		}
	}
}
