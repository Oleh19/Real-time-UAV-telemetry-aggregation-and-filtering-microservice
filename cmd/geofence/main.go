package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uavmonitor/internal/config"
	"uavmonitor/internal/env"
	"uavmonitor/internal/geofence"
	"uavmonitor/internal/health"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(health.Probe(env.String("HTTP_ADDR", ":8081")))
	}

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

	deps, cleanup, err := newDependencies(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	go deps.zoneIndex.Run(ctx, deps.repo, time.Minute, logger)
	go geofence.RunRetention(ctx, deps.repo, cfg.HistoryRetention, logger)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newHTTPHandler(deps, logger),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		logger.Info("http observability server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
		}
	}()

	runErr := runConsumers(ctx, deps, cfg, logger)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)

	if err := errors.Join(runErr, shutdownErr); err != nil {
		return err
	}

	logger.Info("geofence worker stopped")
	return nil
}

func runConsumers(ctx context.Context, deps *dependencies, cfg config.Geofence, logger *slog.Logger) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	errCh := make(chan error, 3)
	go func() {
		errCh <- geofence.NewHistoryWriter(deps.repo, logger).Run(runCtx, deps.historyConsumer, cfg.BatchSize, cfg.FlushInterval)
	}()
	go func() {
		errCh <- deps.checker.Run(runCtx, deps.zonesConsumer, cfg.WorkerCount, cfg.QueueSize)
	}()
	go func() {
		errCh <- geofence.NewBreachJournal(deps.repo, logger).Run(runCtx, deps.breachConsumer)
	}()

	var runErr error
	for n := 0; n < 3; n++ {
		if err := <-errCh; err != nil && runErr == nil {
			runErr = err
			cancelRun()
		}
	}
	return runErr
}
