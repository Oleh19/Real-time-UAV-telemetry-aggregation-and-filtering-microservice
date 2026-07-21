package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"uavmonitor/internal/usecase"
)

type publishFailureCounter interface {
	Failed() int64
}

type fusionStats interface {
	ActiveTracks() int
	MergesTotal() int64
	GatedTotal() int64
}

type hubStats interface {
	Subscribers() int
	Delivered() int64
	Dropped() int64
}

func newMetricsHandler(ingestor *usecase.Ingestor, publisher publishFailureCounter, fuser fusionStats, hub hubStats) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	registerCounter(registry, "uav_ingest_received_total", "Telemetry samples received over gRPC.",
		func() int64 { return ingestor.Stats().Received })
	registerCounter(registry, "uav_ingest_dropped_total", "Telemetry samples dropped because the queue was full.",
		func() int64 { return ingestor.Stats().Dropped })
	registerCounter(registry, "uav_ingest_published_total", "Telemetry samples published to the stream.",
		func() int64 { return ingestor.Stats().Published })
	registerCounter(registry, "uav_ingest_rejected_total", "Telemetry samples rejected by validation.",
		func() int64 { return ingestor.Stats().Rejected })
	registerCounter(registry, "uav_ingest_failed_total", "Telemetry samples that failed to publish.",
		func() int64 { return ingestor.Stats().Failed + publisher.Failed() })
	registerGauge(registry, "uav_ingest_queue_depth", "Telemetry samples currently waiting in the ingest queue.",
		func() int64 { return int64(ingestor.QueueDepth()) })
	registerGauge(registry, "uav_ingest_queue_capacity", "Capacity of the ingest queue.",
		func() int64 { return int64(ingestor.QueueCapacity()) })
	registerGauge(registry, "uav_tracked_drones", "Drones currently tracked in the last-known-state cache.",
		func() int64 { return int64(ingestor.TrackedDrones()) })
	registerGauge(registry, "uav_fused_tracks", "Canonical tracks currently maintained by the fusion engine.",
		func() int64 { return int64(fuser.ActiveTracks()) })
	registerCounter(registry, "uav_fusion_merges_total", "Cross-station observations associated to an existing track.",
		fuser.MergesTotal)
	registerCounter(registry, "uav_fusion_gated_total", "Candidate associations rejected by the Mahalanobis gate.",
		fuser.GatedTotal)
	registerGauge(registry, "uav_subscribers", "Active SubscribeTelemetry streams.",
		func() int64 { return int64(hub.Subscribers()) })
	registerCounter(registry, "uav_subscriber_delivered_total", "Samples delivered to subscribers.",
		hub.Delivered)
	registerCounter(registry, "uav_subscriber_dropped_total", "Samples dropped because a subscriber was too slow.",
		hub.Dropped)
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func registerCounter(registry *prometheus.Registry, name, help string, value func() int64) {
	registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{Name: name, Help: help},
		func() float64 { return float64(value()) },
	))
}

func registerGauge(registry *prometheus.Registry, name, help string, value func() int64) {
	registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{Name: name, Help: help},
		func() float64 { return float64(value()) },
	))
}
