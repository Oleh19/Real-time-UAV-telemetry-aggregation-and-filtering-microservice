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

	"github.com/jackc/pgx/v5/pgxpool"

	"uavmonitor/internal/config"
	"uavmonitor/internal/env"
	"uavmonitor/internal/fleet"
	"uavmonitor/internal/health"
	"uavmonitor/internal/repository/postgres"
	"uavmonitor/internal/tracing"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(health.Probe(env.String("HTTP_ADDR", ":8083")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fleet service stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadFleet()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stopTracing, err := tracing.Setup(ctx, "uav-fleet", env.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer stopTracing()

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

	repo := postgres.NewRepository(pool)
	if err := repo.Migrate(ctx); err != nil {
		return err
	}

	manager := fleet.NewManager(repo, logger)
	if err := manager.Load(ctx); err != nil {
		return err
	}
	go manager.Run(ctx, cfg.TickInterval)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newHTTPHandler(manager, pool, logger),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		logger.Info("fleet http server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case serveErr = <-errCh:
		logger.Error("http server failed", "error", serveErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)

	if err := errors.Join(serveErr, shutdownErr); err != nil {
		return err
	}
	logger.Info("fleet service stopped")
	return nil
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
