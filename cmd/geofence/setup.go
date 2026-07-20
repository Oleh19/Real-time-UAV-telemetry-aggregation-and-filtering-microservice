package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/config"
	"uavmonitor/internal/geofence"
	"uavmonitor/internal/queue/natspub"
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
	historyConsumer jetstream.Consumer
	zonesConsumer   jetstream.Consumer
	breachConsumer  jetstream.Consumer
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

	cleanup := func() {
		natsConn.Close()
		pool.Close()
	}

	js, err := natspub.NewJetStream(ctx, natsConn)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	historyConsumer, err := newConsumer(ctx, js, "geofence-history", natspub.SubjectTelemetry)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	zonesConsumer, err := newConsumer(ctx, js, "geofence-zones", natspub.SubjectTelemetry)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	breachConsumer, err := newConsumer(ctx, js, "geofence-breach-journal", natspub.SubjectAlerts)
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

	checker := geofence.NewZoneChecker(zoneIndex, natspub.NewPublisher(js), logger)
	historyWriter := geofence.NewHistoryWriter(repo, logger)
	breachJournal := geofence.NewBreachJournal(repo, logger)

	return &dependencies{
		pool:            pool,
		nats:            natsConn,
		repo:            repo,
		checker:         checker,
		zoneIndex:       zoneIndex,
		oblasts:         oblasts,
		historyWriter:   historyWriter,
		breachJournal:   breachJournal,
		historyConsumer: historyConsumer,
		zonesConsumer:   zonesConsumer,
		breachConsumer:  breachConsumer,
	}, cleanup, nil
}

func newConsumer(ctx context.Context, js jetstream.JetStream, durable, subject string) (jetstream.Consumer, error) {
	return js.CreateOrUpdateConsumer(ctx, natspub.StreamName, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    10,
	})
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
