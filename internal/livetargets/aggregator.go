package livetargets

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type entry struct {
	sample telemetry.Sample
	stored time.Time
}

type Store struct {
	ttl     time.Duration
	targets sync.Map
}

func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Store{ttl: ttl}
}

func (s *Store) Observe(sample telemetry.Sample) {
	now := time.Now()
	for {
		existing, loaded := s.targets.Load(sample.DroneID)
		next := entry{sample: sample, stored: now}
		if !loaded {
			if _, raced := s.targets.LoadOrStore(sample.DroneID, next); !raced {
				return
			}
			continue
		}
		current := existing.(entry)
		if sample.Timestamp.Before(current.sample.Timestamp) {
			return
		}
		if s.targets.CompareAndSwap(sample.DroneID, existing, next) {
			return
		}
	}
}

func (s *Store) Snapshot() []telemetry.Sample {
	samples := make([]telemetry.Sample, 0)
	s.targets.Range(func(_, value any) bool {
		samples = append(samples, value.(entry).sample)
		return true
	})
	return samples
}

func (s *Store) Count() int {
	count := 0
	s.targets.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

func (s *Store) EvictLoop(ctx context.Context) {
	interval := s.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			cutoff := now.Add(-s.ttl)
			s.targets.Range(func(key, value any) bool {
				if value.(entry).stored.Before(cutoff) {
					s.targets.Delete(key)
				}
				return true
			})
		}
	}
}

func Run(ctx context.Context, consumer jetstream.Consumer, store *Store, logger *slog.Logger) error {
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		if sample, ok := decode(msg.Data()); ok {
			store.Observe(sample)
		}
	})
	if err != nil {
		return fmt.Errorf("consume fused telemetry for live targets: %w", err)
	}
	defer consumeCtx.Stop()
	logger.Info("live target aggregator started")
	<-ctx.Done()
	return nil
}

func decode(payload []byte) (telemetry.Sample, bool) {
	var pb telemetryv1.DroneTelemetry
	if err := proto.Unmarshal(payload, &pb); err != nil {
		return telemetry.Sample{}, false
	}
	return telemetry.Sample{
		DroneID:    telemetry.DroneID(pb.GetDroneId()),
		Class:      telemetry.TargetClass(pb.GetClassification()),
		Timestamp:  pb.GetTimestamp().AsTime(),
		Latitude:   pb.GetLatitude(),
		Longitude:  pb.GetLongitude(),
		Altitude:   pb.GetAltitude(),
		Speed:      pb.GetSpeed(),
		Confidence: pb.GetConfidence(),
	}, true
}
