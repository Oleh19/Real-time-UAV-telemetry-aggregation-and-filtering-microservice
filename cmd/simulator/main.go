package main

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/config"
	"uavmonitor/internal/simulator"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("simulator stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadSimulator()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, err := grpc.NewClient(cfg.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("close grpc connection", "error", err)
		}
	}()

	client := telemetryv1.NewTelemetryServiceClient(conn)

	logger.Info("simulator starting",
		"server_addr", cfg.ServerAddr,
		"drone_count", cfg.DroneCount,
		"send_interval", cfg.SendInterval.String(),
	)

	var wg sync.WaitGroup
	for i := 0; i < cfg.DroneCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			flyWithRetry(ctx, client, cfg, index, logger)
		}(i)
	}
	wg.Wait()

	logger.Info("simulator stopped")
	return nil
}

func flyWithRetry(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, index int, logger *slog.Logger) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(index)))
	for {
		drone := simulator.NewDrone(index, cfg.StartLatitude, cfg.StartLongitude, rng)
		err := drone.Fly(ctx, client, cfg.SendInterval, logger)
		if err == nil || ctx.Err() != nil {
			return
		}
		logger.Warn("drone stream failed, retrying", "drone_index", index, "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
