package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/broadcast"
	"uavmonitor/internal/classify"
	"uavmonitor/internal/config"
	grpcdelivery "uavmonitor/internal/delivery/grpc"
	"uavmonitor/internal/env"
	"uavmonitor/internal/fusion"
	"uavmonitor/internal/health"
	"uavmonitor/internal/livetargets"
	"uavmonitor/internal/mtls"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/stations"
	"uavmonitor/internal/tracing"
	"uavmonitor/internal/usecase"
)

const maxIngestMessageBytes = 64 * 1024

func newLiveConsumer(ctx context.Context, conn *nats.Conn) (jetstream.Consumer, error) {
	js, err := natspub.NewJetStream(ctx, conn)
	if err != nil {
		return nil, err
	}
	return js.CreateOrUpdateConsumer(ctx, natspub.StreamName, jetstream.ConsumerConfig{
		FilterSubjects:    []string{natspub.SubjectTelemetry + ".*"},
		DeliverPolicy:     jetstream.DeliverNewPolicy,
		AckPolicy:         jetstream.AckNonePolicy,
		InactiveThreshold: time.Minute,
	})
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(health.Probe(env.String("HTTP_ADDR", ":8080")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stopTracing, err := tracing.Setup(ctx, "uav-server", env.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer stopTracing()

	natsConn, err := natspub.Connect(cfg.NATSURL, logger)
	if err != nil {
		return err
	}
	defer natsConn.Close()

	publisher, err := natspub.NewAsyncPublisher(ctx, natsConn, logger, cfg.PartitionCount)
	if err != nil {
		return err
	}
	fusionCfg := fusion.DefaultConfig()
	fusionCfg.IDPrefix = cfg.InstanceID
	fuser := fusion.NewFuser(fusionCfg)
	hub := broadcast.NewHub(broadcast.DefaultSubscriberBuffer)
	classifier := classify.NewClassifier()
	stationRegistry := stations.NewRegistry(stations.DefaultConfig(), logger)
	ingestor := usecase.NewIngestor(publisher, logger, cfg.QueueSize, cfg.StateTTL,
		usecase.WithResolver(fuser),
		usecase.WithBroadcaster(hub),
		usecase.WithClassifier(classifier),
		usecase.WithStationObserver(stationRegistry),
	)
	go stationRegistry.Run(ctx)

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	ingestor.Start(workerCtx, cfg.WorkerCount)
	go fuser.Run(workerCtx)

	var liveTargets *livetargets.Store
	if cfg.ServeLiveAPI {
		liveTargets = livetargets.NewStore(cfg.StateTTL)
		liveConsumer, err := newLiveConsumer(ctx, natsConn)
		if err != nil {
			return err
		}
		go func() {
			if err := livetargets.Run(workerCtx, liveConsumer, liveTargets, logger); err != nil {
				logger.Error("live target aggregator stopped", "error", err)
			}
		}()
		go liveTargets.EvictLoop(workerCtx)
	}

	if cfg.IngestToken == "" {
		logger.Warn("ingest authentication disabled: set INGEST_TOKEN to require a token")
	}
	serverOpts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.StreamInterceptor(grpcdelivery.StreamAuthInterceptor(cfg.IngestToken)),
		grpc.MaxRecvMsgSize(maxIngestMessageBytes),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    2 * time.Minute,
			Timeout: 20 * time.Second,
		}),
		grpc.MaxConcurrentStreams(512),
	}
	tlsFiles := mtls.FilesFromEnv()
	if tlsFiles.ServerEnabled() {
		creds, err := mtls.ServerCredentials(tlsFiles)
		if err != nil {
			return err
		}
		serverOpts = append(serverOpts, grpc.Creds(creds))
		logger.Info("grpc mutual TLS enabled")
	} else {
		logger.Warn("grpc transport is plaintext: set TLS_CA_CERT/TLS_SERVER_CERT/TLS_SERVER_KEY to require client certificates")
	}
	grpcServer := grpc.NewServer(serverOpts...)
	telemetryv1.RegisterTelemetryServiceServer(grpcServer, grpcdelivery.NewHandler(ingestor, hub, logger))

	listener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           observabilityHandler(ingestor, publisher, fuser, hub, classifier, stationRegistry, liveTargets, natsConn, logger),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("grpc server listening", "addr", cfg.GRPCAddr)
		errCh <- grpcServer.Serve(listener)
	}()
	go func() {
		logger.Info("http observability server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hub.Close()
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		grpcServer.Stop()
	}

	ingestor.Shutdown()

	if err := publisher.Flush(shutdownCtx); err != nil {
		logger.Error("flush pending telemetry", "error", err)
	}

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	stats := ingestor.Stats()
	logger.Info("server stopped",
		"received", stats.Received,
		"published", stats.Published,
		"dropped", stats.Dropped,
		"failed", stats.Failed+publisher.Failed(),
		"rejected", stats.Rejected,
	)
	return nil
}
