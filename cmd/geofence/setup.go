package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/config"
	"uavmonitor/internal/geofence"
	"uavmonitor/internal/partition"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/replay"
	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/telemetry"
)

type dependencies struct {
	pool            *pgxpool.Pool
	nats            *nats.Conn
	repo            *postgres.Repository
	checker         *geofence.ZoneChecker
	zoneIndex       *geofence.RefreshingZoneIndex
	oblasts         []telemetry.Zone
	historyWriter   *geofence.HistoryWriter
	breachJournal   *geofence.BreachJournal
	swarmDetector   *geofence.SwarmDetector
	replayManager   *replay.Manager
	historyConsumer []jetstream.Consumer
	zonesConsumer   []jetstream.Consumer
	breachConsumer  []jetstream.Consumer
	swarmConsumer   []jetstream.Consumer
}

func newDependencies(ctx context.Context, cfg config.Geofence, logger *slog.Logger) (*dependencies, func(), error) {
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, nil, err
	}

	pingCtx, cancelPing := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPing()
	if err := waitForPostgres(pingCtx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}

	natsConn, err := natspub.Connect(cfg.NATSURL, logger)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}

	var replayManagerRef *replay.Manager
	var replayPublisherRef *grpcTelemetryPublisher
	cleanup := func() {
		if replayManagerRef != nil {
			replayManagerRef.Close()
		}
		if replayPublisherRef != nil {
			replayPublisherRef.Close()
		}
		natsConn.Close()
		pool.Close()
	}

	js, err := natspub.NewJetStream(ctx, natsConn, cfg.PartitionCount)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	ownedPartitions := partition.AssignedPartitions(cfg.PartitionCount, cfg.ShardIndex, cfg.ShardCount)
	logger.Info("shard telemetry partitions", "replica", cfg.ReplicaID, "partitions", ownedPartitions)

	historyConsumer, err := newTelemetryConsumers(ctx, js, "geofence-history", ownedPartitions)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	zonesConsumer, err := newTelemetryConsumers(ctx, js, "geofence-zones", ownedPartitions)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	breachConsumer, err := newConsumer(ctx, js, natspub.AlertsStreamName, "geofence-breach-journal", "")
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	swarmConsumer, err := newTelemetryConsumers(ctx, js, "geofence-swarms", ownedPartitions)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	repo := postgres.NewRepository(pool)

	if err := repo.Migrate(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}

	seeded, err := repo.SeedOblasts(ctx)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if seeded > 0 {
		logger.Info("seeded oblast alert zones", "count", seeded)
	}

	oblasts, err := repo.ListZones(ctx)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	zoneIndex, err := geofence.NewRefreshingZoneIndex(ctx, repo)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	publisher := natspub.NewPublisher(js)
	checker := geofence.NewZoneChecker(zoneIndex, publisher, logger)
	historyWriter := geofence.NewHistoryWriter(repo, logger)
	replayPublisher, err := newGRPCTelemetryPublisher(cfg.IngestServerAddr, cfg.IngestToken)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	replayManager := replay.NewManager(repo, replayPublisher, logger, replay.DefaultMaxConcurrent, postgres.MaxReplayPoints)
	breachJournal := geofence.NewBreachJournal(repo, logger)
	swarmDetector := geofence.NewSwarmDetector(geofence.SwarmConfig{
		RadiusMeters: float64(cfg.SwarmRadiusM),
		MinSize:      cfg.SwarmMinSize,
		EvalInterval: cfg.SwarmEvalInterval,
	}, logger)

	replayManagerRef = replayManager
	replayPublisherRef = replayPublisher
	return &dependencies{
		pool:            pool,
		nats:            natsConn,
		repo:            repo,
		checker:         checker,
		zoneIndex:       zoneIndex,
		oblasts:         oblasts,
		historyWriter:   historyWriter,
		breachJournal:   breachJournal,
		swarmDetector:   swarmDetector,
		replayManager:   replayManager,
		historyConsumer: historyConsumer,
		zonesConsumer:   zonesConsumer,
		breachConsumer:  []jetstream.Consumer{breachConsumer},
		swarmConsumer:   swarmConsumer,
	}, cleanup, nil
}

func newTelemetryConsumers(ctx context.Context, js jetstream.JetStream, role string, partitions []int) ([]jetstream.Consumer, error) {
	consumers := make([]jetstream.Consumer, 0, len(partitions))
	for _, p := range partitions {
		consumer, err := newConsumer(ctx, js, natspub.TelemetryStreamName(p), fmt.Sprintf("%s-p%d", role, p), "")
		if err != nil {
			return nil, err
		}
		consumers = append(consumers, consumer)
	}
	return consumers, nil
}

func newConsumer(ctx context.Context, js jetstream.JetStream, stream, durable, filterSubject string) (jetstream.Consumer, error) {
	cfg := jetstream.ConsumerConfig{
		Durable:    durable,
		AckPolicy:  jetstream.AckExplicitPolicy,
		AckWait:    30 * time.Second,
		MaxDeliver: 10,
	}
	if filterSubject != "" {
		cfg.FilterSubject = filterSubject
	}
	return js.CreateOrUpdateConsumer(ctx, stream, cfg)
}

func waitForPostgres(ctx context.Context, pool *pgxpool.Pool) error {
	for {
		if err := pool.Ping(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
