package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
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

	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if cfg.IngestToken != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(ingestToken(cfg.IngestToken)))
	}
	conn, err := grpc.NewClient(cfg.ServerAddr, dialOpts...)
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
		"stations", cfg.StationCount,
		"observation_noise_m", cfg.ObservationNoiseM,
		"send_interval", cfg.SendInterval.String(),
	)

	var droneIDs atomic.Int64
	var wg sync.WaitGroup
	for slot := range cfg.DroneCount {
		wg.Go(func() {
			runDroneSlot(ctx, client, cfg, slot, &droneIDs, logger)
		})
	}
	wg.Wait()

	logger.Info("simulator stopped")
	return nil
}

type ingestToken string

func (t ingestToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

func (ingestToken) RequireTransportSecurity() bool { return false }

func runDroneSlot(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, slot int, droneIDs *atomic.Int64, logger *slog.Logger) {
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(slot)))
	for {
		respawnDelay := time.Duration(2+rng.IntN(28)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(respawnDelay):
		}
		drone := simulator.NewDrone(int(droneIDs.Add(1)), rng)
		if err := flyThroughStations(ctx, client, cfg, drone, rng, logger); err != nil {
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

type stationLink struct {
	station *simulator.Station
	stream  telemetryv1.TelemetryService_StreamTelemetryClient
}

func flyThroughStations(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, drone *simulator.Drone, rng *rand.Rand, logger *slog.Logger) error {
	links := make([]stationLink, 0, cfg.StationCount)
	for n := 1; n <= cfg.StationCount; n++ {
		stream, err := client.StreamTelemetry(ctx)
		if err != nil {
			closeLinks(links, drone.ID(), logger)
			return fmt.Errorf("open stream for %s: %w", drone.ID(), err)
		}
		links = append(links, stationLink{
			station: simulator.NewStation(n, cfg.ObservationNoiseM, rng),
			stream:  stream,
		})
	}

	emit := func(truth *telemetryv1.DroneTelemetry) error {
		for _, link := range links {
			if err := link.stream.Send(link.station.Observe(truth)); err != nil {
				return fmt.Errorf("station %s send: %w", link.station.ID(), err)
			}
		}
		return nil
	}

	flyErr := drone.Fly(ctx, cfg.SendInterval, emit, logger)
	closeLinks(links, drone.ID(), logger)
	return flyErr
}

func closeLinks(links []stationLink, droneID string, logger *slog.Logger) {
	for _, link := range links {
		summary, err := link.stream.CloseAndRecv()
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			logger.Warn("close stream", "drone_id", droneID, "station", link.station.ID(), "error", err)
			continue
		}
		if summary != nil {
			logger.Info("stream closed",
				"drone_id", droneID,
				"station", link.station.ID(),
				"received_by_server", summary.GetReceivedCount(),
				"dropped_by_server", summary.GetDroppedCount(),
				"rejected_by_server", summary.GetRejectedCount(),
			)
		}
	}
}
