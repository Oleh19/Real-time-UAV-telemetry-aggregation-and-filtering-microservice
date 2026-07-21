package main

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"uavmonitor/gen/telemetryv1"
	"uavmonitor/internal/telemetry"
)

type ingestToken string

func (t ingestToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

func (ingestToken) RequireTransportSecurity() bool { return false }

type grpcTelemetryPublisher struct {
	client telemetryv1.TelemetryServiceClient
	conn   *grpc.ClientConn
	mu     sync.Mutex
	stream telemetryv1.TelemetryService_StreamTelemetryClient
}

func newGRPCTelemetryPublisher(serverAddr, token string) (*grpcTelemetryPublisher, error) {
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if token != "" {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(ingestToken(token)))
	}
	conn, err := grpc.NewClient(serverAddr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("connect to ingest server at %s: %w", serverAddr, err)
	}
	return &grpcTelemetryPublisher{
		client: telemetryv1.NewTelemetryServiceClient(conn),
		conn:   conn,
	}, nil
}

func (p *grpcTelemetryPublisher) Publish(_ context.Context, sample telemetry.Sample) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == nil {
		stream, err := p.client.StreamTelemetry(context.Background())
		if err != nil {
			return fmt.Errorf("open replay ingest stream: %w", err)
		}
		p.stream = stream
	}
	msg := &telemetryv1.DroneTelemetry{
		DroneId:    string(sample.DroneID),
		StationId:  string(sample.StationID),
		Timestamp:  timestamppb.New(sample.Timestamp),
		Latitude:   sample.Latitude,
		Longitude:  sample.Longitude,
		Altitude:   sample.Altitude,
		Speed:      sample.Speed,
		Confidence: sample.Confidence,
	}
	if err := p.stream.Send(msg); err != nil {
		p.stream = nil
		return fmt.Errorf("send replayed telemetry: %w", err)
	}
	return nil
}

func (p *grpcTelemetryPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		_, _ = p.stream.CloseAndRecv()
		p.stream = nil
	}
	_ = p.conn.Close()
}
