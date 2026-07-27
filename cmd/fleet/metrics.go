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
	registerGauge(registry, "uav_fleet_drones", "Drones registered in the fleet.", func() float64 {
		drones, _, _ := manager.Stats()
		return float64(drones)
	})
	registerGauge(registry, "uav_fleet_airborne", "Fleet drones currently airborne.", func() float64 {
		_, airborne, _ := manager.Stats()
		return float64(airborne)
	})
	registerGauge(registry, "uav_fleet_active_missions", "Missions currently executing.", func() float64 {
		_, _, active := manager.Stats()
		return float64(active)
	})
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func registerGauge(registry *prometheus.Registry, name, help string, value func() float64) {
	registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{Name: name, Help: help},
		value,
	))
}
