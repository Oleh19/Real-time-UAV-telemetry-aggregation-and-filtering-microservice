package config

import (
	"fmt"
	"time"

	"uavmonitor/internal/env"
)

type Server struct {
	GRPCAddr    string
	HTTPAddr    string
	NATSURL     string
	WorkerCount int
	QueueSize   int
	StateTTL    time.Duration
	IngestToken string
}

type Simulator struct {
	ServerAddr   string
	DroneCount   int
	SendInterval time.Duration
	IngestToken  string
}

type Geofence struct {
	NATSURL          string
	PostgresDSN      string
	HTTPAddr         string
	WorkerCount      int
	QueueSize        int
	HistoryRetention time.Duration
	BatchSize        int
	FlushInterval    time.Duration
}

func LoadServer() (Server, error) {
	workerCount, err := env.Int("WORKER_COUNT", 8)
	if err != nil {
		return Server{}, err
	}
	queueSize, err := env.Int("QUEUE_SIZE", 1024)
	if err != nil {
		return Server{}, err
	}
	stateTTL, err := env.Duration("STATE_TTL", 5*time.Minute)
	if err != nil {
		return Server{}, err
	}
	cfg := Server{
		GRPCAddr:    env.String("GRPC_ADDR", ":50051"),
		HTTPAddr:    env.String("HTTP_ADDR", ":8080"),
		NATSURL:     env.String("NATS_URL", "nats://localhost:4222"),
		WorkerCount: workerCount,
		QueueSize:   queueSize,
		StateTTL:    stateTTL,
		IngestToken: env.String("INGEST_TOKEN", ""),
	}
	if cfg.WorkerCount < 1 {
		return Server{}, fmt.Errorf("validate WORKER_COUNT: must be >= 1, got %d", cfg.WorkerCount)
	}
	if cfg.QueueSize < 1 {
		return Server{}, fmt.Errorf("validate QUEUE_SIZE: must be >= 1, got %d", cfg.QueueSize)
	}
	if cfg.StateTTL < time.Second {
		return Server{}, fmt.Errorf("validate STATE_TTL: must be >= 1s, got %s", cfg.StateTTL)
	}
	return cfg, nil
}

func LoadSimulator() (Simulator, error) {
	droneCount, err := env.Int("DRONE_COUNT", 5)
	if err != nil {
		return Simulator{}, err
	}
	sendInterval, err := env.Duration("SEND_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Simulator{}, err
	}
	cfg := Simulator{
		ServerAddr:   env.String("SERVER_ADDR", "localhost:50051"),
		DroneCount:   droneCount,
		SendInterval: sendInterval,
		IngestToken:  env.String("INGEST_TOKEN", ""),
	}
	if cfg.DroneCount < 1 {
		return Simulator{}, fmt.Errorf("validate DRONE_COUNT: must be >= 1, got %d", cfg.DroneCount)
	}
	if cfg.SendInterval < 10*time.Millisecond {
		return Simulator{}, fmt.Errorf("validate SEND_INTERVAL: must be >= 10ms, got %s", cfg.SendInterval)
	}
	return cfg, nil
}

func LoadGeofence() (Geofence, error) {
	workerCount, err := env.Int("WORKER_COUNT", 8)
	if err != nil {
		return Geofence{}, err
	}
	queueSize, err := env.Int("QUEUE_SIZE", 256)
	if err != nil {
		return Geofence{}, err
	}
	historyRetention, err := env.Duration("HISTORY_RETENTION", 24*time.Hour)
	if err != nil {
		return Geofence{}, err
	}
	batchSize, err := env.Int("BATCH_SIZE", 100)
	if err != nil {
		return Geofence{}, err
	}
	flushInterval, err := env.Duration("FLUSH_INTERVAL", time.Second)
	if err != nil {
		return Geofence{}, err
	}
	cfg := Geofence{
		NATSURL:          env.String("NATS_URL", "nats://localhost:4222"),
		PostgresDSN:      env.String("POSTGRES_DSN", "postgres://uav:uav@localhost:5432/uav"),
		HTTPAddr:         env.String("HTTP_ADDR", ":8081"),
		WorkerCount:      workerCount,
		QueueSize:        queueSize,
		HistoryRetention: historyRetention,
		BatchSize:        batchSize,
		FlushInterval:    flushInterval,
	}
	if cfg.WorkerCount < 1 {
		return Geofence{}, fmt.Errorf("validate WORKER_COUNT: must be >= 1, got %d", cfg.WorkerCount)
	}
	if cfg.QueueSize < 1 {
		return Geofence{}, fmt.Errorf("validate QUEUE_SIZE: must be >= 1, got %d", cfg.QueueSize)
	}
	if cfg.HistoryRetention < time.Minute {
		return Geofence{}, fmt.Errorf("validate HISTORY_RETENTION: must be >= 1m, got %s", cfg.HistoryRetention)
	}
	if cfg.BatchSize < 1 {
		return Geofence{}, fmt.Errorf("validate BATCH_SIZE: must be >= 1, got %d", cfg.BatchSize)
	}
	if cfg.FlushInterval < 100*time.Millisecond {
		return Geofence{}, fmt.Errorf("validate FLUSH_INTERVAL: must be >= 100ms, got %s", cfg.FlushInterval)
	}
	return cfg, nil
}
