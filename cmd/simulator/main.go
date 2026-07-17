package main

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
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
		"max_concurrent_drones", cfg.DroneCount,
		"send_interval", cfg.SendInterval.String(),
	)

	var droneIDs atomic.Int64
	var wg sync.WaitGroup
	for slot := 0; slot < cfg.DroneCount; slot++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			runDroneSlot(ctx, client, cfg, slot, &droneIDs, logger)
		}(slot)
	}
	wg.Wait()

	logger.Info("simulator stopped")
	return nil
}

func runDroneSlot(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, slot int, droneIDs *atomic.Int64, logger *slog.Logger) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(slot)))
	for {
		respawnDelay := time.Duration(2+rng.Intn(28)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(respawnDelay):
		}
		drone := simulator.NewDrone(int(droneIDs.Add(1)), rng)
		if err := drone.Fly(ctx, client, cfg.SendInterval, logger); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("drone stream failed", "slot", slot, "error", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}
