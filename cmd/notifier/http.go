package main

import (
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"uavmonitor/internal/notify"
)

func newHTTPHandler(conn *nats.Conn, dispatcher *notify.Dispatcher) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !conn.IsConnected() {
			http.Error(w, "nats disconnected", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /metrics", newMetricsHandler(dispatcher))
	return mux
}

func newMetricsHandler(dispatcher *notify.Dispatcher) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	registerCounter(registry, "uav_notifications_sent_total", "Notifications successfully delivered to sinks.",
		dispatcher.SentTotal)
	registerCounter(registry, "uav_notifications_failed_total", "Notification delivery attempts that failed.",
		dispatcher.FailedTotal)
	registerCounter(registry, "uav_notifications_skipped_total", "Alerts skipped without dispatch.",
		dispatcher.SkippedTotal)
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func registerCounter(registry *prometheus.Registry, name, help string, value func() int64) {
	registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{Name: name, Help: help},
		func() float64 { return float64(value()) },
	))
}
