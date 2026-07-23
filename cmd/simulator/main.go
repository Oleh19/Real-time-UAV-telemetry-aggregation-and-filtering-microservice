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
	"uavmonitor/internal/partition"
	"uavmonitor/internal/simulator"
)

const senderBuffer = 8192

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

	tlsFiles := mtls.FilesFromEnv()
	conns := make([]*grpc.ClientConn, 0, len(cfg.ServerAddrs))
	for _, addr := range cfg.ServerAddrs {
		transportCreds, secure, err := mtls.DialCredentials(tlsFiles, serverName(addr))
		if err != nil {
			return err
		}
		dialOpts := []grpc.DialOption{transportCreds}
		if cfg.IngestToken != "" {
			dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(ingestToken{token: cfg.IngestToken, secure: secure}))
		}
		conn, err := grpc.NewClient(addr, dialOpts...)
		if err != nil {
			return err
		}
		defer func() {
			if err := conn.Close(); err != nil {
				logger.Error("close grpc connection", "error", err)
			}
		}()
		conns = append(conns, conn)
	}

	stationsForServer := cfg.StationCount * len(conns)
	logger.Info("simulator starting",
		"server_addrs", cfg.ServerAddrs,
		"max_concurrent_drones", cfg.DroneCount,
		"stations", cfg.StationCount,
		"streams", stationsForServer,
		"observation_noise_m", cfg.ObservationNoiseM,
		"send_interval", cfg.SendInterval.String(),
	)

	fleet := newFleet(cfg, conns, logger)
	return fleet.run(ctx)
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

type sender struct {
	ch      chan *telemetryv1.DroneTelemetry
	dropped atomic.Int64
}

func (s *sender) emit(msg *telemetryv1.DroneTelemetry) {
	select {
	case s.ch <- msg:
	default:
		s.dropped.Add(1)
	}
}

func (s *sender) run(ctx context.Context, client telemetryv1.TelemetryServiceClient, label string, logger *slog.Logger) {
	for {
		if err := s.pump(ctx, client); err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("sender stream failed, reopening", "stream", label, "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		return
	}
}

func (s *sender) pump(ctx context.Context, client telemetryv1.TelemetryServiceClient) error {
	stream, err := client.StreamTelemetry(ctx)
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			_, closeErr := stream.CloseAndRecv()
			if closeErr != nil && !errors.Is(closeErr, io.EOF) && !errors.Is(closeErr, context.Canceled) {
				return closeErr
			}
			return nil
		case msg := <-s.ch:
			if err := stream.Send(msg); err != nil {
				return fmt.Errorf("send: %w", err)
			}
		}
	}
}

type fleet struct {
	cfg      config.Simulator
	conns    []*grpc.ClientConn
	logger   *slog.Logger
	stations []*simulator.Station
	senders  [][]*sender
	rng      *rand.Rand
	droneIDs atomic.Int64
}

func newFleet(cfg config.Simulator, conns []*grpc.ClientConn, logger *slog.Logger) *fleet {
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x53494d))
	stations := make([]*simulator.Station, cfg.StationCount)
	for n := range stations {
		stations[n] = simulator.NewStation(n+1, cfg.ObservationNoiseM, rng)
	}
	senders := make([][]*sender, cfg.StationCount)
	for st := range senders {
		senders[st] = make([]*sender, len(conns))
		for sv := range senders[st] {
			senders[st][sv] = &sender{ch: make(chan *telemetryv1.DroneTelemetry, senderBuffer)}
		}
	}
	return &fleet{cfg: cfg, conns: conns, logger: logger, stations: stations, senders: senders, rng: rng}
}

func (f *fleet) run(ctx context.Context) error {
	var wg sync.WaitGroup
	for st := range f.senders {
		for sv := range f.senders[st] {
			client := telemetryv1.NewTelemetryServiceClient(f.conns[sv])
			s := f.senders[st][sv]
			label := fmt.Sprintf("station-%02d->server-%d", st+1, sv)
			wg.Go(func() { s.run(ctx, client, label, f.logger) })
		}
	}

	slots := make([]*simulator.Drone, f.cfg.DroneCount)
	respawnAt := make([]time.Time, f.cfg.DroneCount)
	now := time.Now()
	for n := range respawnAt {
		respawnAt[n] = now.Add(time.Duration(f.rng.IntN(int(f.cfg.SendInterval.Seconds()*1000)+2000)) * time.Millisecond)
	}

	var swarmLeader *simulator.Drone
	var swarmMembers []*simulator.SwarmMember
	swarmRespawnAt := now.Add(5 * time.Second)

	ticker := time.NewTicker(f.cfg.SendInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			f.logger.Info("simulator stopped")
			return nil
		case tick := <-ticker.C:
			for n := range slots {
				slots[n] = f.stepDrone(slots[n], &respawnAt[n], tick)
			}
			swarmLeader, swarmMembers = f.stepSwarm(swarmLeader, swarmMembers, &swarmRespawnAt, tick)
		}
	}
}

func (f *fleet) stepDrone(drone *simulator.Drone, respawnAt *time.Time, tick time.Time) *simulator.Drone {
	if drone == nil {
		if tick.Before(*respawnAt) {
			return nil
		}
		return simulator.NewDrone(int(f.droneIDs.Add(1)), f.rng)
	}
	if drone.ShotDown(f.cfg.SendInterval) {
		*respawnAt = tick.Add(time.Duration(2+f.rng.IntN(28)) * time.Second)
		return nil
	}
	f.dispatch(drone.Advance(f.cfg.SendInterval))
	return drone
}

func (f *fleet) stepSwarm(leader *simulator.Drone, members []*simulator.SwarmMember, respawnAt *time.Time, tick time.Time) (*simulator.Drone, []*simulator.SwarmMember) {
	if f.cfg.SwarmSize < 2 {
		return nil, nil
	}
	if leader == nil {
		if tick.Before(*respawnAt) {
			return nil, nil
		}
		leader = simulator.NewDrone(int(f.droneIDs.Add(1)), f.rng)
		members = make([]*simulator.SwarmMember, 0, f.cfg.SwarmSize-1)
		for range f.cfg.SwarmSize - 1 {
			members = append(members, simulator.NewSwarmMember(int(f.droneIDs.Add(1)), f.rng))
		}
		f.logger.Info("swarm launched", "leader", leader.ID(), "size", f.cfg.SwarmSize)
		return leader, members
	}
	if leader.ShotDown(f.cfg.SendInterval) {
		*respawnAt = tick.Add(time.Duration(5+f.rng.IntN(25)) * time.Second)
		return nil, nil
	}
	truth := leader.Advance(f.cfg.SendInterval)
	f.dispatch(truth)
	for _, member := range members {
		f.dispatch(member.Follow(truth))
	}
	return leader, members
}

func (f *fleet) dispatch(truth *telemetryv1.DroneTelemetry) {
	serverIdx := partition.Of(truth.GetLatitude(), truth.GetLongitude(), len(f.conns))
	for st, station := range f.stations {
		f.senders[st][serverIdx].emit(station.Observe(truth))
	}
}
