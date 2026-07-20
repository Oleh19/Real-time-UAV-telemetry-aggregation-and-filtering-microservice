package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newMetricsHandler(deps *dependencies) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	registerCounter(registry, "uav_zone_entries_total", "Drones that entered an alert zone.",
		deps.checker.EnteredTotal)
	registerCounter(registry, "uav_zone_exits_total", "Drones that left an alert zone.",
		deps.checker.ExitedTotal)
	registerCounter(registry, "uav_breaches_recorded_total", "Zone breach events persisted to the journal.",
		deps.breachJournal.RecordedTotal)
	registerCounter(registry, "uav_history_samples_written_total", "Telemetry samples written to flight history.",
		deps.historyWriter.WrittenTotal)
	registerGauge(registry, "uav_alarmed_zones", "Alert zones with at least one drone inside.",
		func() int64 { return int64(len(deps.checker.ActiveAlarms())) })
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
