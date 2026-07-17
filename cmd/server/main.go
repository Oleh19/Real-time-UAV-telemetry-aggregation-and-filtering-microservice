package main

import (
	"context"
	"encoding/json"
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
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/config"
	grpcdelivery "uavmonitor/internal/delivery/grpc"
	"uavmonitor/internal/health"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/sse"
	"uavmonitor/internal/telemetry"
	"uavmonitor/internal/usecase"
)

const eventsInterval = 500 * time.Millisecond

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(health.Probe(strEnv("HTTP_ADDR", ":8080")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func strEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	natsConn, err := natspub.Connect(cfg.NATSURL, logger)
	if err != nil {
		return err
	}
	defer natsConn.Close()

	publisher, err := natspub.NewAsyncPublisher(ctx, natsConn, logger)
	if err != nil {
		return err
	}
	ingestor := usecase.NewIngestor(publisher, logger, cfg.QueueSize, cfg.StateTTL)

	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	defer cancelWorkers()
	ingestor.Start(workerCtx, cfg.WorkerCount)

	grpcServer := grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    2 * time.Minute,
			Timeout: 20 * time.Second,
		}),
		grpc.MaxConcurrentStreams(512),
	)
	telemetryv1.RegisterTelemetryServiceServer(grpcServer, grpcdelivery.NewHandler(ingestor, logger))

	listener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           observabilityHandler(ingestor, publisher, natsConn, logger),
		ReadHeaderTimeout: 5 * time.Second,
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

func observabilityHandler(ingestor *usecase.Ingestor, publisher *natspub.AsyncPublisher, natsConn *nats.Conn, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if natsConn.Status() != nats.CONNECTED {
			http.Error(w, "nats unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, metricsSnapshot(ingestor, publisher))
	})
	mux.HandleFunc("GET /drones", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, ingestor.Snapshot())
	})
	mux.HandleFunc("GET /events", sse.Handler(eventsInterval, func(context.Context) any {
		return telemetryEvent{
			Drones: ingestor.Snapshot(),
			Stats:  metricsSnapshot(ingestor, publisher),
		}
	}, logger))
	return mux
}

type telemetryEvent struct {
	Drones []telemetry.Sample `json:"drones"`
	Stats  usecase.Stats      `json:"stats"`
}

func metricsSnapshot(ingestor *usecase.Ingestor, publisher *natspub.AsyncPublisher) usecase.Stats {
	stats := ingestor.Stats()
	stats.Failed += publisher.Failed()
	return stats
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
