package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/config"
	"uavmonitor/internal/mtls"
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

	transportCreds, secure, err := mtls.DialCredentials(mtls.FilesFromEnv(), serverName(cfg.ServerAddr))
	if err != nil {
		return err
	}
	dialOpts := []grpc.DialOption{transportCreds}
	if cfg.IngestToken != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(ingestToken{token: cfg.IngestToken, secure: secure}))
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
	if cfg.SwarmSize >= 2 {
		wg.Go(func() {
			runSwarmSlot(ctx, client, cfg, &droneIDs, logger)
		})
	}
	wg.Wait()

	logger.Info("simulator stopped")
	return nil
}

type ingestToken struct {
	token  string
	secure bool
}

func (t ingestToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + t.token}, nil
}

func (t ingestToken) RequireTransportSecurity() bool { return t.secure }

func serverName(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

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

func openStationLinks(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, rng *rand.Rand, logger *slog.Logger) ([]stationLink, error) {
	links := make([]stationLink, 0, cfg.StationCount)
	for n := 1; n <= cfg.StationCount; n++ {
		stream, err := client.StreamTelemetry(ctx)
		if err != nil {
			closeLinks(links, "unopened", logger)
			return nil, fmt.Errorf("open station stream: %w", err)
		}
		links = append(links, stationLink{
			station: simulator.NewStation(n, cfg.ObservationNoiseM, rng),
			stream:  stream,
		})
	}
	return links, nil
}

func sendThroughLinks(links []stationLink, truth *telemetryv1.DroneTelemetry) error {
	for _, link := range links {
		if err := link.stream.Send(link.station.Observe(truth)); err != nil {
			return fmt.Errorf("station %s send: %w", link.station.ID(), err)
		}
	}
	return nil
}

func flyThroughStations(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, drone *simulator.Drone, rng *rand.Rand, logger *slog.Logger) error {
	links, err := openStationLinks(ctx, client, cfg, rng, logger)
	if err != nil {
		return fmt.Errorf("open streams for %s: %w", drone.ID(), err)
	}

	emit := func(truth *telemetryv1.DroneTelemetry) error {
		return sendThroughLinks(links, truth)
	}

	flyErr := drone.Fly(ctx, cfg.SendInterval, emit, logger)
	closeLinks(links, drone.ID(), logger)
	return flyErr
}

func runSwarmSlot(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, droneIDs *atomic.Int64, logger *slog.Logger) {
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5747524d))
	for {
		respawnDelay := time.Duration(5+rng.IntN(25)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(respawnDelay):
		}
		leader := simulator.NewDrone(int(droneIDs.Add(1)), rng)
		members := make([]*simulator.SwarmMember, 0, cfg.SwarmSize-1)
		for range cfg.SwarmSize - 1 {
			members = append(members, simulator.NewSwarmMember(int(droneIDs.Add(1)), rng))
		}
		logger.Info("swarm launched", "leader", leader.ID(), "size", cfg.SwarmSize)
		if err := flySwarm(ctx, client, cfg, leader, members, rng, logger); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("swarm stream failed", "leader", leader.ID(), "error", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func flySwarm(ctx context.Context, client telemetryv1.TelemetryServiceClient, cfg config.Simulator, leader *simulator.Drone, members []*simulator.SwarmMember, rng *rand.Rand, logger *slog.Logger) error {
	leaderLinks, err := openStationLinks(ctx, client, cfg, rng, logger)
	if err != nil {
		return fmt.Errorf("open streams for swarm leader %s: %w", leader.ID(), err)
	}
	memberLinks := make([][]stationLink, 0, len(members))
	for _, member := range members {
		links, err := openStationLinks(ctx, client, cfg, rng, logger)
		if err != nil {
			closeLinks(leaderLinks, leader.ID(), logger)
			for n, opened := range memberLinks {
				closeLinks(opened, members[n].ID(), logger)
			}
			return fmt.Errorf("open streams for swarm member %s: %w", member.ID(), err)
		}
		memberLinks = append(memberLinks, links)
	}

	emit := func(truth *telemetryv1.DroneTelemetry) error {
		if err := sendThroughLinks(leaderLinks, truth); err != nil {
			return err
		}
		for n, member := range members {
			if err := sendThroughLinks(memberLinks[n], member.Follow(truth)); err != nil {
				return err
			}
		}
		return nil
	}

	flyErr := leader.Fly(ctx, cfg.SendInterval, emit, logger)
	closeLinks(leaderLinks, leader.ID(), logger)
	for n, links := range memberLinks {
		closeLinks(links, members[n].ID(), logger)
	}
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
