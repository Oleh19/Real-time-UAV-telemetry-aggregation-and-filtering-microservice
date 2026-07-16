package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Server struct {
	GRPCAddr    string
	HTTPAddr    string
	NATSURL     string
	WorkerCount int
	QueueSize   int
	StateTTL    time.Duration
}

type Simulator struct {
	ServerAddr     string
	DroneCount     int
	SendInterval   time.Duration
	StartLatitude  float64
	StartLongitude float64
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
	workerCount, err := intEnv("WORKER_COUNT", 8)
	if err != nil {
		return Server{}, err
	}
	queueSize, err := intEnv("QUEUE_SIZE", 1024)
	if err != nil {
		return Server{}, err
	}
	stateTTL, err := durationEnv("STATE_TTL", 5*time.Minute)
	if err != nil {
		return Server{}, err
	}
	cfg := Server{
		GRPCAddr:    strEnv("GRPC_ADDR", ":50051"),
		HTTPAddr:    strEnv("HTTP_ADDR", ":8080"),
		NATSURL:     strEnv("NATS_URL", "nats://localhost:4222"),
		WorkerCount: workerCount,
		QueueSize:   queueSize,
		StateTTL:    stateTTL,
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
	droneCount, err := intEnv("DRONE_COUNT", 5)
	if err != nil {
		return Simulator{}, err
	}
	sendInterval, err := durationEnv("SEND_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Simulator{}, err
	}
	startLat, err := floatEnv("START_LATITUDE", 50.45)
	if err != nil {
		return Simulator{}, err
	}
	startLon, err := floatEnv("START_LONGITUDE", 30.52)
	if err != nil {
		return Simulator{}, err
	}
	cfg := Simulator{
		ServerAddr:     strEnv("SERVER_ADDR", "localhost:50051"),
		DroneCount:     droneCount,
		SendInterval:   sendInterval,
		StartLatitude:  startLat,
		StartLongitude: startLon,
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
	workerCount, err := intEnv("WORKER_COUNT", 8)
	if err != nil {
		return Geofence{}, err
	}
	queueSize, err := intEnv("QUEUE_SIZE", 256)
	if err != nil {
		return Geofence{}, err
	}
	historyRetention, err := durationEnv("HISTORY_RETENTION", 24*time.Hour)
	if err != nil {
		return Geofence{}, err
	}
	batchSize, err := intEnv("BATCH_SIZE", 100)
	if err != nil {
		return Geofence{}, err
	}
	flushInterval, err := durationEnv("FLUSH_INTERVAL", time.Second)
	if err != nil {
		return Geofence{}, err
	}
	cfg := Geofence{
		NATSURL:          strEnv("NATS_URL", "nats://localhost:4222"),
		PostgresDSN:      strEnv("POSTGRES_DSN", "postgres://uav:uav@localhost:5432/uav"),
		HTTPAddr:         strEnv("HTTP_ADDR", ":8081"),
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

func strEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func floatEnv(key string, fallback float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
