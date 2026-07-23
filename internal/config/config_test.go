package config_test

import (
	"testing"
	"time"

	"uavmonitor/internal/config"
)

func TestLoadServer(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Server
		wantErr bool
	}{
		{
			name: "defaults",
			want: config.Server{
				GRPCAddr:             ":50051",
				HTTPAddr:             ":8080",
				NATSURL:              "nats://localhost:4222",
				WorkerCount:          8,
				QueueSize:            1024,
				PartitionCount:       4,
				StateTTL:             5 * time.Minute,
				InstanceID:           "target",
				ServeLiveAPI:         true,
				MaxConcurrentStreams: 512,
			},
		},
		{
			name: "overrides applied",
			env: map[string]string{
				"GRPC_ADDR":    ":6000",
				"WORKER_COUNT": "2",
				"QUEUE_SIZE":   "16",
				"STATE_TTL":    "30s",
			},
			want: config.Server{
				GRPCAddr:             ":6000",
				HTTPAddr:             ":8080",
				NATSURL:              "nats://localhost:4222",
				WorkerCount:          2,
				QueueSize:            16,
				PartitionCount:       4,
				StateTTL:             30 * time.Second,
				InstanceID:           "target",
				ServeLiveAPI:         true,
				MaxConcurrentStreams: 512,
			},
		},
		{
			name:    "invalid worker count",
			env:     map[string]string{"WORKER_COUNT": "abc"},
			wantErr: true,
		},
		{
			name:    "worker count below minimum",
			env:     map[string]string{"WORKER_COUNT": "0"},
			wantErr: true,
		},
		{
			name:    "state ttl below minimum",
			env:     map[string]string{"STATE_TTL": "100ms"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearServerEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := config.LoadServer()
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadServer() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadServer() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("LoadServer() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadGeofence(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Geofence
		wantErr bool
	}{
		{
			name: "defaults",
			want: config.Geofence{
				NATSURL:           "nats://localhost:4222",
				PostgresDSN:       "postgres://uav:uav@localhost:5432/uav",
				HTTPAddr:          ":8081",
				WorkerCount:       8,
				QueueSize:         256,
				HistoryRetention:  24 * time.Hour,
				BatchSize:         100,
				FlushInterval:     time.Second,
				SwarmRadiusM:      5000,
				SwarmMinSize:      3,
				SwarmEvalInterval: 5 * time.Second,
				IngestServerAddr:  "localhost:50051",
				ReplicaID:         "geofence-0",
				ShardIndex:        0,
				ShardCount:        1,
				PartitionCount:    4,
			},
		},
		{
			name:    "batch size below minimum",
			env:     map[string]string{"BATCH_SIZE": "0"},
			wantErr: true,
		},
		{
			name:    "swarm min size below minimum",
			env:     map[string]string{"SWARM_MIN_SIZE": "1"},
			wantErr: true,
		},
		{
			name:    "flush interval below minimum",
			env:     map[string]string{"FLUSH_INTERVAL": "10ms"},
			wantErr: true,
		},
		{
			name:    "retention below minimum",
			env:     map[string]string{"HISTORY_RETENTION": "10s"},
			wantErr: true,
		},
		{
			name:    "invalid retention",
			env:     map[string]string{"HISTORY_RETENTION": "sometimes"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearGeofenceEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			got, err := config.LoadGeofence()
			if tt.wantErr {
				if err == nil {
					t.Fatal("LoadGeofence() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadGeofence() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("LoadGeofence() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadSimulator(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{name: "defaults"},
		{
			name:    "interval below minimum",
			env:     map[string]string{"SEND_INTERVAL": "1ms"},
			wantErr: true,
		},
		{
			name:    "invalid drone count",
			env:     map[string]string{"DRONE_COUNT": "-1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearSimulatorEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := config.LoadSimulator()
			if tt.wantErr && err == nil {
				t.Fatal("LoadSimulator() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("LoadSimulator() error = %v", err)
			}
		})
	}
}

func clearServerEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GRPC_ADDR", "HTTP_ADDR", "NATS_URL", "WORKER_COUNT", "QUEUE_SIZE", "STATE_TTL"} {
		t.Setenv(key, "")
	}
}

func clearGeofenceEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"NATS_URL", "POSTGRES_DSN", "HTTP_ADDR", "WORKER_COUNT", "QUEUE_SIZE", "HISTORY_RETENTION", "BATCH_SIZE", "FLUSH_INTERVAL"} {
		t.Setenv(key, "")
	}
}

func clearSimulatorEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"SERVER_ADDR", "DRONE_COUNT", "SEND_INTERVAL"} {
		t.Setenv(key, "")
	}
}
