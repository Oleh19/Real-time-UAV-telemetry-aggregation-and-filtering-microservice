package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func newMetricsHandler(manager fleetService) http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	registerGauge(registry, "uav_fleet_drones", "Drones registered in the fleet.",
		func() float64 { return float64(manager.Stats().Drones) })
	registerGauge(registry, "uav_fleet_airborne", "Fleet drones currently airborne.",
		func() float64 { return float64(manager.Stats().Airborne) })
	registerGauge(registry, "uav_fleet_active_missions", "Missions currently executing.",
		func() float64 { return float64(manager.Stats().ActiveMissions) })
	registerGauge(registry, "uav_fleet_low_battery", "Fleet drones at or below the low-battery threshold.",
		func() float64 { return float64(manager.Stats().LowBattery) })
	registerCounter(registry, "uav_fleet_missions_completed_total", "Missions that completed successfully.",
		func() float64 { return float64(manager.Stats().Completed) })
	registerCounter(registry, "uav_fleet_missions_aborted_total", "Missions aborted by command, recall or low battery.",
		func() float64 { return float64(manager.Stats().Aborted) })
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func registerGauge(registry *prometheus.Registry, name, help string, value func() float64) {
	registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{Name: name, Help: help},
		value,
	))
}

func registerCounter(registry *prometheus.Registry, name, help string, value func() float64) {
	registry.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{Name: name, Help: help},
		value,
	))
}
