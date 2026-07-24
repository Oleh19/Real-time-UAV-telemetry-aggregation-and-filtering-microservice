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

	"github.com/nats-io/nats.go/jetstream"

	"uavmonitor/internal/config"
	"uavmonitor/internal/env"
	"uavmonitor/internal/health"
	"uavmonitor/internal/notify"
	"uavmonitor/internal/queue/natspub"
	"uavmonitor/internal/tracing"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local health endpoint and exit")
	flag.Parse()
	if *healthcheck {
		os.Exit(health.Probe(env.String("HTTP_ADDR", ":8082")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("notifier stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadNotifier()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stopTracing, err := tracing.Setup(ctx, "uav-notifier", env.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return err
	}
	defer stopTracing()

	sinks := buildSinks(cfg)
	if len(sinks) == 0 {
		logger.Warn("no notification sinks configured; alerts will be acknowledged and dropped")
	}

	conn, err := natspub.Connect(cfg.NATSURL, logger)
	if err != nil {
		return err
	}
	defer conn.Close()

	js, err := natspub.NewJetStreamContext(conn)
	if err != nil {
		return err
	}
	if err := natspub.EnsureAlertsStream(ctx, js); err != nil {
		return err
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, natspub.AlertsStreamName, jetstream.ConsumerConfig{
		Durable:    cfg.DurableName,
		AckPolicy:  jetstream.AckExplicitPolicy,
		AckWait:    30 * time.Second,
		MaxDeliver: 10,
	})
	if err != nil {
		return err
	}

	dispatcher := notify.NewDispatcher(sinks, cfg.NotifyOnExit, cfg.Cooldown, cfg.RequestTimeout, logger)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newHTTPHandler(conn, dispatcher),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		logger.Info("http observability server listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
		}
	}()

	runErr := dispatcher.Run(ctx, []jetstream.Consumer{consumer})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)

	if err := errors.Join(runErr, shutdownErr); err != nil {
		return err
	}

	logger.Info("notifier stopped")
	return nil
}

func buildSinks(cfg config.Notifier) []notify.Sink {
	client := &http.Client{
		Timeout: cfg.RequestTimeout,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}
	sinks := make([]notify.Sink, 0, 2)
	if cfg.WebhookURL != "" {
		sinks = append(sinks, notify.NewWebhookSink(cfg.WebhookURL, client))
	}
	if cfg.TelegramToken != "" {
		sinks = append(sinks, notify.NewTelegramSink(cfg.TelegramBaseURL, cfg.TelegramToken, cfg.TelegramChatID, client))
	}
	return sinks
}
