package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/config"
	"uavmonitor/internal/geofence"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("geofence worker stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadGeofence()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 30*time.Second)
	defer cancelPing()
	if err := waitForPostgres(pingCtx, pool); err != nil {
		return err
	}

	natsConn, err := natspub.Connect(cfg.NATSURL, logger)
	if err != nil {
		return err
	}
	defer natsConn.Close()

	js, err := natspub.NewJetStream(ctx, natsConn)
	if err != nil {
		return err
	}

	historyConsumer, err := newConsumer(ctx, js, "geofence-history")
	if err != nil {
		return err
	}
	zonesConsumer, err := newConsumer(ctx, js, "geofence-zones")
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           healthHandler(pool),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("http observability server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
		}
	}()

	repo := postgres.NewRepository(pool)
	publisher := natspub.NewPublisher(js)
	go geofence.RunRetention(ctx, repo, cfg.HistoryRetention, logger)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	errCh := make(chan error, 2)
	go func() {
		errCh <- geofence.NewHistoryWriter(repo, logger).Run(runCtx, historyConsumer, cfg.BatchSize, cfg.FlushInterval)
	}()
	go func() {
		errCh <- geofence.NewZoneChecker(repo, publisher, logger).Run(runCtx, zonesConsumer, cfg.WorkerCount, cfg.QueueSize)
	}()

	var runErr error
	for n := 0; n < 2; n++ {
		if err := <-errCh; err != nil && runErr == nil {
			runErr = err
			cancelRun()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)

	if err := errors.Join(runErr, shutdownErr); err != nil {
		return err
	}

	logger.Info("geofence worker stopped")
	return nil
}

func newConsumer(ctx context.Context, js jetstream.JetStream, durable string) (jetstream.Consumer, error) {
	consumer, err := js.CreateOrUpdateConsumer(ctx, natspub.StreamName, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: natspub.SubjectTelemetry,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    10,
	})
	if err != nil {
		return nil, err
	}
	return consumer, nil
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

func healthHandler(pool *pgxpool.Pool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
